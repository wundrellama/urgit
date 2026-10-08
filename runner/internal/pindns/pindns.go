// Package pindns is the VM launcher's pinned-name responder (CI-P4-NET-1,
// named destinations, 2026-10-08). For one networked VM it answers DNS
// queries from a fixed table: the names the VM's policy grants, each with
// the IPv4 addresses the launcher resolved on the host and allowed in the
// VM's firewall chain. It never forwards a query and never resolves
// anything itself, so DNS cannot carry data out of the VM and cannot widen
// what the firewall allows.
//
// Answers:
//   - an A query for a pinned name: the pinned addresses;
//   - any other type for a pinned name: no data (NOERROR, no answers), so
//     a client asking AAAA or HTTPS falls back to A;
//   - a name that is not pinned, another class, another opcode, more or
//     fewer than one question, or a malformed question: REFUSED;
//   - a datagram too short for a header, or a response: nothing.
//
// The parser reads only the header and the one question, within the bytes
// it was given; additional records (EDNS0 OPT) are ignored.
package pindns

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	headerLen  = 12
	maxNameLen = 255 // wire length of a name, its final zero byte included
	udpLimit   = 512 // a response without EDNS0 fits a classic datagram
	tcpLimit   = 4096
	ttl        = 60
	typeA      = 1
	classIN    = 1
	rcodeOK    = 0
	rcodeRef   = 5
	// MaxAddrs bounds a pinned name's addresses: more is refused when the
	// table is built, never silently cut.
	MaxAddrs = 64
	// MaxConns bounds concurrent TCP clients; more are closed at once.
	MaxConns = 16
	// idle closes a TCP client that sends nothing for this long.
	idle = 10 * time.Second
)

// Table maps a lower-case DNS name to its pinned IPv4 addresses.
type Table map[string][]netip.Addr

// Check refuses a table the responder could not answer from faithfully:
// an empty name, a name that is not lower case, a name with no or too many
// addresses, or an address that is not IPv4.
func (t Table) Check() error {
	for name, addrs := range t {
		if name == "" || name != strings.ToLower(name) {
			return errors.New("pindns: a pinned name must be non-empty and lower case: " + name)
		}
		if len(addrs) == 0 || len(addrs) > MaxAddrs {
			return errors.New("pindns: a pinned name needs 1-64 addresses: " + name)
		}
		for _, a := range addrs {
			if !a.Is4() {
				return errors.New("pindns: only IPv4 addresses are pinned: " + name + " " + a.String())
			}
		}
	}
	return nil
}

// Answer is the response to one query message, or nil when nothing is to
// be sent. limit bounds the response size: answers that do not fit are
// left out (every one is allowed by the firewall, so any subset is right).
func Answer(t Table, q []byte, limit int) []byte {
	if len(q) < headerLen || q[2]&0x80 != 0 { // too short, or a response
		return nil
	}
	resp := make([]byte, headerLen, limit)
	copy(resp, q[:2]) // the id
	// QR, the opcode, AA and the query's RD; RA clear
	resp[2] = 0x80 | q[2]&0x78 | 0x04 | q[2]&0x01
	// a message whose question cannot be read is refused with the header
	// alone (QDCOUNT 0); a readable question is echoed below
	refuse := func() []byte {
		resp[3] = rcodeRef
		return resp
	}
	if q[2]&0x78 != 0 || binary.BigEndian.Uint16(q[4:6]) != 1 { // not QUERY, or not one question
		return refuse()
	}
	// the question: its name, then QTYPE and QCLASS
	i, name := headerLen, make([]byte, 0, maxNameLen)
	for {
		if i >= len(q) {
			return refuse()
		}
		n := int(q[i])
		if n == 0 {
			i++
			break
		}
		if n > 63 || i+1+n > len(q) || len(name)+n+2 > maxNameLen { // a pointer, a long label, short input
			return refuse()
		}
		if len(name) > 0 {
			name = append(name, '.')
		}
		name = append(name, q[i+1:i+1+n]...)
		i += 1 + n
	}
	if i+4 > len(q) || len(name) == 0 {
		return refuse()
	}
	qtype, qclass := binary.BigEndian.Uint16(q[i:i+2]), binary.BigEndian.Uint16(q[i+2:i+4])
	question := q[headerLen : i+4]
	if headerLen+len(question) > limit {
		return refuse()
	}
	// from here every answer echoes the question: a client (Go's, glibc's)
	// discards a reply whose question does not match its query and waits
	// for its timeout instead, so a refusal without it would be slow
	binary.BigEndian.PutUint16(resp[4:6], 1)
	resp = append(resp, question...)
	addrs, pinned := t[strings.ToLower(string(name))]
	if qclass != classIN || !pinned {
		resp[3] = rcodeRef
		return resp
	}
	resp[3] = rcodeOK
	if qtype != typeA {
		return resp // no data
	}
	var count uint16
	for _, a := range addrs {
		if len(resp)+16 > limit {
			break
		}
		ip := a.As4()
		resp = append(resp, 0xc0, headerLen, 0, typeA, 0, classIN, 0, 0, 0, ttl, 0, 4, ip[0], ip[1], ip[2], ip[3])
		count++
	}
	binary.BigEndian.PutUint16(resp[6:8], count)
	return resp
}

// Server answers on one datagram socket and one stream listener until
// Close.
type Server struct {
	table Table
	pc    net.PacketConn
	ln    net.Listener
	wg    sync.WaitGroup
	slots chan struct{}
	once  sync.Once
	mu    sync.Mutex
	conns map[net.Conn]bool
}

// Serve starts answering on pc and ln from a copy of t, which must pass
// Check. The sockets are the server's from then on: Close closes them.
func Serve(t Table, pc net.PacketConn, ln net.Listener) (*Server, error) {
	if err := t.Check(); err != nil {
		return nil, err
	}
	own := Table{}
	for name, addrs := range t {
		own[name] = append([]netip.Addr(nil), addrs...)
	}
	s := &Server{table: own, pc: pc, ln: ln, slots: make(chan struct{}, MaxConns), conns: map[net.Conn]bool{}}
	s.wg.Add(2)
	go s.serveUDP()
	go s.serveTCP()
	return s, nil
}

// Close stops the server, closes its sockets and every client, and waits
// for its goroutines.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		err = errors.Join(s.pc.Close(), s.ln.Close())
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
	})
	s.wg.Wait()
	return err
}

func (s *Server) serveUDP() {
	defer s.wg.Done()
	buf := make([]byte, 1500)
	for {
		n, from, err := s.pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if resp := Answer(s.table, buf[:n], udpLimit); resp != nil {
			s.pc.WriteTo(resp, from)
		}
	}
}

func (s *Server) serveTCP() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		select {
		case s.slots <- struct{}{}:
		default:
			c.Close() // MaxConns clients already
			continue
		}
		s.mu.Lock()
		s.conns[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.slots }()
			defer func() {
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
				c.Close()
			}()
			s.serveConn(c)
		}()
	}
}

// serveConn answers length-prefixed queries on one client until it is
// idle, sends a message past tcpLimit, or goes.
func (s *Server) serveConn(c net.Conn) {
	var size [2]byte
	for {
		c.SetDeadline(time.Now().Add(idle))
		if _, err := io.ReadFull(c, size[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(size[:]))
		if n > tcpLimit {
			return
		}
		q := make([]byte, n)
		if _, err := io.ReadFull(c, q); err != nil {
			return
		}
		resp := Answer(s.table, q, tcpLimit)
		if resp == nil {
			return
		}
		out := binary.BigEndian.AppendUint16(nil, uint16(len(resp)))
		if _, err := c.Write(append(out, resp...)); err != nil {
			return
		}
	}
}
