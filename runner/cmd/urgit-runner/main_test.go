package main

// The operator's recovery of the daemon's retentions (recovery ruling A;
// runner/launcher/INTEGRATION.md §8.4) through its entry points, against
// the real launcher core on a private unix socket over a nonexecuting
// host, and a recorded stand-in for the Docker objects.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/state"
)

const daemonID = "0v1.daemon"

// quarantined leaves attempt quarantined in the launcher under this
// daemon: reserved, created, and a destroy whose jail removal failed.
func quarantined(t *testing.T, h *launchertest.Host, srv *launchertest.Server, digest, attempt string) launcher.Record {
	t.Helper()
	cl, err := launcher.Dial(context.Background(), srv.Socket, daemonID)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	r, err := cl.Reserve(launcher.ReserveRequest{Attempt: attempt, Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked", Label: "owner/repo · ci.yml · " + attempt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Create(r.Ref()); err != nil {
		t.Fatal(err)
	}
	h.SetFail("rmjail", launchertest.ErrInjected)
	defer h.SetFail("rmjail", nil)
	if q, err := cl.DestroyOf(r.Ref()); err == nil || !q {
		t.Fatalf("fixture: %v %v", err, q)
	}
	for _, rec := range srv.Held(t, daemonID) {
		if rec.ID == r.ID {
			return rec
		}
	}
	t.Fatal("fixture: not held")
	return launcher.Record{}
}

// launcherRelease is the launcher operator's retry and release of rec.
func launcherRelease(t *testing.T, srv *launchertest.Server, rec launcher.Record) {
	t.Helper()
	in, err := srv.Service.RetryCleanup(rec.Selection())
	if err != nil {
		t.Fatalf("the launcher's retry: %v", err)
	}
	if err := srv.Service.Release(in.Selection); err != nil {
		t.Fatalf("the launcher's release: %v", err)
	}
}

// retention is the daemon's entry for rec, as retain records it: its exact
// incarnation, the token included (INTEGRATION.md §11.1).
func retention(rec launcher.Record) state.Quarantine {
	return state.Quarantine{Handle: "ci-" + rec.Attempt, Reason: "teardown failed", At: time.Now().Unix(), Backend: "microvm", Attempt: rec.Attempt,
		VM: rec.ID, Incarnation: rec.Incarnation, CID: rec.CID, Created: rec.Created, Label: rec.Label}
}

func saveState(t *testing.T, path string, qs ...state.Quarantine) {
	t.Helper()
	if err := state.Save(path, &state.State{DaemonID: daemonID, Bearer: "b", ShipURL: "http://ship.test", Quarantined: qs}); err != nil {
		t.Fatal(err)
	}
}

func loaded(t *testing.T, path string) *state.State {
	t.Helper()
	st, err := state.Load(path)
	if err != nil || st == nil {
		t.Fatalf("state: %v", err)
	}
	return st
}

func handles(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, q := range loaded(t, path).Quarantined {
		out = append(out, q.Handle)
	}
	return out
}

// fakeDocker is the Docker objects of a retention as a set of names.
type fakeDocker struct {
	objects map[string]bool
	fail    error
}

func (f *fakeDocker) Leftovers(ctx context.Context, h sandbox.Handle) ([]string, error) {
	var left []string
	for _, o := range []string{"container " + h.Container, "volume " + h.Volume, "network " + h.Network} {
		if f.objects[o] {
			left = append(left, o)
		}
	}
	return left, nil
}

func (f *fakeDocker) Destroy(ctx context.Context, h sandbox.Handle) error {
	if f.fail != nil {
		return f.fail
	}
	for _, o := range []string{"container " + h.Container, "volume " + h.Volume, "network " + h.Network} {
		delete(f.objects, o)
	}
	return nil
}

func dockerRetention() state.Quarantine {
	return state.Quarantine{Handle: "ci-0v8.att", Reason: "teardown failed", At: time.Now().Unix(), Backend: "docker-rootless", Attempt: "0v8.att",
		Network: "ci-0v8.att", Volume: "ci-0v8.att-work", Container: "ci-0v8.att", Label: "owner/repo · ci.yml · docker"}
}

// A microvm retention is released only by the operator, with the daemon
// stopped, and only once the launcher's list shows the exact incarnation
// it names gone — never its retry, which is the launcher operator's; a new
// incarnation of the same attempt does not block it, and the release is
// reported only once durable, the entry kept as evidence.
func TestRecoverReleasesAMicrovmRetentionOnlyOnTheLaunchersProof(t *testing.T) {
	h := launchertest.NewHost()
	_, digest := launchertest.Image(t, "img")
	srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
	rec := quarantined(t, h, srv, digest, "0v9.att")
	statePath := filepath.Join(t.TempDir(), "state", "state.json")
	q := retention(rec)
	saveState(t, statePath, q)
	cfg := &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, StateFile: statePath}
	rc := recovery{cfg: cfg, records: launcherRecords, now: time.Now}
	var out strings.Builder

	// the daemon runs: it holds the state file
	running, err := state.Acquire(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if code := rc.act(q.Selection(), "release", &out); code != 2 || len(handles(t, statePath)) != 1 {
		t.Fatalf("A RELEASE BESIDE A RUNNING DAEMON: exit %d\n%s", code, out.String())
	}
	running.Release()

	// the launcher still holds it: refused, and the refusal names the
	// launcher operator's own step, with its selection
	out.Reset()
	if code := rc.act(q.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 1 {
		t.Fatalf("A RETENTION THE LAUNCHER STILL HOLDS WAS RELEASED: exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "urgit-vm-launcher recover -select "+rec.ID) {
		t.Fatalf("the refusal must say what comes first: %s", out.String())
	}
	out.Reset()
	if code := rc.act(q.Selection(), "retry", &out); code != 1 || !strings.Contains(out.String(), "the launcher operator's") {
		t.Fatalf("a runner retry of a microvm retention: exit %d\n%s", code, out.String())
	}

	// an unreachable launcher proves nothing
	lost := *cfg
	lost.LauncherSocket = filepath.Join(t.TempDir(), "no.sock")
	out.Reset()
	if code := (recovery{cfg: &lost, records: launcherRecords, now: time.Now}).act(q.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 1 {
		t.Fatalf("a release without the launcher's word: exit %d\n%s", code, out.String())
	}

	// the launcher's operator releases its record; the same attempt is then
	// reserved again — a new incarnation, which the old retention is not
	launcherRelease(t, srv, rec)
	cl, err := launcher.Dial(context.Background(), srv.Socket, daemonID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v9.att", Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	cl.Close()
	if err != nil || again.ID != rec.ID || again.CID == rec.CID {
		t.Fatalf("fixture: a new incarnation %+v %v", again, err)
	}
	if v := rc.inspect(q, daemonID); !v.Proven {
		t.Fatalf("THE EXACT INCARNATION IS GONE, BUT ITS RELEASE WAS REFUSED: %s", v.Why)
	}

	// the save cannot be made durable: not reported released
	dir := filepath.Dir(statePath)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	code := rc.act(q.Selection(), "release", &out)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if code != 2 || len(handles(t, statePath)) != 1 {
		t.Fatalf("A RELEASE REPORTED WITHOUT A DURABLE SAVE: exit %d\n%s", code, out.String())
	}

	out.Reset()
	if code := rc.act(q.Selection(), "release", &out); code != 0 || len(handles(t, statePath)) != 0 {
		t.Fatalf("the exact incarnation is gone: exit %d\n%s", code, out.String())
	}
	st := loaded(t, statePath)
	if len(st.Released) != 1 || st.Released[0].VM != rec.ID || st.Released[0].ReleasedAt == 0 {
		t.Fatalf("THE RELEASED RETENTION WAS NOT KEPT AS EVIDENCE: %+v", st.Released)
	}
	if recs := srv.Held(t, daemonID); len(recs) != 1 || recs[0].CID != again.CID {
		t.Fatalf("the new incarnation must stay the launcher's: %+v", recs)
	}
}

// A retention recorded before stage 01 (the handle only) on a microvm
// runner is released only when the launcher holds no record of its
// attempt.
//
// Policy-dependent (settled-admission ruling 01; INTEGRATION.md §11.10):
// before the ruling, once the launcher had released its attempt's record,
// the older entry was released on that absence. It names no reserve
// request, so nothing can settle it, and an empty inventory is no
// settlement: it stays withheld after the launcher's release too.
func TestRecoverOfAnOlderEntryAsksTheLauncherByAttempt(t *testing.T) {
	h := launchertest.NewHost()
	_, digest := launchertest.Image(t, "img")
	srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
	rec := quarantined(t, h, srv, digest, "0v7.att")
	statePath := filepath.Join(t.TempDir(), "state.json")
	older := state.Quarantine{Handle: "ci-0v7.att", Reason: "teardown failed"}
	saveState(t, statePath, older)
	rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, StateFile: statePath}, records: launcherRecords, now: time.Now}
	var out strings.Builder
	if code := rc.act(older.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 1 {
		t.Fatalf("AN OLDER ENTRY WAS RELEASED WHILE ITS ATTEMPT IS HELD: exit %d\n%s", code, out.String())
	}
	// the refusal names what the launcher still holds of the attempt: since
	// settled-admission ruling 01 the release is refused whatever the list
	// shows, and what the attempt's match still decides is this (full-11:
	// X37's mutant passed on the refusal alone)
	if !strings.Contains(out.String(), rec.ID) {
		t.Fatalf("THE RESERVATION THE LAUNCHER HOLDS OF AN OLDER ENTRY'S ATTEMPT WAS NOT NAMED:\n%s", out.String())
	}
	launcherRelease(t, srv, rec)
	out.Reset()
	if code := rc.act(older.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 1 || !strings.Contains(out.String(), "nothing can settle it") {
		t.Fatalf("AN ENTRY WITHOUT ITS REQUEST WAS RELEASED ON AN ABSENCE: exit %d\n%s", code, out.String())
	}
}

// A Docker retention's objects are the runner's own: its retry removes
// them by name, records the attempt and keeps the retention; its release
// waits for none of them to remain, looked at again at release time, and
// names the revision inspected.
func TestRecoverRetriesAndReleasesADockerRetention(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	q := dockerRetention()
	saveState(t, statePath, q)
	docker := &fakeDocker{objects: map[string]bool{"container ci-0v8.att": true, "network ci-0v8.att": true}}
	rc := recovery{cfg: &config.Config{Sandbox: "docker-rootless", StateFile: statePath}, docker: docker, now: time.Now}
	var out strings.Builder
	if code := rc.act(q.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 1 {
		t.Fatalf("A DOCKER RETENTION WITH OBJECTS LEFT WAS RELEASED: exit %d\n%s", code, out.String())
	}
	docker.fail = errors.New("injected: container busy")
	out.Reset()
	if code := rc.act(q.Selection(), "retry", &out); code != 1 {
		t.Fatalf("a failed retry: exit %d\n%s", code, out.String())
	}
	docker.fail = nil
	out.Reset()
	if code := rc.act(q.Selection(), "retry", &out); code != 0 {
		t.Fatalf("the retry: exit %d\n%s", code, out.String())
	}
	st := loaded(t, statePath)
	if len(st.Quarantined) != 1 || len(st.Quarantined[0].Attempts) != 2 || st.Quarantined[0].Attempts[0].Result != "unresolved" ||
		st.Quarantined[0].Attempts[1].Result != "resolved" || st.Quarantined[0].Rev != 2 {
		t.Fatalf("A DOCKER RETRY WAS NOT RECORDED, OR RELEASED THE RETENTION: %+v", st.Quarantined)
	}
	out.Reset()
	if code := rc.act(q.Selection(), "release", &out); code != 1 || !strings.Contains(out.String(), "stale") {
		t.Fatalf("A STALE SELECTION WAS RELEASED: exit %d\n%s", code, out.String())
	}
	// an object that turns up again before the release refuses it
	docker.objects["volume ci-0v8.att-work"] = true
	out.Reset()
	if code := rc.act(st.Quarantined[0].Selection(), "release", &out); code != 1 {
		t.Fatalf("A RELEASE TRUSTED AN EARLIER LOOK: exit %d\n%s", code, out.String())
	}
	delete(docker.objects, "volume ci-0v8.att-work")
	out.Reset()
	if code := rc.act(st.Quarantined[0].Selection(), "release", &out); code != 0 || len(handles(t, statePath)) != 0 {
		t.Fatalf("the release: exit %d\n%s", code, out.String())
	}
	if st := loaded(t, statePath); len(st.Released) != 1 || len(st.Released[0].Attempts) != 2 {
		t.Fatalf("the released retention's evidence: %+v", st.Released)
	}
}

// The session lists the retentions by job label and identity, shows the
// one selected, refuses its release while its objects remain, retries its
// cleanup, does not release it — proven released now — on an answer other
// than "release", and releases it confirmed; the other is not touched.
func TestRecoverSessionSelectsByNumber(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	other := dockerRetention()
	other.Handle, other.Attempt, other.Network, other.Volume, other.Container, other.Label = "ci-0v5.att", "0v5.att", "ci-0v5.att", "ci-0v5.att-work", "ci-0v5.att", "owner/repo · ci.yml · other"
	q := dockerRetention()
	saveState(t, statePath, other, q)
	docker := &fakeDocker{objects: map[string]bool{"container ci-0v8.att": true, "container ci-0v5.att": true}}
	rc := recovery{cfg: &config.Config{Sandbox: "docker-rootless", StateFile: statePath}, docker: docker, now: time.Now}
	var out strings.Builder
	code := rc.interactive(strings.NewReader("2\nR\nrelease\nr\nR\nnope\nR\nrelease\nq\n"), &out)
	text := out.String()
	for _, want := range []string{"[1] owner/repo · ci.yml · other", "[2] owner/repo · ci.yml · docker", "select      " + q.Handle + "/docker-rootless/",
		"release     refused: its objects remain", "release refused: its objects remain", "cleanup retried: resolved", "release     allowed",
		"not released", "released ci-0v8.att"} {
		if !strings.Contains(text, want) {
			t.Fatalf("THE SESSION DOES NOT SHOW %q (exit %d):\n%s", want, code, text)
		}
	}
	if i, j := strings.Index(text, "release     allowed"), strings.Index(text, "not released"); j < i {
		t.Fatalf("the unconfirmed release was not asked while the retention was releasable:\n%s", text)
	}
	if left := handles(t, statePath); code != 0 || !slices.Equal(left, []string{"ci-0v5.att"}) || !docker.objects["container ci-0v5.att"] {
		t.Fatalf("after the session: exit %d, left %v, objects %v", code, left, docker.objects)
	}
}

// Two retentions of one handle — two incarnations of an attempt's launcher
// reservation — are two selections: the one released is exactly the one
// selected (the other's incarnation is still the launcher's), and a replay
// of that selection is stale, never the other's.
func TestRecoverSelectsTheExactIncarnation(t *testing.T) {
	h := launchertest.NewHost()
	_, digest := launchertest.Image(t, "img")
	srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
	// an earlier incarnation of the attempt the launcher released: its own
	// token, and its evidence — fixture adaptation (late-accounting ruling
	// 01; INTEGRATION.md §11.8): before the ruling it was a made-up identity
	// the launcher had never held, whose absence was taken for its release
	cl, err := launcher.Dial(context.Background(), srv.Socket, daemonID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v6.att", Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 10, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.DestroyOf(first.Ref()); err != nil {
		t.Fatal(err)
	}
	cl.Close()
	rec := quarantined(t, h, srv, digest, "0v6.att")
	held := retention(rec)
	gone := retention(launcher.Record{ID: first.ID, Incarnation: first.Incarnation, Attempt: "0v6.att", CID: first.CID, Created: first.Created, Label: rec.Label})
	gone.At = held.At - 60
	statePath := filepath.Join(t.TempDir(), "state.json")
	saveState(t, statePath, held, gone)
	rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, StateFile: statePath}, records: launcherRecords, now: time.Now}
	var out strings.Builder
	if code := rc.act(gone.Selection(), "release", &out); code != 0 {
		t.Fatalf("THE SELECTION REACHED ANOTHER INCARNATION: exit %d\n%s", code, out.String())
	}
	st := loaded(t, statePath)
	if len(st.Quarantined) != 1 || st.Quarantined[0].CID != held.CID || len(st.Released) != 1 || st.Released[0].CID != gone.CID {
		t.Fatalf("THE SELECTION REACHED ANOTHER INCARNATION: kept %+v, released %+v", st.Quarantined, st.Released)
	}
	out.Reset()
	if code := rc.act(gone.Selection(), "release", &out); code != 1 || !strings.Contains(out.String(), "stale") {
		t.Fatalf("A REPLAYED SELECTION WAS NOT STALE: exit %d\n%s", code, out.String())
	}
	if st := loaded(t, statePath); len(st.Quarantined) != 1 || st.Quarantined[0].CID != held.CID {
		t.Fatalf("A REPLAY REACHED ANOTHER INCARNATION: %+v", st.Quarantined)
	}
}
