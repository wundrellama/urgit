package launcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// rawCall sends one request line after a hello on a fresh connection and
// returns the launcher's reply line: the request is the same bytes
// whichever client would send it.
func rawCall(t *testing.T, path, daemon, request string) string {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	if _, err := fmt.Fprintf(c, `{"op":"hello","daemon":%q}`+"\n", daemon); err != nil {
		t.Fatal(err)
	}
	if line, err := r.ReadString('\n'); err != nil || !strings.Contains(line, `"ok":true`) {
		t.Fatalf("hello: %q %v", line, err)
	}
	if _, err := fmt.Fprintln(c, request); err != nil {
		t.Fatal(err)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	return strings.TrimSpace(line)
}

// A destroy names the incarnation it means (INTEGRATION.md §§6, 11.1): a
// caller holding an earlier incarnation of an id — a retention the
// operator has released since, a reconcile pass that listed it before —
// never tears down the reservation the same attempt holds now, whether it
// names the old incarnation by its token or by its cid and creation time;
// the released incarnation it names is answered as destroyed (idempotent).
// A destroy that names no incarnation is refused: there is no bare-id route.
func TestStaleDestroyNeverReachesALaterIncarnation(t *testing.T) {
	h := newFakeHost()
	path, _ := serve(t, h, map[uint32]bool{uint32(os.Getuid()): true})
	cl, err := Dial(context.Background(), path, "0va")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	req := ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}
	first, err := cl.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	// the named observation first (late-accounting ruling 01: a destroy
	// that names no incarnation held is refused, so its error comes after)
	_, derr := cl.DestroyOf(first.Ref())
	if _, err := cl.Inspect(first.ID); err == nil {
		t.Fatalf("THE CLIENT'S DESTROY OF ITS OWN INCARNATION DID NOT REACH IT: %v", derr)
	}
	if derr != nil {
		t.Fatal(derr)
	}
	second, err := cl.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !validIncarnation(second.Incarnation) || second.Incarnation == first.Incarnation {
		t.Fatalf("fixture: a new incarnation of the same id: %+v then %+v", first, second)
	}
	if _, err := cl.Create(second.Ref()); err != nil {
		t.Fatal(err)
	}
	untouched := func(what, reply string) {
		t.Helper()
		now, err := cl.Inspect(second.ID)
		if err != nil || now.State != StateRunning || now.Incarnation != second.Incarnation || h.has("kill "+second.ID) {
			t.Fatalf("A STALE DESTROY REACHED A LATER INCARNATION (%s): reply %s; the attempt's reservation now: %+v %v", what, reply, now, err)
		}
	}
	reply := rawCall(t, path, "0va", fmt.Sprintf(`{"op":"destroy","id":%q,"incarnation":%q}`, first.ID, first.Incarnation))
	untouched("by the old token", reply)
	if !strings.Contains(reply, `"ok":true`) {
		t.Fatalf("a destroy of an incarnation already released is done: %s", reply)
	}
	reply = rawCall(t, path, "0va", fmt.Sprintf(`{"op":"destroy","id":%q,"cid":%d,"created":%d}`, first.ID, first.CID, first.Created))
	untouched("by the old cid and creation time", reply)
	reply = rawCall(t, path, "0va", fmt.Sprintf(`{"op":"destroy","id":%q}`, first.ID))
	untouched("naming no incarnation", reply)
	if !strings.Contains(reply, "names no incarnation") {
		t.Fatalf("A DESTROY NAMING NO INCARNATION WAS NOT REFUSED: %s", reply)
	}
	// the incarnation it names now is destroyed as usual
	reply = rawCall(t, path, "0va", fmt.Sprintf(`{"op":"destroy","id":%q,"incarnation":%q}`, second.ID, second.Incarnation))
	if _, err := cl.Inspect(second.ID); !strings.Contains(reply, `"ok":true`) || err == nil {
		t.Fatalf("the current incarnation's destroy: %s, then %v", reply, err)
	}
}

// The incarnation token survives what the tuple does not (INTEGRATION.md
// §11.1; independent review 01, R2): after an attempt's reservation is
// released and the service reopened with no records, the same attempt
// admitted in the same second — or after the clock stepped back — gets the
// old cid and creation time again, but a new token. A stale destroy naming
// the old incarnation — by its token, or by the repeated tuple without one —
// releases nothing, and every other mutation naming it is refused, the new
// incarnation untouched.
func TestIncarnationTokenSurvivesAnEmptyRestart(t *testing.T) {
	for _, step := range []time.Duration{0, -10 * time.Second} {
		t.Run(fmt.Sprintf("the clock moved %v", step), func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			clock := freeze(&cfg)
			s := open(t, cfg, h, nil)
			req := lockedReq("0vsame", 1, 128)
			first := mustReserve(t, s, owner1, req)
			if _, err := s.CreateOf(owner1, first.Ref()); err != nil {
				t.Fatal(err)
			}
			if err := s.DestroyOf(owner1, first.Ref()); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			clock.set(clock.now().Add(step))
			again := open(t, cfg, h, nil)
			second := mustReserve(t, again, owner1, req)
			if second.ID != first.ID || second.CID != first.CID {
				t.Fatalf("fixture: the tuple repeats: %+v then %+v", first, second)
			}
			if step == 0 && second.Created != first.Created {
				t.Fatalf("fixture: the same second: %+v then %+v", first, second)
			}
			if _, err := again.CreateOf(owner1, second.Ref()); err != nil {
				t.Fatal(err)
			}
			running := func(what string, err error) {
				t.Helper()
				if r, ok := held(again, second.ID); !ok || r.State != StateRunning || r.Incarnation != second.Incarnation || !h.exists("vmm", second.ID) {
					t.Fatalf("A STALE REFERENCE REACHED THE NEW INCARNATION (%s): %v; %+v %v", what, err, r, ok)
				}
			}
			legacy := Ref{ID: first.ID, CID: first.CID, Created: first.Created}
			for _, c := range []struct {
				name string
				ref  Ref
			}{{"by its token", first.Ref()}, {"by its cid and creation time alone", legacy}} {
				err := again.DestroyOf(owner1, c.ref)
				running("destroy "+c.name, err)
				// policy-dependent (late-accounting ruling 01; §11.8): by its
				// token, the released incarnation is proven by its evidence,
				// done; by its cid and creation time alone the reference names
				// a record written before tokens, which no evidence shows
				// released — done before the ruling, refused now
				if c.ref.Incarnation != "" && err != nil {
					t.Fatalf("a destroy of a released incarnation is done: %v", err)
				}
				if c.ref.Incarnation == "" && !errors.Is(err, ErrUnproven) {
					t.Fatalf("a destroy naming no released incarnation: %v", err)
				}
				if _, err := again.CreateOf(owner1, c.ref); !errors.Is(err, ErrStale) {
					t.Fatalf("A STALE CREATE WAS NOT REFUSED (%s): %v", c.name, err)
				}
				if err := again.StopOf(owner1, c.ref); !errors.Is(err, ErrStale) {
					t.Fatalf("A STALE STOP WAS NOT REFUSED (%s): %v", c.name, err)
				}
				running("stop "+c.name, nil)
				if _, err := again.ConnectOf(owner1, c.ref, 5000); !errors.Is(err, ErrStale) {
					t.Fatalf("A STALE CONNECT WAS NOT REFUSED (%s): %v", c.name, err)
				}
			}
			if !validIncarnation(second.Incarnation) || second.Incarnation == first.Incarnation {
				t.Fatalf("THE NEW INCARNATION'S TOKEN IS NOT ITS OWN: %q then %q", first.Incarnation, second.Incarnation)
			}
			if err := again.DestroyOf(owner1, second.Ref()); err != nil {
				t.Fatalf("the new incarnation's own destroy: %v", err)
			}
		})
	}
}

// A record written before tokens existed keeps none — nothing is guessed
// for it — and is named only by its cid and creation time: a reference
// with a token never reaches it, and its operator selection carries "-".
// A record whose token is malformed is not loaded: the service fences.
func TestALegacyRecordIsNamedOnlyByItsTuple(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	old := goodRecord(cfg, "0vlegacy", minCID)
	writeRecord(t, cfg.StateDir, old)
	s := open(t, cfg, h, nil)
	rec, ok := held(s, old.ID)
	if !ok || rec.Incarnation != "" {
		t.Fatalf("a record written before tokens: %+v %v", rec, ok)
	}
	if sel := rec.Selection(); !strings.Contains(sel.String(), "/-/") {
		t.Fatalf("its selection: %s", sel)
	} else if back, err := ParseSelection(sel.String()); err != nil || back != sel {
		t.Fatalf("its selection does not read back: %+v %v", back, err)
	}
	tokened := Ref{ID: old.ID, Incarnation: strings.Repeat("a", 32), CID: old.CID, Created: old.Created}
	// policy-dependent (late-accounting ruling 01; INTEGRATION.md §11.8):
	// before it, this destroy of an incarnation never held was answered
	// done; no evidence proves its release, so it is refused now
	err := s.DestroyOf(owner1, tokened)
	if _, ok := held(s, old.ID); !ok {
		t.Fatal("A REFERENCE WITH A TOKEN REACHED A RECORD WRITTEN BEFORE TOKENS")
	}
	if !errors.Is(err, ErrUnproven) {
		t.Fatalf("a destroy of an incarnation never released: %v", err)
	}
	if err := s.DestroyOf(owner1, Ref{ID: old.ID, CID: old.CID, Created: old.Created}); err != nil {
		t.Fatal(err)
	}
	if _, ok := held(s, old.ID); ok {
		t.Fatal("its own reference did not destroy it")
	}
	for _, text := range []string{old.ID + "/" + strings.Repeat("A", 32) + "/3/1/1", old.ID + "/xyz/3/1/1", old.ID + "/3/1/1"} {
		if _, err := ParseSelection(text); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a malformed selection read: %q %v", text, err)
		}
	}
	bad := goodRecord(cfg, "0vbad", minCID+1)
	bad.Incarnation = "not-a-token"
	dir := t.TempDir()
	writeRecord(t, dir, bad)
	if snap, err := ReadState(dir, "t"); err != nil || len(snap.Problems) != 1 || snap.Problems[0].Kind != "identity" {
		t.Fatalf("A MALFORMED TOKEN WAS LOADED: %+v %v", snap, err)
	}
}

// Two incarnations whose tuple repeats — the attempt released as an
// incident, the service reopened with no records and the attempt admitted
// again in the same second — are two incidents: each release keeps its own
// evidence, by its token (INTEGRATION.md §11.1), neither refused for the
// other's nor taken for it.
func TestEvidenceIsKeptPerIncarnation(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	freeze(&cfg)
	release := func(s *Service) Record {
		t.Helper()
		rec := quarantinedByDisk(t, s, h, "0vsame")
		in, err := s.RetryCleanup(selectionOf(rec))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Release(in.Selection); err != nil {
			t.Fatalf("THE RELEASE OF AN INCARNATION WHOSE TUPLE REPEATS WAS REFUSED: %v", err)
		}
		return rec
	}
	s := open(t, cfg, h, nil)
	first := release(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	second := release(open(t, cfg, h, nil))
	if second.ID != first.ID || second.CID != first.CID || second.Created != first.Created {
		t.Fatalf("fixture: the tuple repeats: %s then %s", first.Ref(), second.Ref())
	}
	for _, rec := range []Record{first, second} {
		got, ok := archived(t, cfg.StateDir, rec)
		if !ok || got.Incarnation != rec.Incarnation || got.State != StateReleased {
			t.Fatalf("AN INCARNATION'S EVIDENCE WAS LOST OR TAKEN FOR ANOTHER'S: %s: %s %v", rec.Ref(), got.Ref(), ok)
		}
	}
}

// The wire's protocol is 2 since every mutation names its incarnation: a
// launcher that answers hello with protocol 1 — one that would ignore a
// token and act on a bare id — is refused at hello, before any request. The
// launcher here is a scripted reply on a fresh private unix socket.
func TestAProtocolOneLauncherIsRefused(t *testing.T) {
	t.Chdir(t.TempDir()) // a short, private socket path
	path := "old.sock"
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	served := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			served <- err
			return
		}
		defer c.Close()
		if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
			served <- err
			return
		}
		_, err = fmt.Fprintln(c, `{"ok":true,"launcher":"old","protocol":1}`)
		served <- err
	}()
	cl, err := Dial(context.Background(), path, "0va")
	if err == nil {
		cl.Close()
		t.Fatal("A LAUNCHER OF PROTOCOL 1 WAS WORKED WITH")
	}
	if !strings.Contains(err.Error(), "protocol 1") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// A reservation whose first publication failed after its rename — not
// acknowledged, kept charged (R8) — is named on its retained answer by its
// token, and its record carries that token across a restart: the reopened
// launcher releases it on its exact reference, and a reference by its cid
// and creation time alone — which names only a record written before
// tokens — releases nothing.
func TestARetainedReservationKeepsItsTokenAcrossARestart(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	s := open(t, testConfig(t.TempDir()), h, fsys)
	fsys.inject(&fsFault{op: "rename", mode: "after", err: errInjected, times: 1})
	fsys.inject(&fsFault{op: "unlink", path: ".json", mode: "before", err: errInjected, times: 1})
	_, err := s.Reserve(owner1, lockedReq("0vkept", 1, 128))
	var kept *retainedError
	if !errors.As(err, &kept) || !validIncarnation(kept.charged.Incarnation) {
		t.Fatalf("fixture: a retained reservation named by its token: %v", err)
	}
	fsys.clear()
	s2 := reopen(t, s, h)
	rec, ok := held(s2, kept.charged.ID)
	if !ok || rec.Incarnation != kept.charged.Incarnation {
		t.Fatalf("A RETAINED RESERVATION'S TOKEN DID NOT SURVIVE THE RESTART: %+v %v (named %s)", rec, ok, kept.charged.Ref())
	}
	// policy-dependent (late-accounting ruling 01; §11.8): before it, a
	// reference without the token — naming no incarnation held — was
	// answered done; no evidence proves a release of what it names now
	if err := s2.DestroyOf(owner1, Ref{ID: rec.ID, CID: rec.CID, Created: rec.Created}); !errors.Is(err, ErrUnproven) {
		t.Fatalf("a reference without its token: %v", err)
	}
	if _, ok := held(s2, rec.ID); !ok {
		t.Fatal("A REFERENCE WITHOUT ITS TOKEN RELEASED A RETAINED RESERVATION")
	}
	if err := s2.DestroyOf(owner1, kept.charged.Ref()); err != nil {
		t.Fatalf("its exact destroy after the restart: %v", err)
	}
	if _, ok := held(s2, rec.ID); ok || used(s2)[2] != 0 {
		t.Fatalf("its exact destroy after the restart released nothing: used %v", used(s2))
	}
}
