package launcher

// Stage 01's core seams (runner/launcher/INTEGRATION.md §§3, 7.2, 7.4):
// a reservation left charged is named on the wire; the stop cuts a create
// short instead of waiting for it; teardowns the launcher starts on its
// own are reported unless they release in time; create steps run under the
// create's context, the start under its own bound.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A reservation the launcher cannot acknowledge and cannot withdraw (R8)
// stays charged; over the wire its answer names it — the reply stays
// empty, the error carries the identity — and the owner's destroy of that
// incarnation releases it.
func TestRetainedReserveIsNamedOnTheWire(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, newModelHost(t, tr), fsys)
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "w.sock")
	if err != nil {
		t.Fatal(err)
	}
	sv := &Server{Service: s, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test"}
	go sv.Serve(l)
	t.Cleanup(func() { l.Close() })
	cl, err := Dial(context.Background(), "w.sock", "0vd1")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	// correction-dependent (settled-admission ruling 01; INTEGRATION.md
	// §11.10): on the wire every reserve names its request, whose ledger
	// entry is renamed into place before the record, so the fault names the
	// record's rename — it named the first rename of any path
	fsys.inject(&fsFault{op: "rename", path: IDFor(cfg.IDPrefix, "0v1") + ".json", mode: "after", err: errInjected, times: 1})
	fsys.inject(&fsFault{op: "unlink", path: ".json", mode: "before", err: errInjected, times: 1})
	r, err := cl.Reserve(lockedReq("0v1", 1, 128))
	kept, retained := Retained(err)
	vms, _ := cl.List()
	if r.ID != "" || !errors.Is(err, ErrRetained) || !retained || len(vms) != 1 || !kept.Ref().Names(vms[0]) || kept.Incarnation == "" {
		tr.dump(t)
		t.Fatalf("A RESERVATION LEFT CHARGED WAS NOT NAMED: reply %+v, %v; named %+v; held %+v", r, err, kept, vms)
	}
	if _, err := cl.DestroyOf(kept.Ref()); err != nil {
		t.Fatalf("destroy of the named incarnation: %v", err)
	}
	if vms, _ := cl.List(); len(vms) != 0 || used(s)[2] != 0 {
		t.Fatalf("after the destroy: %+v %v", vms, used(s))
	}
}

// stallHost is the model host whose disk step for one id stands for a
// copy that does not end on its own: it ends when its context does.
type stallHost struct {
	*modelHost
	id      string
	entered chan struct{}
	once    sync.Once
}

func (h *stallHost) Bound(ctx context.Context) Host { return &stallView{stallHost: h, ctx: ctx} }

type stallView struct {
	*stallHost
	ctx context.Context
}

func (v *stallView) PrepareDisk(id string, img Image, total int) (string, error) {
	if id == v.id {
		v.once.Do(func() { close(v.entered) })
		select {
		case <-v.ctx.Done():
			return "", fmt.Errorf("disk copy cut short: %w", v.ctx.Err())
		case <-time.After(15 * time.Second): // a copy that takes this long
		}
	}
	return v.modelHost.PrepareDisk(id, img, total)
}

// The launcher's stop cuts a create in progress short (BeginStop) instead
// of waiting for it: the create rolls back, Shutdown tears the rest down,
// and the whole stop ends in a fraction of the create's own duration.
func TestBeginStopBoundsTheShutdown(t *testing.T) {
	tr := &tracer{}
	cfg := testConfig(t.TempDir())
	h := &stallHost{modelHost: newModelHost(t, tr), entered: make(chan struct{})}
	s := open(t, cfg, h, nil)
	running := mustReserve(t, s, owner1, lockedReq("0v2", 1, 128))
	if _, err := s.Create(owner1, running.ID); err != nil {
		t.Fatal(err)
	}
	slow := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	h.id = slow.ID
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, slow.ID); created <- err }()
	<-h.entered
	begun := time.Now()
	s.BeginStop("launcher stopping")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err := s.Shutdown(ctx)
	took := time.Since(begun)
	cerr := <-created
	if err != nil || took > 5*time.Second || cerr == nil || !strings.Contains(cerr.Error(), "rolled back") {
		t.Fatalf("THE STOP WAITED ON A CREATE IT SHOULD HAVE CUT SHORT: shutdown %v after %v; the create: %v", err, took, cerr)
	}
	if len(h.leaks(slow.ID)) != 0 || len(h.leaks(running.ID)) != 0 {
		t.Fatalf("left behind: %v %v", h.leaks(slow.ID), h.leaks(running.ID))
	}
}

// shortDeadline is a locked request whose deadline — a whole second, as
// records keep it — passes about two seconds from now, on the wall clock.
func shortDeadline(attempt string) (ReserveRequest, time.Time) {
	r := lockedReq(attempt, 1, 128)
	at := time.Now().Add(2 * time.Second).Truncate(time.Second).Add(time.Second)
	r.DeadlineUnix = at.Unix()
	return r, at
}

// Every teardown the launcher starts on its own and that does not release
// its record in time is reported — a halt (its `stopping` record not
// durable), a quarantine — with its reason; a release in time is not.
func TestSelfStartedTeardownsAreReported(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	cfg.MaxGuests, cfg.CleanupBound = 3, 2*time.Second
	s := open(t, cfg, h, fsys)
	var mu sync.Mutex
	var got []Outcome
	s.SetReport(func(o Outcome) {
		mu.Lock()
		got = append(got, o)
		mu.Unlock()
	})
	var ids []string
	var deadline time.Time
	for _, a := range []string{"0v1", "0v2", "0v3"} {
		req, at := shortDeadline(a)
		r := mustReserve(t, s, owner1, req)
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
		ids, deadline = append(ids, r.ID), at
	}
	halts, hangs, clean := ids[0], ids[1], ids[2]
	h.mu.Lock()
	h.hang[hangs] = true // survives TERM and KILL: quarantined
	h.mu.Unlock()
	fsys.inject(&fsFault{op: "open", path: halts + ".json.tmp", mode: "before", err: errInjected, times: 1}) // its stopping record
	time.Sleep(time.Until(deadline) + 50*time.Millisecond)
	s.ReapOnce() // the reaper's tick, right after the deadline
	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]Outcome{}
	for _, o := range got {
		kinds[o.ID] = o
	}
	if len(got) != 2 || kinds[halts].Kind != "halted" || !strings.Contains(kinds[halts].Reason, "not durable") || kinds[hangs].Kind != "quarantined" || kinds[hangs].By != "reaper" || kinds[halts].Late || kinds[hangs].Late {
		t.Fatalf("A TEARDOWN THAT DID NOT RELEASE WAS NOT REPORTED (or a release in time was): %+v", got)
	}
	if _, ok := held(s, clean); ok {
		t.Fatal("the clean one was not released")
	}
}

// A teardown that begins after its cleanup deadline has passed — here the
// reaper reaches a record long after its deadline, as when the launcher was
// not running — is LATE: its cleanup still runs, with one allowance from
// its start, and it is reported and says so in its reason; the obligation
// was not met for it, and nothing hides that — the record stays an
// incident, quarantined and charged (recovery ruling A; recovery_test.go
// for the incident itself).
func TestLateTeardownsAreReported(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, h, nil)
	var mu sync.Mutex
	var got []Outcome
	s.SetReport(func(o Outcome) {
		mu.Lock()
		got = append(got, o)
		mu.Unlock()
	})
	r := mustReserve(t, s, owner1, expiring("0v1", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	clock.advance(expire) // a minute past its deadline plus its allowance
	s.ReapOnce()
	rec, ok := held(s, r.ID)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !got[0].Late || got[0].Kind != "quarantined" || !ok || !strings.Contains(rec.Reason, "LATE") {
		t.Fatalf("A LATE TEARDOWN WAS NOT REPORTED AS LATE: reports %+v, record %+v %v", got, rec, ok)
	}
}

// ctxHost records the context each create step ran under; its cgroup step
// for one id waits for that context's end.
type ctxHost struct {
	*modelHost
	block   string
	entered chan struct{}
	mu      sync.Mutex
	seen    map[string]context.Context // "op id" -> its context
}

func (h *ctxHost) Bound(ctx context.Context) Host { return &ctxView{ctxHost: h, ctx: ctx} }

type ctxView struct {
	*ctxHost
	ctx context.Context
}

func (v *ctxView) note(op, id string) {
	v.mu.Lock()
	v.seen[op+" "+id] = v.ctx
	v.mu.Unlock()
}
func (v *ctxView) CreateCgroup(id string, cpus, mem int) error {
	v.note("cgroup", id)
	if id == v.block {
		close(v.entered)
		select {
		case <-v.ctx.Done():
			return fmt.Errorf("cgroup step cut short: %w", v.ctx.Err())
		case <-time.After(8 * time.Second): // a step that takes this long
		}
	}
	return v.modelHost.CreateCgroup(id, cpus, mem)
}

func (v *ctxView) StartVM(id string, spec VMSpec) (int, error) {
	v.note("start", id)
	return v.modelHost.StartVM(id, spec)
}

// A create's cancellable steps run under the create's context: its
// deadline is the reservation's, and an owner's destroy ends it at once,
// so the destroy never waits on a step that does not end by itself. The
// start runs under its own bound (startBound); a trigger cuts it only a
// quarter of the allowance after it (trigger_test.go).
func TestCreateStepsRunUnderTheCreatesContext(t *testing.T) {
	h := &ctxHost{modelHost: newModelHost(t, &tracer{}), entered: make(chan struct{}), seen: map[string]context.Context{}}
	s := open(t, testConfig(t.TempDir()), h, nil)
	ok := mustReserve(t, s, owner1, lockedReq("0v2", 1, 128))
	started := time.Now()
	if _, err := s.Create(owner1, ok.ID); err != nil {
		t.Fatal(err)
	}
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	rec, _ := held(s, r.ID)
	h.block = r.ID
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-h.entered
	begun := time.Now()
	derr := s.Destroy(owner1, r.ID)
	took := time.Since(begun)
	<-created
	h.mu.Lock()
	step, start := h.seen["cgroup "+r.ID], h.seen["start "+ok.ID]
	h.mu.Unlock()
	d, hasDeadline := step.Deadline()
	// the observation first: a destroy made to wait begins its rollback
	// after its deadline, a late recovery whose error would otherwise hide it
	if took > 5*time.Second || step.Err() == nil || !hasDeadline || d.Unix() != rec.DeadlineUnix {
		t.Fatalf("THE DESTROY WAITED ON A STEP IT SHOULD HAVE CUT SHORT (after %v): the step's context %v, deadline %v (want the reservation's %d); destroy: %v", took, step.Err(), d, rec.DeadlineUnix, derr)
	}
	if derr != nil {
		t.Fatalf("destroy: %v", derr)
	}
	if sd, bounded := start.Deadline(); !bounded || sd.Before(started.Add(startBound-time.Second)) || sd.After(time.Now().Add(startBound)) {
		t.Fatalf("the start's own bound: deadline %v (want about %v after %v)", sd, startBound, started)
	}
}
