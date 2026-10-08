package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

const c9Result = `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","msg":"done"}` + "\n"

func c9Wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("job did not return within test bound")
	}
}
func c9NoSignals(t *testing.T, b *silentJobBox) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if strings.Contains(strings.Join(b.ops, "\n"), "signal") {
		t.Fatalf("uncancelled job signalled: %v", b.ops)
	}
}
func c9Daemon(t *testing.T) (*Daemon, *silentJobBox, *fakeShip) {
	b := newSilentJobBox(t)
	sh := &fakeShip{}
	d := newTestDaemon(t, b.fakeBox, sh, 1)
	d.box, d.jobTiming = b, fastJobTiming()
	return d, b, sh
}
func TestC9UnclearStatusNeverStopsJob(t *testing.T) {
	cases := []struct {
		name string
		code int
		body string
	}{
		{"running", 200, `{"status":"running"}`},
		{"future", 200, `{"status":"new-terminal-name"}`},
		{"empty", 200, `{}`}, {"malformed", 200, `!`},
		{"absent", 404, `{}`}, {"error", 503, `{"status":"cancelled"}`},
		{"unauthorized", 401, `{}`},
		{"foreign", 401, `{"error":"attempt authentication required"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, b, sh := c9Daemon(t)
			queried := make(chan struct{}, 4)
			d.client.HTTP.Transport = inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.code)
					io.WriteString(w, tc.body)
					select {
					case queried <- struct{}{}:
					default:
					}
					return
				}
				sh.handler(t).ServeHTTP(w, r)
			})}
			finished := make(chan struct{})
			go func() { d.handle(context.Background(), jobAssignment); close(finished) }()
			for i := 0; i < 3; i++ {
				c9Wait(t, queried)
			}
			c9NoSignals(t, b)
			if _, err := io.WriteString(b.writer, c9Result); err != nil {
				t.Fatal(err)
			}
			b.writer.Close()
			b.done <- 0
			c9Wait(t, finished)
			c9NoSignals(t, b)
			sh.mu.Lock()
			defer sh.mu.Unlock()
			if len(sh.results) != 1 || !strings.Contains(sh.results[0], `"success"`) || len(sh.abandons) != 0 {
				t.Fatalf("results=%v abandons=%v", sh.results, sh.abandons)
			}
		})
	}
}

func TestC9RunReturnsOnDaemonStop(t *testing.T) {
	d, b, sh := c9Daemon(t)
	d.inFlight = map[string]bool{}
	d.ledger = state.LedgerFor(d.cfg.StateFile)
	var err error
	d.began, err = d.ledger.Open(state.History{Since: time.Now().Unix(), Enrolled: true, StateFormat: state.CurrentFormat}, false)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	d.ciKey = hex.EncodeToString(pub)
	a := *jobAssignment
	msg := sig.ManifestMessage{Recipient: d.daemonID, Attempt: a.Attempt, Operation: "assign", Expiry: time.Now().Unix() + 300, Nonce: "0v7", Manifest: *a.Manifest}
	data, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	a.Sig = &ship.Signature{Version: 2, Recipient: msg.Recipient, Attempt: msg.Attempt, Operation: msg.Operation, Expiry: msg.Expiry, Nonce: msg.Nonce, Sig: hex.EncodeToString(ed25519.Sign(priv, data))}
	var offered atomic.Bool
	d.client.HTTP.Transport = inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/assignment") {
			if !offered.Swap(true) {
				json.NewEncoder(w).Encode(map[string]any{"assignment": a})
				return
			}
			<-r.Context().Done()
			w.WriteHeader(204)
			return
		}
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"status":"running"}`)
			return
		}
		sh.handler(t).ServeHTTP(w, r)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	var code int
	go func() { code = d.Run(ctx); close(finished) }()
	c9Wait(t, b.started)
	cancel()
	c9Wait(t, finished)
	if code != 0 {
		t.Fatalf("Run exit=%d", code)
	}
	b.mu.Lock()
	ops := strings.Join(b.ops, "\n")
	b.mu.Unlock()
	if !strings.Contains(ops, "signal act TERM\nsignal act KILL\ndestroy ci-"+uvKey(jobAssignment.Attempt)) {
		t.Fatal(ops)
	}
}

func TestC9DeadlineReportingGrace(t *testing.T) {
	d, b, sh := c9Daemon(t)
	d.jobTiming.poll = time.Hour
	eventSeen := make(chan struct{})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
	defer cancel()
	d.client.HTTP.Transport = inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/event") {
			close(eventSeen)
			<-ctx.Done() // result arrived before deadline; ship answers after it
			if r.Context().Err() != nil {
				t.Error("reporting grace lost")
			}
		}
		sh.handler(t).ServeHTTP(w, r)
	})}
	// Build the real bundle without turning the daemon context into a deadline.
	work := t.TempDir()
	bundle, err := d.assembleBundle(context.Background(), jobAssignment, work, b.ExecEnv(sandbox.Handle{}))
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		d.runJob(ctx, context.Background(), jobAssignment, sandbox.Handle{}, b.ExecEnv(sandbox.Handle{}), bundle, func(string, ...any) {})
		close(finished)
	}()
	c9Wait(t, b.started)
	io.WriteString(b.writer, c9Result)
	c9Wait(t, eventSeen)
	b.writer.Close()
	b.done <- 0
	c9Wait(t, finished)
	c9NoSignals(t, b)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if len(sh.results) != 1 || len(sh.abandons) != 0 {
		t.Fatalf("results=%v abandons=%v", sh.results, sh.abandons)
	}
}

func TestC9StopAfterAttemptDeadline(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	attempt, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer cancel()
	report, done := reportingContext(attempt, parent)
	defer done()
	if report.Err() != nil {
		t.Fatal("attempt deadline removed grace")
	}
	stop()
	if report.Err() != context.Canceled {
		t.Fatal("daemon stop did not cancel reporting after deadline")
	}
}

func TestC9Existing409StillStops(t *testing.T) {
	d, b, sh := c9Daemon(t)
	d.jobTiming.poll = time.Hour
	d.client.HTTP.Transport = inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/event") {
			w.WriteHeader(409)
			io.WriteString(w, `{"error":"attempt is closed"}`)
			return
		}
		sh.handler(t).ServeHTTP(w, r)
	})}
	finished := make(chan struct{})
	go func() { d.handle(context.Background(), jobAssignment); close(finished) }()
	c9Wait(t, b.started)
	io.WriteString(b.writer, c9Result)
	c9Wait(t, finished)
	b.mu.Lock()
	ops := strings.Join(b.ops, "\n")
	b.mu.Unlock()
	if !strings.Contains(ops, "signal act TERM\nsignal act KILL\ndestroy ci-0v1.att") {
		t.Fatal(ops)
	}
}

type c9ControlBox struct {
	*silentJobBox
	signalBlock  <-chan struct{}
	destroyBlock <-chan struct{}
	termExits    bool
}

func (b *c9ControlBox) Signal(ctx context.Context, h sandbox.Handle, process, signal string) error {
	b.silentJobBox.Signal(ctx, h, process, signal)
	if b.termExits && signal == "TERM" {
		b.done <- 0
	}
	if b.signalBlock != nil {
		<-b.signalBlock
	}
	return nil
}
func (b *c9ControlBox) Destroy(ctx context.Context, h sandbox.Handle) error {
	b.silentJobBox.Destroy(ctx, h)
	if b.destroyBlock != nil {
		<-b.destroyBlock
	}
	return nil
}
func TestC9TERMAcknowledgedSkipsKILL(t *testing.T) {
	d, b, _ := c9Daemon(t)
	d.box = &c9ControlBox{silentJobBox: b, termExits: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() { d.handle(ctx, jobAssignment); close(finished) }()
	c9Wait(t, b.started)
	cancel()
	c9Wait(t, finished)
	b.mu.Lock()
	ops := strings.Join(b.ops, "\n")
	b.mu.Unlock()
	if !strings.Contains(ops, "signal act TERM\ndestroy") || strings.Contains(ops, "KILL") {
		t.Fatal(ops)
	}
}
func TestC9BlockedControlQuarantinesWithinBound(t *testing.T) {
	d, b, _ := c9Daemon(t)
	unblock := make(chan struct{})
	defer close(unblock)
	d.box = &c9ControlBox{silentJobBox: b, signalBlock: unblock, destroyBlock: unblock}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	var keep bool
	go func() { keep = d.handle(ctx, jobAssignment); close(finished) }()
	c9Wait(t, b.started)
	cancel()
	c9Wait(t, finished)
	if keep || d.remainingCapacity() != 0 {
		t.Fatal("unproven Destroy was not quarantined")
	}
}
func TestC9EOFWithoutExitIsBounded(t *testing.T) {
	d, b, _ := c9Daemon(t)
	d.jobTiming.poll = time.Hour
	finished := make(chan struct{})
	go func() { d.handle(context.Background(), jobAssignment); close(finished) }()
	c9Wait(t, b.started)
	b.writer.Close()
	c9Wait(t, finished)
	b.mu.Lock()
	ops := strings.Join(b.ops, "\n")
	b.mu.Unlock()
	if !strings.Contains(ops, "signal act TERM\nsignal act KILL\ndestroy") {
		t.Fatal(ops)
	}
}
func TestC9StopDuringBlockedShipRequest(t *testing.T) {
	for _, kind := range []string{"status", "event"} {
		t.Run(kind, func(t *testing.T) {
			d, b, _ := c9Daemon(t)
			entered := make(chan struct{}, 1)
			d.client.HTTP.Transport = inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && kind == "event" {
					io.WriteString(w, `{"status":"running"}`)
					return
				}
				select {
				case entered <- struct{}{}:
				default:
				}
				<-r.Context().Done()
				w.WriteHeader(503)
			})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan struct{})
			go func() { d.handle(ctx, jobAssignment); close(finished) }()
			c9Wait(t, b.started)
			if kind == "event" {
				io.WriteString(b.writer, c9Result)
			}
			c9Wait(t, entered)
			cancel()
			c9Wait(t, finished)
			b.mu.Lock()
			ops := strings.Join(b.ops, "\n")
			b.mu.Unlock()
			if !strings.Contains(ops, "signal act TERM\nsignal act KILL\ndestroy") {
				t.Fatal(ops)
			}
		})
	}
}

func TestC9ProductionCleanupBudget(t *testing.T) {
	timing := (&Daemon{}).jobBounds()
	total := timing.poll + timing.request + 2*timing.signal + timing.term + timing.exit + timing.drain + timing.destroy
	if total != 104*time.Second || total > teardownBound {
		t.Fatalf("budget %v > %v", total, teardownBound)
	}
}
