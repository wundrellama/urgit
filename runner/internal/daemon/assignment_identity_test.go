package daemon

// Assignment-identity ruling 01, Q13 A (runner/launcher/INTEGRATION.md
// §11.13, "One assignment, one identity"; independent review 09, R9-1): an
// authenticated assignment's attempt is its atom. A delivery respelling it —
// a separator added or moved, a leading zero, in both of its fields — keeps
// the signed bytes and the original signature, and is the same attempt: it
// takes no second claim, reserves no second sandbox, is never given up to
// the ship while the attempt runs here, and every message about the attempt
// is in the ship's own spelling. Every respelling below copies the signed
// assignment and respells only its attempt: no signing key makes an alias.
// A new attempt — a new atom, signed anew — is claimed and run as before.
//
// Through the real Run loop, verifyAssignment and claim, the real Microvm
// backend and the launcher's real core and wire over private files, with
// the in-process ships of retained_test.go and legacy_test.go.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// attemptAtom is an attempt as the ship writes it (scot %uv); the aliases
// are other spellings of its atom.
const attemptAtom = "0v5cmd"

var attemptAliases = []string{"0v5.cmd", "0v05cmd", "0v5c.md"}

// respelledAssignment is the signed assignment a with only its attempt
// respelled, in both of its fields: the same signed bytes, the original
// signature.
func respelledAssignment(a *ship.Assignment, attempt string) *ship.Assignment {
	c := *a
	s := *a.Sig
	c.Sig = &s
	c.Attempt, c.Sig.Attempt = attempt, attempt
	return &c
}

// heardPaths serves the ship of these tests, and records every request it
// hears.
type heardPaths struct {
	mu    sync.Mutex
	paths []string
	next  http.Handler
}

func (h *heardPaths) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.paths = append(h.paths, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/apps/urgit/api/ci"))
	h.mu.Unlock()
	h.next.ServeHTTP(w, r)
}

// attemptPaths is every request the ship heard about an attempt.
func (h *heardPaths) attemptPaths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, p := range h.paths {
		if strings.Contains(p, " /attempt/") {
			out = append(out, p)
		}
	}
	return out
}

// startHeard is f's daemon, with every request its ship hears recorded.
func (f *vmFixture) startHeard(t *testing.T) (*Daemon, *heardPaths) {
	t.Helper()
	d := f.start(t)
	heard := &heardPaths{next: f.ship}
	d.client.HTTP = &http.Client{Transport: inproc{"ship.test": heard}}
	return d, heard
}

// offerAssignment hands a to Run on a later poll.
func (f *vmFixture) offerAssignment(a *ship.Assignment) {
	f.ship.mu.Lock()
	defer f.ship.mu.Unlock()
	f.ship.queue = append(f.ship.queue, queued{a: a})
}

// reachedWithin is the next launcher step that reached a hold, if one does
// within d.
func (f *vmFixture) reachedWithin(d time.Duration) (string, bool) {
	select {
	case op := <-f.host.Reached():
		return op, true
	case <-time.After(d):
		return "", false
	}
}

// claims is how many attempts this process holds a claim on.
func claims(d *Daemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.inFlight)
}

// ignoredOrActed waits until Run has acted on the last delivery: the log
// says it was ignored, or done says so; at most 20 s.
func ignoredOrActed(f *vmFixture, done func() bool) {
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(f.logs.String(), "is already running here") && !done() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// A respelled delivery of an attempt this process has claimed — running,
// or waiting for a slot — is that attempt, delivered again: no second
// claim, and no second reservation. The same spelling repeated is the
// control; a new attempt is claimed, and reserves its own sandbox.
func TestARespelledDeliveryOfAClaimedAttemptIsThatAttempt(t *testing.T) {
	type delivery struct {
		name, attempt string
		fresh         bool
	}
	deliveries := []delivery{{"the same spelling", attemptAtom, false}}
	for _, a := range attemptAliases {
		deliveries = append(deliveries, delivery{"respelled " + a, a, false})
	}
	deliveries = append(deliveries, delivery{"a new attempt", "0v6cmd", true})
	for _, c := range deliveries {
		t.Run("running/"+c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			d, _ := f.startHeard(t)
			defer closeDaemon(d)
			release := f.host.Hold(t, "disk")
			orig := f.signed(t, attemptAtom)
			f.offerAssignment(orig)
			stop := run(t, d)
			if _, ok := f.reachedWithin(20 * time.Second); !ok {
				release()
				stop()
				t.Fatalf("fixture: the attempt did not reach its disk step; log:\n%s", f.logs.String())
			}
			again := respelledAssignment(orig, c.attempt)
			if c.fresh {
				again = f.signed(t, c.attempt)
			}
			f.offerAssignment(again)
			var second string
			ignoredOrActed(f, func() bool {
				if op, ok := f.reachedWithin(time.Millisecond); ok {
					second = op
				}
				return second != ""
			})
			n := claims(d)
			release()
			stop()
			if c.fresh {
				if second != "disk "+launcher.IDFor("t", c.attempt) || n != 2 {
					t.Fatalf("a new attempt was not claimed and reserved: claims %d, second reservation %q; log:\n%s", n, second, f.logs.String())
				}
				return
			}
			if second != "" || n != 1 {
				t.Fatalf("A RESPELLED DELIVERY OF A CLAIMED ATTEMPT WAS CLAIMED AGAIN (%s): claims %d, a second reservation %q; launcher ops %v", c.name, n, second, f.host.Ops())
			}
			if !strings.Contains(f.logs.String(), "is already running here") {
				t.Fatalf("the delivery was not said to be ignored:\n%s", f.logs.String())
			}
		})
		t.Run("waiting/"+c.name, func(t *testing.T) {
			f := newVMFixture(t, 1)
			d, _ := f.startHeard(t)
			defer closeDaemon(d)
			release := f.host.Hold(t, "disk")
			f.offerAssignment(f.signed(t, "0v7cmd")) // it holds the only slot
			stop := run(t, d)
			if _, ok := f.reachedWithin(20 * time.Second); !ok {
				release()
				stop()
				t.Fatalf("fixture: the first attempt did not reach its disk step; log:\n%s", f.logs.String())
			}
			orig := f.signed(t, attemptAtom)
			f.offerAssignment(orig)
			deadline := time.Now().Add(20 * time.Second)
			for claims(d) < 2 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if claims(d) != 2 {
				release()
				stop()
				t.Fatalf("fixture: the attempt did not wait for the slot: claims %d; log:\n%s", claims(d), f.logs.String())
			}
			again := respelledAssignment(orig, c.attempt)
			if c.fresh {
				again = f.signed(t, c.attempt)
			}
			f.offerAssignment(again)
			ignoredOrActed(f, func() bool { return claims(d) > 2 })
			n := claims(d)
			release()
			stop()
			want := 2
			if c.fresh {
				want = 3
			}
			if n != want {
				t.Fatalf("A RESPELLED DELIVERY OF A WAITING ATTEMPT WAS CLAIMED AGAIN (%s): claims %d, want %d; log:\n%s", c.name, n, want, f.logs.String())
			}
		})
	}
}

// A respelled delivery that is the first of its attempt runs as the ship
// writes the attempt: its launcher record, and every message about it, are
// in the ship's spelling, never the delivered one.
func TestARespelledAttemptRunsAsTheShipWritesIt(t *testing.T) {
	for _, alias := range attemptAliases {
		t.Run(alias, func(t *testing.T) {
			f := newVMFixture(t, 1)
			d, heard := f.startHeard(t)
			defer closeDaemon(d)
			f.offerAssignment(respelledAssignment(f.signed(t, attemptAtom), alias))
			stop := run(t, d)
			// the run ends at its checkout, which this fixture has none of,
			// and is answered
			waitUntil(t, 20*time.Second, "the attempt's answer", f.ship.abandoned(1))
			stop()
			if f.host.Count("disk", launcher.IDFor("t", alias)) != 0 || f.host.Count("disk", launcher.IDFor("t", attemptAtom)) != 1 {
				t.Fatalf("A RESPELLED ATTEMPT WAS RESERVED UNDER ITS SPELLING, NOT AS THE SHIP WRITES IT: launcher ops %v", f.host.Ops())
			}
			asked := heard.attemptPaths()
			if len(asked) == 0 {
				t.Fatal("fixture: the ship heard nothing about the attempt")
			}
			for _, p := range asked {
				if !strings.Contains(p, " /attempt/"+attemptAtom+"/") && !strings.HasSuffix(p, " /attempt/"+attemptAtom) {
					t.Fatalf("A MESSAGE ABOUT A RESPELLED ATTEMPT NAMED IT OTHERWISE THAN THE SHIP WRITES IT: %v", asked)
				}
			}
		})
	}
}

// A delivery naming an attempt this process has claimed is never given up
// to the ship — not while the state file is not durable, and not when the
// delivery fails verification — whatever its spelling: an abandon would
// hand the attempt to another runner while it runs here. Nothing runs for
// the delivery, and nothing is said about it.
func TestADeliveryOfAClaimedAttemptIsNeverGivenUp(t *testing.T) {
	for _, why := range []string{"the state file not durable", "a copy that fails verification"} {
		for _, spelling := range []string{attemptAtom, "0v5.cmd", "0v05cmd"} {
			t.Run(why+"/"+spelling, func(t *testing.T) {
				f := newVMFixture(t, 2)
				d, heard := f.startHeard(t)
				defer closeDaemon(d)
				release := f.host.Hold(t, "disk")
				orig := f.signed(t, attemptAtom)
				f.offerAssignment(orig)
				stop := run(t, d)
				if _, ok := f.reachedWithin(20 * time.Second); !ok {
					release()
					stop()
					t.Fatalf("fixture: the attempt did not reach its disk step; log:\n%s", f.logs.String())
				}
				again := respelledAssignment(orig, spelling)
				if why == "a copy that fails verification" {
					again.Sig.Nonce = "0v1.tampered"
				} else {
					d.mu.Lock()
					d.saveState = func(path string, _ *state.State) error {
						return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
					}
					d.persistLocked()
					d.mu.Unlock()
				}
				f.offerAssignment(again)
				ignoredOrActed(f, f.ship.abandoned(1))
				gaveUp := f.ship.abandon(0)
				asked := heard.attemptPaths()
				d.mu.Lock()
				d.saveState = nil
				d.mu.Unlock()
				release()
				stop()
				if gaveUp != "" {
					t.Fatalf("A DELIVERY OF A CLAIMED ATTEMPT WAS GIVEN UP TO THE SHIP (%s, %s): %q; the ship heard %v", why, spelling, gaveUp, asked)
				}
				if !strings.Contains(f.logs.String(), "is already running here") {
					t.Fatalf("the delivery was not said to be ignored:\n%s", f.logs.String())
				}
			})
		}
	}
}

// verifyAssignment binds an assignment's attempt and candidate to the
// signed ones by atom and, once it verifies, names the assignment as the
// ship writes it: its attempt and candidate in both of their fields, and
// the manifest's incarnation. Whatever does not verify is refused, as
// before: another atom under the original signature, a respelled
// recipient, a text that is no @uv, another candidate, an expired
// authorization.
func TestAnAssignmentIsVerifiedAsItsAtoms(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	canonical := func(text string) string {
		c, err := sig.CanonicalUV(text)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	// the manifest as the ship writes it: every @uv in its own spelling
	shipManifest := jobManifest
	shipManifest.Candidate, shipManifest.Incarnation = canonical(jobManifest.Candidate), canonical(jobManifest.Incarnation)
	recipientAlias := canonical(vmDaemon) // this daemon's id respelled
	d := &Daemon{daemonID: vmDaemon, ciKey: hex.EncodeToString(pub), log: log.New(io.Discard, "", 0), inFlight: map[string]bool{}}
	sign := func(attempt string, expiry int64) *ship.Assignment {
		m := shipManifest
		a := *jobAssignment
		a.Attempt, a.Candidate, a.Manifest = attempt, m.Candidate, &m
		msg := sig.ManifestMessage{Recipient: vmDaemon, Attempt: attempt, Operation: "assign", Expiry: expiry, Nonce: "0v7", Manifest: m}
		b, err := msg.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		a.Sig = &ship.Signature{Version: sig.ManifestVersion, Recipient: vmDaemon, Attempt: attempt, Operation: "assign", Expiry: expiry, Nonce: "0v7", Sig: hex.EncodeToString(ed25519.Sign(priv, b))}
		return &a
	}
	soon := time.Now().Add(time.Minute).Unix()
	named := func(t *testing.T, a *ship.Assignment) {
		t.Helper()
		if a.Attempt != attemptAtom || a.Sig.Attempt != attemptAtom || a.Candidate != shipManifest.Candidate ||
			a.Manifest.Candidate != shipManifest.Candidate || a.Manifest.Incarnation != shipManifest.Incarnation {
			t.Fatalf("A VERIFIED ASSIGNMENT WAS NOT NAMED AS THE SHIP WRITES IT: attempt %q / %q, candidate %q / %q, incarnation %q",
				a.Attempt, a.Sig.Attempt, a.Candidate, a.Manifest.Candidate, a.Manifest.Incarnation)
		}
	}
	t.Run("the ship's own spelling", func(t *testing.T) {
		a := sign(attemptAtom, soon)
		if err := d.verifyAssignment(a); err != nil {
			t.Fatalf("fixture: the assignment verifies: %v", err)
		}
		named(t, a)
	})
	for _, alias := range attemptAliases {
		t.Run("respelled together/"+alias, func(t *testing.T) {
			a := respelledAssignment(sign(attemptAtom, soon), alias)
			if err := d.verifyAssignment(a); err != nil {
				t.Fatalf("a respelled assignment keeps its signature: %v", err)
			}
			named(t, a)
		})
	}
	respelled := []struct {
		name   string
		change func(a *ship.Assignment)
	}{
		{"the assignment's attempt alone", func(a *ship.Assignment) { a.Attempt = "0v5.cmd" }},
		{"the signature's attempt alone", func(a *ship.Assignment) { a.Sig.Attempt = "0v05cmd" }},
		{"the assignment's candidate alone", func(a *ship.Assignment) { a.Candidate = jobManifest.Candidate }},
		{"the manifest's candidate alone", func(a *ship.Assignment) { a.Manifest.Candidate = jobManifest.Candidate }},
		{"the manifest's incarnation", func(a *ship.Assignment) { a.Manifest.Incarnation = jobManifest.Incarnation }},
	}
	for _, c := range respelled {
		t.Run("respelled/"+c.name, func(t *testing.T) {
			a := sign(attemptAtom, soon)
			c.change(a)
			if err := d.verifyAssignment(a); err != nil {
				t.Fatalf("ONE ASSIGNMENT IN TWO SPELLINGS WAS REFUSED (%s): %v", c.name, err)
			}
			named(t, a)
		})
	}
	refused := []struct {
		name   string
		change func(a *ship.Assignment)
	}{
		{"another atom under the original signature", func(a *ship.Assignment) { a.Attempt, a.Sig.Attempt = "0v8cmd", "0v8cmd" }},
		{"the recipient respelled", func(a *ship.Assignment) { a.Sig.Recipient = recipientAlias }},
		{"an attempt that is no @uv", func(a *ship.Assignment) { a.Attempt, a.Sig.Attempt = "0vzz", "0vzz" }},
		{"another candidate", func(a *ship.Assignment) { a.Candidate = "0v9.cand" }},
		{"expired", func(a *ship.Assignment) { a.Sig.Expiry = time.Now().Add(-time.Minute).Unix() }},
	}
	for _, c := range refused {
		t.Run("refused/"+c.name, func(t *testing.T) {
			a := respelledAssignment(sign(attemptAtom, soon), "0v5.cmd")
			c.change(a)
			if err := d.verifyAssignment(a); err == nil {
				t.Fatalf("an assignment that does not verify was accepted (%s): %+v", c.name, a.Sig)
			}
		})
	}
}

// claim, release and claimed hold one claim per attempt, whatever the
// spelling each is asked under; a text that is no @uv is only itself.
func TestOneAttemptHasOneClaimWhateverItsSpelling(t *testing.T) {
	d := &Daemon{inFlight: map[string]bool{}}
	if !d.claim(attemptAtom) {
		t.Fatal("fixture: the first claim")
	}
	for _, alias := range attemptAliases {
		if d.claim(alias) {
			t.Fatalf("ONE ATTEMPT WAS CLAIMED TWICE UNDER TWO SPELLINGS (%s): %v", alias, d.inFlight)
		}
		if !d.claimed(alias) {
			t.Fatalf("A CLAIMED ATTEMPT WAS NOT FOUND UNDER ANOTHER SPELLING (%s): %v", alias, d.inFlight)
		}
	}
	d.release("0v5c.md")
	if d.claimed(attemptAtom) || len(d.inFlight) != 0 {
		t.Fatalf("A RELEASE UNDER ANOTHER SPELLING LEFT THE CLAIM: %v", d.inFlight)
	}
	if !d.claim("ci-not-an-atom") || d.claimed("ci-not.an-atom") || !d.claimed("ci-not-an-atom") {
		t.Fatalf("a text that is no @uv is only itself: %v", d.inFlight)
	}
}

// Where the question is the attempt, its atom answers: a legacy retention
// recorded in another spelling of an attempt claimed here is not idle, and
// the launcher's record of the attempt in another spelling is its record.
// Either keeps the retention's slot withheld. The report names the attempt
// as the ship writes it, and the record stays as written.
func TestARetentionInAnotherSpellingOfItsAttemptIsThatAttempts(t *testing.T) {
	t.Run("its attempt claimed here", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling("0v5.cmd"))
		d := f.start(t)
		defer closeDaemon(d)
		if e := reportedEntry(t, d); !e.Eligible {
			t.Fatalf("fixture: the entry is releasable: %+v", e.Conditions)
		}
		if !d.claim(attemptAtom) {
			t.Fatal("fixture: the attempt claimed")
		}
		if e := reportedEntry(t, d); e.Eligible {
			t.Fatalf("A RETENTION OF AN ATTEMPT CLAIMED HERE, RECORDED IN ANOTHER SPELLING, WAS OFFERED FOR RELEASE: %+v", e.Conditions)
		}
	})
	t.Run("a command while its attempt is claimed here", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling("0v5.cmd"))
		d := f.start(t)
		defer closeDaemon(d)
		e := reportedEntry(t, d)
		if !e.Eligible {
			t.Fatalf("fixture: the entry is releasable: %+v", e.Conditions)
		}
		cmd := toCommand(t, f.command(t, bound(e)))
		if !d.claim(attemptAtom) {
			t.Fatal("fixture: the attempt claimed")
		}
		if a := d.recover(context.Background(), cmd); a.Status == ship.AnswerCompleted || len(released(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("A RETENTION WAS RELEASED WHILE ITS ATTEMPT, IN ANOTHER SPELLING, WAS CLAIMED HERE: %+v", a)
		}
	})
	t.Run("the launcher's record of its attempt", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling("0v5.cmd"))
		cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cl.Reserve(launcher.ReserveRequest{Attempt: attemptAtom, Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}); err != nil {
			cl.Close()
			t.Fatal(err)
		}
		cl.Close()
		f.ship.status[attemptAtom] = "running"
		d := f.start(t)
		defer closeDaemon(d)
		if e := reportedEntry(t, d); e.Eligible {
			t.Fatalf("THE LAUNCHER'S RECORD OF AN ATTEMPT, IN ANOTHER SPELLING, WAS NOT COUNTED FOR IT: %+v", e.Conditions)
		}
	})
	t.Run("the report", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling("0v5.cmd"))
		d := f.start(t)
		defer closeDaemon(d)
		if e := reportedEntry(t, d); e.Attempt != attemptAtom {
			t.Fatalf("A RETENTION'S ATTEMPT WAS REPORTED OTHERWISE THAN THE SHIP WRITES IT: %q", e.Attempt)
		}
		if kept := retentions(t, f.cfg.StateFile); len(kept) != 1 || kept[0]["attempt"] != "0v5.cmd" || kept[0]["handle"] != "ci-0v5.cmd" {
			t.Fatalf("A RECORD UNDER ANOTHER SPELLING WAS REWRITTEN: %v", kept)
		}
	})
}

// Reconcile: a sandbox is this process's own only under its exact name. An
// orphan named in another spelling of an attempt claimed here is charged
// while the ship says the attempt runs, and the ship is asked about it in
// its own spelling.
func TestAnOrphanInAnotherSpellingOfAClaimedAttemptIsCharged(t *testing.T) {
	f := newVMFixture(t, 3)
	// what a daemon before this correction reserved for a respelled delivery
	cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v5.cmd", Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}); err != nil {
		cl.Close()
		t.Fatal(err)
	}
	cl.Close()
	f.ship.status[attemptAtom] = "running"
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	if !d.claim(attemptAtom) {
		t.Fatal("fixture: the attempt claimed")
	}
	if err := d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	_, held := d.held["ci-0v5.cmd"]
	d.mu.Unlock()
	if !held || len(f.srv.Held(t, vmDaemon)) != 1 {
		t.Fatalf("AN ORPHAN IN ANOTHER SPELLING OF A RUNNING ATTEMPT WAS NOT CHARGED: held %v, the launcher %+v; log:\n%s", held, f.srv.Held(t, vmDaemon), f.logs.String())
	}
	if asked := heard.attemptPaths(); len(asked) != 1 || asked[0] != "GET /attempt/"+attemptAtom {
		t.Fatalf("THE SHIP WAS ASKED ABOUT AN ORPHAN'S ATTEMPT OTHERWISE THAN IN ITS OWN SPELLING: %v", asked)
	}
}

// After a restart, an earlier life's reservation of an attempt the ship
// still runs is held; a respelled delivery of that attempt reserves no
// second sandbox for it.
func TestARespelledDeliveryAfterARestartReservesNoSecondSandbox(t *testing.T) {
	f := newVMFixture(t, 3)
	cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Reserve(launcher.ReserveRequest{Attempt: attemptAtom, Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}); err != nil {
		cl.Close()
		t.Fatal(err)
	}
	cl.Close()
	f.ship.status[attemptAtom] = "running"
	d, _ := f.startHeard(t)
	defer closeDaemon(d)
	if err := d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.offerAssignment(respelledAssignment(f.signed(t, attemptAtom), "0v5.cmd"))
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the delivery's answer", f.ship.abandoned(1))
	stop()
	if n := f.host.Count("disk", launcher.IDFor("t", "0v5.cmd")); n != 0 {
		t.Fatalf("A RESPELLED DELIVERY AFTER A RESTART RESERVED A SECOND SANDBOX FOR ITS ATTEMPT: %d create(s) under %s; launcher ops %v", n, launcher.IDFor("t", "0v5.cmd"), f.host.Ops())
	}
	for _, r := range f.srv.Held(t, vmDaemon) {
		if r.Attempt != attemptAtom {
			t.Fatalf("A RESPELLED DELIVERY AFTER A RESTART RESERVED A SECOND SANDBOX FOR ITS ATTEMPT: %+v", f.srv.Held(t, vmDaemon))
		}
	}
}
