package pindns

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

var table = Table{
	"archive.ubuntu.com":  {netip.MustParseAddr("185.125.190.81"), netip.MustParseAddr("91.189.91.82")},
	"bootstrap.urbit.org": {netip.MustParseAddr("104.21.64.85")},
}

// serve starts a responder on loopback and a resolver that only asks it
// (Go's own DNS client, over UDP and then TCP as the client chooses).
func serve(t *testing.T, tab Table) (*Server, *net.Resolver, string) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		pc.Close()
		t.Fatal(err)
	}
	s, err := Serve(tab, pc, ln)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}
	return s, r, addr
}

func lookup(t *testing.T, r *net.Resolver, name string) ([]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.LookupHost(ctx, name)
}

// A pinned name answers exactly its pinned addresses, whatever the case
// the client asks in; Go's client asks A and AAAA, and AAAA gets no data.
func TestPinnedNameAnswersItsAddresses(t *testing.T) {
	_, r, _ := serve(t, table)
	got, err := lookup(t, r, "archive.ubuntu.com")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"185.125.190.81", "91.189.91.82"}; !slices.Equal(got, want) {
		t.Fatalf("archive.ubuntu.com: %v, want %v", got, want)
	}
	if got, err := lookup(t, r, "Bootstrap.Urbit.ORG"); err != nil || !slices.Equal(got, []string{"104.21.64.85"}) {
		t.Fatalf("a pinned name in mixed case: %v %v", got, err)
	}
}

// Any name the policy does not grant is refused: never forwarded, never
// resolved, no address.
func TestUngrantedNameIsRefused(t *testing.T) {
	_, r, _ := serve(t, table)
	// fully qualified, so the host's search domains are not tried; Go's
	// client answers localhost itself, so that case is TestAnswerRules'
	for _, name := range []string{"github.com.", "evil.example.", "ubuntu.com.", "x.archive.ubuntu.com."} {
		start := time.Now()
		got, err := lookup(t, r, name)
		var de *net.DNSError
		if err == nil || !errors.As(err, &de) || len(got) != 0 {
			t.Fatalf("AN UNGRANTED NAME %s WAS ANSWERED: %v %v", name, got, err)
		}
		// the refusal is an answer the client accepts, not a timeout: it
		// echoes the question, so the client stops at once
		if de.IsTimeout || time.Since(start) > 2*time.Second {
			t.Fatalf("the refusal of %s was a timeout (%s, %v): the client discarded it", name, time.Since(start).Round(time.Millisecond), err)
		}
	}
}

func query(id uint16, name string, qtype, qclass uint16, flags byte) []byte {
	q := make([]byte, headerLen)
	binary.BigEndian.PutUint16(q[0:2], id)
	q[2] = flags
	binary.BigEndian.PutUint16(q[4:6], 1)
	for _, l := range strings.Split(name, ".") {
		q = append(q, byte(len(l)))
		q = append(q, l...)
	}
	q = append(q, 0)
	q = binary.BigEndian.AppendUint16(q, qtype)
	return binary.BigEndian.AppendUint16(q, qclass)
}

func rcode(resp []byte) int { return int(resp[3] & 0x0f) }
func ancount(resp []byte) int {
	return int(binary.BigEndian.Uint16(resp[6:8]))
}

// The answer rules, message by message: what is refused, what gets no
// data, what gets nothing at all, and that every answer echoes the id.
func TestAnswerRules(t *testing.T) {
	a := Answer(table, query(0x1234, "archive.ubuntu.com", typeA, classIN, 0x01), udpLimit)
	if a == nil || binary.BigEndian.Uint16(a[0:2]) != 0x1234 || rcode(a) != rcodeOK || ancount(a) != 2 || a[2]&0x80 == 0 || a[2]&0x01 == 0 {
		t.Fatalf("a pinned A query: %x", a)
	}
	if a := Answer(table, query(1, "archive.ubuntu.com", 28, classIN, 0), udpLimit); rcode(a) != rcodeOK || ancount(a) != 0 {
		t.Fatalf("AAAA for a pinned name must be no data: %x", a)
	}
	refused := map[string][]byte{
		"an ungranted name":         query(1, "github.com", typeA, classIN, 0),
		"localhost":                 query(1, "localhost", typeA, classIN, 0),
		"a parent of a pinned name": query(1, "ubuntu.com", typeA, classIN, 0),
		"a child of a pinned name":  query(1, "x.archive.ubuntu.com", typeA, classIN, 0),
		"another class":             query(1, "archive.ubuntu.com", typeA, 3, 0),
		"an inverse query opcode":   query(1, "archive.ubuntu.com", typeA, classIN, 0x08),
		"a compression pointer":     append(append([]byte(nil), query(1, "x.y", typeA, classIN, 0)[:headerLen]...), 0xc0, 0x0c, 0, 1, 0, 1),
		"a truncated question":      query(1, "archive.ubuntu.com", typeA, classIN, 0)[:headerLen+5],
		"no question":               append([]byte{0, 1, 0, 0, 0, 0}, make([]byte, 6)...),
		"two questions": func() []byte {
			q := query(1, "archive.ubuntu.com", typeA, classIN, 0)
			binary.BigEndian.PutUint16(q[4:6], 2)
			return q
		}(),
	}
	for name, q := range refused {
		a := Answer(table, q, udpLimit)
		if a == nil || rcode(a) != rcodeRef || ancount(a) != 0 {
			t.Fatalf("%s: NOT REFUSED: %x", name, a)
		}
	}
	// a refusal of a readable question echoes it (the client matches on it)
	q := query(9, "github.com", typeA, classIN, 0)
	if a := Answer(table, q, udpLimit); binary.BigEndian.Uint16(a[4:6]) != 1 || string(a[headerLen:]) != string(q[headerLen:]) {
		t.Fatalf("a refusal did not echo its question: %x", a)
	}
	if a := Answer(table, []byte{1, 2, 3}, udpLimit); a != nil {
		t.Fatalf("a datagram shorter than a header was answered: %x", a)
	}
	if a := Answer(table, query(1, "archive.ubuntu.com", typeA, classIN, 0x80), udpLimit); a != nil {
		t.Fatalf("a response was answered (a reflection loop): %x", a)
	}
}

// An answer never exceeds its limit: addresses that do not fit are left
// out, never truncated mid-record.
func TestAnswerFitsItsLimit(t *testing.T) {
	many := Table{"many.example": nil}
	for i := 0; i < MaxAddrs; i++ {
		many["many.example"] = append(many["many.example"], netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}))
	}
	q := query(7, "many.example", typeA, classIN, 0)
	a := Answer(many, q, udpLimit)
	if len(a) > udpLimit || rcode(a) != rcodeOK {
		t.Fatalf("an answer of %d bytes past the %d limit", len(a), udpLimit)
	}
	if want := (udpLimit - len(q)) / 16; ancount(a) != want || len(a) != len(q)+16*want {
		t.Fatalf("answers %d (want %d), length %d", ancount(a), want, len(a))
	}
}

// A table the responder could not answer faithfully is refused.
func TestCheckRefusesBadTables(t *testing.T) {
	bad := map[string]Table{
		"an empty name":      {"": {netip.MustParseAddr("192.0.2.1")}},
		"upper case":         {"Archive.ubuntu.com": {netip.MustParseAddr("192.0.2.1")}},
		"no addresses":       {"a.example": nil},
		"an IPv6 address":    {"a.example": {netip.MustParseAddr("2001:db8::1")}},
		"too many addresses": {"a.example": make([]netip.Addr, MaxAddrs+1)},
	}
	for name, tab := range bad {
		if err := tab.Check(); err == nil {
			t.Fatalf("%s: a bad table was accepted", name)
		}
	}
	if err := table.Check(); err != nil {
		t.Fatal(err)
	}
}

// Close stops both sockets and every open client.
func TestCloseStopsEverything(t *testing.T) {
	s, _, addr := serve(t, table)
	c, err := net.Dial("tcp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return while a client was connected")
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("a client stayed open after Close")
	}
	if _, err := net.Dial("tcp4", addr); err == nil {
		t.Fatal("the listener still accepts after Close")
	}
}
