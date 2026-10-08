package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var errInjected = errors.New("injected EIO")

// faultOps is osOps with a trace, and a fault before or after one step.
type faultOps struct {
	osOps
	trace []string
	fail  string // the step that fails
	after bool   // it acted, then failed
}

func (f *faultOps) step(name string, real func() error) error {
	f.trace = append(f.trace, name)
	if f.fail == name && !f.after {
		return errInjected
	}
	if err := real(); err != nil {
		return err
	}
	if f.fail == name {
		return errInjected
	}
	return nil
}

func (f *faultOps) MkdirAll(dir string) error {
	return f.step("mkdir", func() error { return f.osOps.MkdirAll(dir) })
}
func (f *faultOps) Create(path string) (writable, error) {
	var w writable
	err := f.step("create", func() (err error) { w, err = f.osOps.Create(path); return })
	if err != nil {
		if w != nil {
			w.Close()
		}
		return nil, err
	}
	return &faultFile{w, f}, nil
}
func (f *faultOps) Rename(from, to string) error {
	return f.step("rename", func() error { return f.osOps.Rename(from, to) })
}
func (f *faultOps) SyncDir(dir string) error {
	return f.step("sync-dir", func() error { return f.osOps.SyncDir(dir) })
}

type faultFile struct {
	w writable
	f *faultOps
}

func (x *faultFile) Write(p []byte) (int, error) {
	n := 0
	err := x.f.step("write", func() (err error) { n, err = x.w.Write(p); return })
	return n, err
}
func (x *faultFile) Chmod(m os.FileMode) error {
	return x.f.step("chmod", func() error { return x.w.Chmod(m) })
}
func (x *faultFile) Sync() error  { return x.f.step("sync", x.w.Sync) }
func (x *faultFile) Close() error { return x.f.step("close", x.w.Close) }

// A save is write-temp, fsync, rename, fsync-directory, and says nil only
// when all of them did (INTEGRATION.md §5); each failure names its step
// and whether the file may already hold the new content.
func TestSaveIsDurableAndReportsEveryFailure(t *testing.T) {
	st := &State{DaemonID: "0v1.d", Bearer: "b", Quarantined: []Quarantine{{Handle: "ci-a", Backend: "microvm", VM: "t-a", CID: 3, Created: 7}}}
	ops := &faultOps{}
	p := filepath.Join(t.TempDir(), "d", "state.json")
	if err := save(ops, p, st); err != nil {
		t.Fatal(err)
	}
	if want := "mkdir create write chmod sync close rename sync-dir"; strings.Join(ops.trace, " ") != want {
		t.Fatalf("SAVE ORDER %v, want %s", ops.trace, want)
	}
	if got, err := Load(p); err != nil || !reflect.DeepEqual(got, st) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for _, c := range []struct {
		fail      string
		after     bool
		uncertain bool
		changed   bool
	}{
		{"create", false, false, false}, {"write", false, false, false}, {"sync", false, false, false},
		{"close", true, false, false}, {"rename", false, true, false}, {"rename", true, true, true}, {"sync-dir", false, true, true},
	} {
		next := *st
		next.Quarantined = append(append([]Quarantine(nil), st.Quarantined...), Quarantine{Handle: "ci-b"})
		ops := &faultOps{fail: c.fail, after: c.after}
		err := save(ops, p, &next)
		var se *SaveError
		if !errors.As(err, &se) || se.Step != c.fail || se.Uncertain != c.uncertain || !errors.Is(err, errInjected) {
			t.Fatalf("%s (after %v): A FAILED SAVE WAS NOT REPORTED AS SUCH: %v", c.fail, c.after, err)
		}
		got, _ := Load(p)
		if changed := len(got.Quarantined) == 2; changed != c.changed {
			t.Fatalf("%s (after %v): file changed %v, want %v", c.fail, c.after, changed, c.changed)
		}
		if err := save(&faultOps{}, p, st); err != nil { // back to one entry
			t.Fatal(err)
		}
	}
}

// One process holds a state file at a time: a second holder — the
// operator's clear beside a running daemon — is refused, and the lock
// comes back when released.
func TestStateLockIsExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s", "state.json")
	a, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := Acquire(p); !errors.Is(err, ErrLocked) {
		b.Release()
		t.Fatalf("A SECOND HOLDER WAS GIVEN THE STATE FILE: %v", err)
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	b, err := Acquire(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	b.Release()
}

// A retention is one per identity: the same launcher incarnation twice is
// one; two incarnations of an attempt are two; an entry an older daemon
// wrote (the handle only) is every retention of its handle and is
// completed, never counted beside it; nothing merges across backends.
func TestRetentionsAreOnePerIdentity(t *testing.T) {
	inc1 := Quarantine{Handle: "ci-a", Backend: "microvm", Attempt: "a", VM: "t-a", CID: 3, Created: 10, Reason: "r1"}
	inc2 := Quarantine{Handle: "ci-a", Backend: "microvm", Attempt: "a", VM: "t-a", CID: 4, Created: 20}
	legacy := Quarantine{Handle: "ci-a", Reason: "teardown failed", At: 5}
	docker := Quarantine{Handle: "ci-a", Backend: "docker-rootless", Network: "ci-a", Volume: "ci-a-work", Container: "ci-a"}
	s := &State{}
	if !s.Retain(inc1) || s.Retain(inc1) || !s.Retain(inc2) || !s.Retain(docker) || len(s.Quarantined) != 3 {
		t.Fatalf("retain: %+v", s.Quarantined)
	}
	s = &State{Quarantined: []Quarantine{legacy, legacy, inc1}}
	if n := s.Dedupe(); n != 2 || len(s.Quarantined) != 1 {
		t.Fatalf("DUPLICATE ENTRIES STILL CHARGED: merged %d, left %+v", n, s.Quarantined)
	}
	if q := s.Quarantined[0]; q.VM != "t-a" || q.CID != 3 || q.Created != 10 || q.Backend != "microvm" || q.Reason != "teardown failed" || q.At != 5 {
		t.Fatalf("the older entry was not completed with the identity: %+v", q)
	}
	s = &State{Quarantined: []Quarantine{legacy}}
	if s.Retain(inc2) || len(s.Quarantined) != 1 || s.Quarantined[0].CID != 4 {
		t.Fatalf("an older entry was charged beside its identity: %+v", s.Quarantined)
	}
	unknown := Quarantine{Handle: "ci-b", Backend: "microvm", Attempt: "b"}
	s = &State{Quarantined: []Quarantine{unknown}}
	if s.Retain(Quarantine{Handle: "ci-b", Backend: "microvm", Attempt: "b", VM: "t-b", CID: 9, Created: 1}) || s.Quarantined[0].VM != "t-b" {
		t.Fatalf("an identity learnt later was charged again: %+v", s.Quarantined)
	}
}

// Stage 01, independent review 01 (R2): a microvm retention's identity is
// its launcher incarnation's token (runner/launcher/INTEGRATION.md §11.1).
// Two incarnations with the same record, cid and creation time — an empty
// launcher restart within one second — are two retentions; an entry
// without a token (retained against a record written before tokens) is
// never the same as one with a token; completion carries the token; and a
// selection names it, so it reaches exactly that entry.
func TestRetentionIdentityIsTheIncarnationToken(t *testing.T) {
	tokA, tokB := strings.Repeat("a", 32), strings.Repeat("b", 32)
	a := Quarantine{Handle: "ci-a", Backend: "microvm", Attempt: "a", VM: "t-a", Incarnation: tokA, CID: 3, Created: 10, At: 1}
	b := Quarantine{Handle: "ci-a", Backend: "microvm", Attempt: "a", VM: "t-a", Incarnation: tokB, CID: 3, Created: 10, At: 2}
	legacy := Quarantine{Handle: "ci-a", Backend: "microvm", Attempt: "a", VM: "t-a", CID: 3, Created: 10, At: 3}
	s := &State{}
	if !s.Retain(a) || !s.Retain(b) || s.Retain(a) || len(s.Quarantined) != 2 {
		t.Fatalf("TWO INCARNATIONS WITH ONE TUPLE WERE ONE RETENTION: %+v", s.Quarantined)
	}
	if !s.Retain(legacy) || len(s.Quarantined) != 3 {
		t.Fatalf("A TOKEN-LESS ENTRY WAS TAKEN FOR A TOKENED ONE: %+v", s.Quarantined)
	}
	unknown := Quarantine{Handle: "ci-c", Backend: "microvm", Attempt: "c"}
	s = &State{Quarantined: []Quarantine{unknown}}
	if s.Retain(Quarantine{Handle: "ci-c", Backend: "microvm", Attempt: "c", VM: "t-c", Incarnation: tokA, CID: 9, Created: 1}) || s.Quarantined[0].Incarnation != tokA {
		t.Fatalf("THE IDENTITY LEARNT LATER LOST ITS TOKEN: %+v", s.Quarantined)
	}
	s = &State{Quarantined: []Quarantine{a, b}}
	i, err := s.Find(b.Selection(), true)
	if err != nil || i != 1 {
		t.Fatalf("A SELECTION REACHED ANOTHER INCARNATION: %d %v", i, err)
	}
	for _, sel := range []string{"ci-a/microvm/t-a/3/10/2/0", strings.Replace(b.Selection(), tokB, strings.Repeat("c", 32), 1)} {
		if i, err := s.Find(sel, true); err == nil {
			t.Fatalf("A SELECTION WITHOUT, OR WITH ANOTHER, TOKEN FOUND ENTRY %d: %q", i, sel)
		}
	}
}
