package state

// The execution ledger (assignment-replay ruling 01, Q14; runner/launcher/
// INTEGRATION.md §11.14): one record per attempt taken, created exclusively
// under the attempt's canonical spelling, made durable, and never replaced,
// deleted or expired. A record whose write fails is an error naming its
// step and whether its file exists; the filesystem full or refusing, every
// record there already stays. What the ledger cannot read is no answer.
// Over real files in private directories; a fault is injected before or
// after the real call (ledgerFaults).

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"urgit/runner/internal/sig"
)

// ledgerFaults is osOps for the ledger with a trace, and a fault — err —
// before or after one step.
type ledgerFaults struct {
	osOps
	trace []string
	fail  string // the step that fails
	after bool   // it acted, then failed
	err   error
}

func (f *ledgerFaults) step(name string, real func() error) error {
	f.trace = append(f.trace, name)
	if f.fail == name && !f.after {
		return f.err
	}
	if err := real(); err != nil {
		return err
	}
	if f.fail == name {
		return f.err
	}
	return nil
}

func (f *ledgerFaults) CreateNew(path string) (writable, error) {
	var w writable
	err := f.step("create", func() (err error) { w, err = f.osOps.CreateNew(path); return })
	if err != nil {
		if w != nil {
			w.Close()
		}
		return nil, err
	}
	return &ledgerFile{w, f}, nil
}

func (f *ledgerFaults) SyncDir(dir string) error {
	return f.step("sync-dir", func() error { return f.osOps.SyncDir(dir) })
}

type ledgerFile struct {
	w writable
	f *ledgerFaults
}

func (x *ledgerFile) Write(p []byte) (int, error) {
	n := 0
	err := x.f.step("write", func() (err error) { n, err = x.w.Write(p); return })
	return n, err
}
func (x *ledgerFile) Chmod(m os.FileMode) error { return x.w.Chmod(m) }
func (x *ledgerFile) Sync() error               { return x.f.step("sync", x.w.Sync) }
func (x *ledgerFile) Close() error              { return x.f.step("close", x.w.Close) }

// newLedger is a ledger opened beside a state file in a private directory.
func newLedger(t *testing.T) *Ledger {
	t.Helper()
	l := LedgerFor(filepath.Join(t.TempDir(), "state.json"))
	if _, err := l.Open(History{Since: 1000, Enrolled: true, StateFormat: CurrentFormat}, false); err != nil {
		t.Fatal(err)
	}
	return l
}

// attemptNumbered is the canonical spelling of attempt n.
func attemptNumbered(n int64) string { return sig.FormatUV(big.NewInt(n)) }

// contents is everything under dir: a file's bytes, "<dir>", or "-> " and
// a symlink's target.
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = "-> " + target
		case e.IsDir():
			out[rel] = "<dir>"
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// kept says what of before is missing or changed in after.
func kept(before, after map[string]string) []string {
	var lost []string
	for k, v := range before {
		if got, ok := after[k]; !ok || got != v {
			lost = append(lost, k)
		}
	}
	return lost
}

// A take is made durable in order — created, written, synced, closed, its
// directory synced — and says what was taken. A second take of the attempt,
// with another nonce, is refused and replaces nothing; a finish is recorded
// once, and a second one is no error and replaces nothing.
func TestATakenRecordIsWrittenOnceAndNeverReplaced(t *testing.T) {
	l := newLedger(t)
	ops := &ledgerFaults{}
	l.ops = ops
	if err := l.Take(Execution{Attempt: "0v5cmd", At: 7, ID: "0v1.asg", Kind: "job", Expiry: 99, Nonce: "0v7.a"}); err != nil {
		t.Fatal(err)
	}
	if want := "create write sync close sync-dir"; strings.Join(ops.trace, " ") != want {
		t.Fatalf("A RECORD WAS NOT MADE DURABLE IN ORDER: %v, want %s", ops.trace, want)
	}
	path := filepath.Join(l.Dir, "taken", "0v5cmd")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e Execution
	if err := json.Unmarshal(first, &e); err != nil || e.Attempt != "0v5cmd" || e.Nonce != "0v7.a" || e.Expiry != 99 || e.ID != "0v1.asg" {
		t.Fatalf("the record does not say what was taken: %q %v", first, err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("a record is private: %v %v", info.Mode(), err)
	}
	if err := l.Take(Execution{Attempt: "0v5cmd", At: 8, Nonce: "0v8.fresh"}); !errors.Is(err, ErrTaken) {
		t.Fatalf("A SECOND TAKE OF AN ATTEMPT WAS NOT REFUSED: %v", err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(again, first) {
		t.Fatalf("A TAKEN RECORD WAS REPLACED: %q, was %q", again, first)
	}
	if taken, finished, err := l.Has("0v5cmd"); !taken || finished || err != nil {
		t.Fatalf("a taken attempt is taken, not finished: %v %v %v", taken, finished, err)
	}
	if err := l.Finish("0v5cmd", 9); err != nil {
		t.Fatal(err)
	}
	if taken, finished, err := l.Has("0v5cmd"); !taken || !finished || err != nil {
		t.Fatalf("A FINISHED ATTEMPT IS NOT FINISHED: %v %v %v", taken, finished, err)
	}
	done, err := os.ReadFile(filepath.Join(l.Dir, "finished", "0v5cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Finish("0v5cmd", 10); err != nil {
		t.Fatalf("a second finish is no error: %v", err)
	}
	if again, _ := os.ReadFile(filepath.Join(l.Dir, "finished", "0v5cmd")); !bytes.Equal(again, done) {
		t.Fatalf("A FINISHED RECORD WAS REPLACED: %q, was %q", again, done)
	}
	if taken, finished, err := l.Has("0v6cmd"); taken || finished || err != nil {
		t.Fatalf("AN ATTEMPT NEVER TAKEN COUNTS AS TAKEN: %v %v %v", taken, finished, err)
	}
}

// A record is named by its attempt's canonical spelling, and nothing else:
// another spelling, a path, a name of the ledger's own is refused before
// anything is written — and, asked of, answers taken, with the error.
func TestARecordIsNamedByItsAttemptsCanonicalSpellingOnly(t *testing.T) {
	l := newLedger(t)
	before := contents(t, l.Dir)
	for _, name := range []string{"0v5.cmd", "0v05cmd", "../0v5cmd", "", "HISTORY", "taken", "0v5cmd/x", "5cmd"} {
		var le *LedgerError
		if err := l.Take(Execution{Attempt: name}); !errors.As(err, &le) || le.Step != "name" {
			t.Fatalf("A RECORD WAS TAKEN UNDER A NAME THAT IS NO ATTEMPT'S CANONICAL SPELLING (%q): %v", name, err)
		}
		if err := l.Finish(name, 1); !errors.As(err, &le) || le.Step != "name" {
			t.Fatalf("A RECORD WAS FINISHED UNDER A NAME THAT IS NO ATTEMPT'S CANONICAL SPELLING (%q): %v", name, err)
		}
		if taken, _, err := l.Has(name); !taken || err == nil {
			t.Fatalf("A NAME THAT IS NO ATTEMPT'S CANONICAL SPELLING WAS ANSWERED (%q): taken %v, %v", name, taken, err)
		}
	}
	if lost := kept(before, contents(t, l.Dir)); len(lost) != 0 || len(contents(t, l.Dir)) != len(before) {
		t.Fatalf("the ledger changed: %v", contents(t, l.Dir))
	}
}

// A ledger directory that cannot be read cannot say whether an attempt was
// taken: the answer is taken, with the error — never "not taken".
func TestWhatTheLedgerCannotReadCountsAsTaken(t *testing.T) {
	l := newLedger(t)
	if err := l.Take(Execution{Attempt: "0v5cmd"}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"taken", "finished"} {
		dir := filepath.Join(l.Dir, sub)
		if err := os.Chmod(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		for _, a := range []string{"0v5cmd", "0v6cmd"} {
			taken, finished, err := l.Has(a)
			if err == nil || !taken || finished {
				os.Chmod(dir, 0o700)
				t.Fatalf("AN UNREADABLE LEDGER (%s) ANSWERED FOR %s: taken %v finished %v, %v", sub, a, taken, finished, err)
			}
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// A record counts by its existence, whatever it holds: garbage, nothing (a
// write a crash cut short), a directory, a dangling symlink. A take of it is
// refused, rewrites nothing, and never writes through the symlink.
func TestARecordCountsWhateverItHolds(t *testing.T) {
	l := newLedger(t)
	taken, finished := filepath.Join(l.Dir, "taken"), filepath.Join(l.Dir, "finished")
	target := filepath.Join(t.TempDir(), "elsewhere")
	for _, err := range []error{
		os.WriteFile(filepath.Join(taken, "0v5cmd"), []byte("\x00garbage"), 0o600),
		os.WriteFile(filepath.Join(taken, "0v6cmd"), nil, 0o600),
		os.Mkdir(filepath.Join(taken, "0v7cmd"), 0o700),
		os.Symlink(target, filepath.Join(taken, "0v8cmd")),
		os.WriteFile(filepath.Join(finished, "0v9cmd"), []byte("garbage"), 0o600),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []string{"0v5cmd", "0v6cmd", "0v7cmd", "0v8cmd"} {
		if taken, finished, err := l.Has(a); !taken || finished || err != nil {
			t.Fatalf("A RECORD WAS NOT COUNTED WHATEVER IT HOLDS (%s): taken %v finished %v, %v", a, taken, finished, err)
		}
	}
	if taken, finished, err := l.Has("0v9cmd"); !taken || !finished || err != nil {
		t.Fatalf("A FINISHED RECORD WAS NOT COUNTED WHATEVER IT HOLDS: taken %v finished %v, %v", taken, finished, err)
	}
	before := contents(t, l.Dir)
	for _, a := range []string{"0v5cmd", "0v6cmd", "0v7cmd", "0v8cmd"} {
		if err := l.Take(Execution{Attempt: a}); !errors.Is(err, ErrTaken) {
			t.Fatalf("A TAKE OF AN ATTEMPT WITH A RECORD WAS NOT REFUSED (%s): %v", a, err)
		}
	}
	if lost := kept(before, contents(t, l.Dir)); len(lost) != 0 {
		t.Fatalf("A RECORD WAS REWRITTEN: %v", lost)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("A TAKE WROTE THROUGH A SYMLINK: %v", err)
	}
}

// A record the filesystem refuses — full (ENOSPC), before or after the
// step's effect — is an error naming the step, never a success, and says
// whether its file exists, as the ledger then answers; every record there
// already stays as it was: nothing is evicted, rewritten or pruned to make
// room. Once the filesystem has room, a new record is written.
func TestAFullLedgerRefusesTheRecordAndEvictsNothing(t *testing.T) {
	l := newLedger(t)
	for n := int64(1); n <= 64; n++ {
		if err := l.Take(Execution{Attempt: attemptNumbered(n), At: n}); err != nil {
			t.Fatal(err)
		}
		if n%2 == 1 {
			if err := l.Finish(attemptNumbered(n), n); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := contents(t, l.Dir)
	for i, c := range []struct {
		step    string
		after   bool
		created bool
	}{
		{"create", false, false}, {"create", true, true}, {"write", false, true}, {"write", true, true},
		{"sync", false, true}, {"close", true, true}, {"sync-dir", false, true},
	} {
		attempt := attemptNumbered(int64(1000 + i))
		l.ops = &ledgerFaults{fail: c.step, after: c.after, err: syscall.ENOSPC}
		err := l.Take(Execution{Attempt: attempt})
		var le *LedgerError
		if !errors.As(err, &le) || le.Step != c.step || !errors.Is(err, syscall.ENOSPC) || errors.Is(err, ErrTaken) {
			t.Fatalf("A RECORD THE FILESYSTEM REFUSED WAS NOT REPORTED AS SUCH (%s, after %v): %v", c.step, c.after, err)
		}
		if le.Created != c.created {
			t.Fatalf("a refused record (%s, after %v) says its file exists: %v, want %v", c.step, c.after, le.Created, c.created)
		}
		if taken, _, err := l.Has(attempt); taken != c.created || err != nil {
			t.Fatalf("A REFUSED RECORD (%s, after %v) IS ANSWERED taken %v, want %v (its file's existence): %v", c.step, c.after, taken, c.created, err)
		}
		if lost := kept(before, contents(t, l.Dir)); len(lost) != 0 {
			t.Fatalf("A RECORD WAS EVICTED OR REWRITTEN ON A FULL LEDGER (%s, after %v): %v", c.step, c.after, lost)
		}
	}
	l.ops = nil
	if err := l.Take(Execution{Attempt: attemptNumbered(2000)}); err != nil {
		t.Fatalf("THE LEDGER REFUSED A RECORD ONCE THE FILESYSTEM HAD ROOM: %v", err)
	}
	if lost := kept(before, contents(t, l.Dir)); len(lost) != 0 {
		t.Fatalf("A RECORD WAS EVICTED OR REWRITTEN: %v", lost)
	}
}

// Open makes the ledger — each directory synced into its parent, HISTORY
// written durably — once. A second open answers the first HISTORY and
// changes nothing: no record, and not the history.
func TestOpenMakesTheLedgerOnceAndNeverReplacesItsHistory(t *testing.T) {
	l := LedgerFor(filepath.Join(t.TempDir(), "state", "state.json"))
	if err := os.MkdirAll(filepath.Dir(l.Dir), 0o700); err != nil {
		t.Fatal(err)
	}
	ops := &ledgerFaults{}
	l.ops = ops
	first := History{Since: 1000, Enrolled: false, StateFormat: 0}
	if h, err := l.Open(first, false); err != nil || h != first {
		t.Fatalf("open: %+v %v", h, err)
	}
	if want := "sync-dir sync-dir sync-dir create write sync close sync-dir"; strings.Join(ops.trace, " ") != want {
		t.Fatalf("THE LEDGER WAS NOT MADE DURABLE AS IT WAS MADE: %v, want %s", ops.trace, want)
	}
	for _, dir := range []string{l.Dir, filepath.Join(l.Dir, "taken"), filepath.Join(l.Dir, "finished")} {
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s: a private directory: %v %v", dir, info, err)
		}
	}
	var got map[string]any
	data, err := os.ReadFile(filepath.Join(l.Dir, "HISTORY"))
	if err != nil || json.Unmarshal(data, &got) != nil || got["since"] != float64(1000) || got["enrolled"] != false || got["state_format"] != float64(0) {
		t.Fatalf("HISTORY says when the history began, and with what: %q %v", data, err)
	}
	if err := l.Take(Execution{Attempt: "0v5cmd"}); err != nil {
		t.Fatal(err)
	}
	before := contents(t, l.Dir)
	for _, keptBefore := range []bool{false, true} {
		if h, err := l.Open(History{Since: 2000, Enrolled: true, StateFormat: CurrentFormat}, keptBefore); err != nil || h != first {
			t.Fatalf("AN OPEN DID NOT ANSWER THE LEDGER'S OWN HISTORY (kept %v): %+v %v", keptBefore, h, err)
		}
		after := contents(t, l.Dir)
		if lost := kept(before, after); len(lost) != 0 || len(after) != len(before) {
			t.Fatalf("AN OPEN CHANGED THE LEDGER (kept %v): %v", keptBefore, lost)
		}
	}
}

// A ledger whose making a stop cut short — its directory, part of its
// record directories, no HISTORY: nothing can have been recorded in it —
// is completed, HISTORY last.
func TestALedgerWhoseMakingWasCutShortIsCompleted(t *testing.T) {
	l := LedgerFor(filepath.Join(t.TempDir(), "state.json"))
	if err := os.MkdirAll(filepath.Join(l.Dir, "taken"), 0o700); err != nil {
		t.Fatal(err)
	}
	want := History{Since: 7, StateFormat: 0}
	if h, err := l.Open(want, false); err != nil || h != want {
		t.Fatalf("A LEDGER WHOSE MAKING WAS CUT SHORT WAS NOT COMPLETED: %+v %v", h, err)
	}
	for _, p := range []string{"finished", "HISTORY"} {
		if _, err := os.Lstat(filepath.Join(l.Dir, p)); err != nil {
			t.Fatalf("A LEDGER WHOSE MAKING WAS CUT SHORT WAS NOT COMPLETED: %s: %v", p, err)
		}
	}
}

// A ledger made before — its HISTORY there — or one its runner kept (its
// state file says so) is never made again: gone, or missing a record
// directory, it is an error, and nothing is made in its place; kept, its
// HISTORY gone, it answers an unknown history and writes none.
func TestALedgerMadeBeforeIsNeverMadeAgain(t *testing.T) {
	setAside := func(t *testing.T, p string) {
		if err := os.Rename(p, p+".aside"); err != nil { // the test's own files: set aside, not deleted
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name  string
		gone  string // under the ledger; "" the ledger itself
		kept  bool
		fails bool
	}{
		{"kept, the ledger gone", "", true, true},
		{"kept, its taken directory gone", "taken", true, true},
		{"kept, its finished directory gone", "finished", true, true},
		{"made, its taken directory gone", "taken", false, true},
		{"made, its finished directory gone", "finished", false, true},
		{"kept, its HISTORY gone", "HISTORY", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := newLedger(t)
			if err := l.Take(Execution{Attempt: "0v5cmd"}); err != nil {
				t.Fatal(err)
			}
			if c.gone == "" {
				setAside(t, l.Dir)
			} else {
				setAside(t, filepath.Join(l.Dir, c.gone))
			}
			before := contents(t, filepath.Dir(l.Dir))
			h, err := l.Open(History{Since: 2000, Enrolled: true, StateFormat: CurrentFormat}, c.kept)
			if c.fails && err == nil {
				t.Fatalf("A LEDGER THAT LOST ITS RECORDS WAS OPENED (%s): %+v", c.name, h)
			}
			if !c.fails && (err != nil || h != (History{})) {
				t.Fatalf("a kept ledger without its HISTORY answers an unknown history (%s): %+v %v", c.name, h, err)
			}
			after := contents(t, filepath.Dir(l.Dir))
			if lost := kept(before, after); len(lost) != 0 || len(after) != len(before) {
				t.Fatalf("A LEDGER THAT LOST ITS RECORDS WAS MADE AGAIN (%s): %v", c.name, after)
			}
		})
	}
}

// A ledger that is no directory — the ledger, or one of its record
// directories, a file or a symlink — is not opened: nothing runs on a
// ledger that cannot record, or that records somewhere else.
func TestOpenRefusesALedgerThatIsNoDirectory(t *testing.T) {
	file := func(p string) error { return os.WriteFile(p, []byte("not a directory\n"), 0o600) }
	link := func(p string) error { return os.Symlink(t.TempDir(), p) }
	for _, c := range []struct {
		name, sub string
		make      func(string) error
	}{
		{"the ledger a file", "", file},
		{"the ledger a symlink", "", link},
		{"its taken directory a file", "taken", file},
		{"its taken directory a symlink", "taken", link},
		{"its finished directory a file", "finished", file},
		{"its finished directory a symlink", "finished", link},
	} {
		l := LedgerFor(filepath.Join(t.TempDir(), "state.json"))
		p := l.Dir
		if c.sub != "" {
			if err := os.Mkdir(l.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			p = filepath.Join(l.Dir, c.sub)
		}
		if err := c.make(p); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Open(History{Since: 1}, false); err == nil {
			t.Fatalf("A LEDGER THAT IS NO DIRECTORY WAS OPENED (%s)", c.name)
		}
	}
}

// A HISTORY that cannot be read — a write a crash cut short, garbage, no
// regular file — answers an unknown history, and is kept as it is: no
// record depends on it, and nothing rewrites it.
func TestAnUnreadableHistoryIsUnknownAndKept(t *testing.T) {
	for _, c := range []struct {
		name string
		make func(string) error
	}{
		{"cut short", func(p string) error { return os.WriteFile(p, nil, 0o600) }},
		{"garbage", func(p string) error { return os.WriteFile(p, []byte("\x00{"), 0o600) }},
		{"a directory", func(p string) error { return os.Mkdir(p, 0o700) }},
		{"a symlink", func(p string) error { return os.Symlink(filepath.Join(filepath.Dir(p), "elsewhere"), p) }},
	} {
		l := LedgerFor(filepath.Join(t.TempDir(), "state.json"))
		for _, dir := range []string{l.Dir, filepath.Join(l.Dir, "taken"), filepath.Join(l.Dir, "finished")} {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(l.Dir, "HISTORY")
		if err := c.make(path); err != nil {
			t.Fatal(err)
		}
		before := contents(t, l.Dir)
		h, err := l.Open(History{Since: 5, Enrolled: true, StateFormat: CurrentFormat}, true)
		if err != nil || h != (History{}) {
			t.Fatalf("AN UNREADABLE HISTORY WAS NOT ANSWERED AS UNKNOWN (%s): %+v %v", c.name, h, err)
		}
		if lost := kept(before, contents(t, l.Dir)); len(lost) != 0 {
			t.Fatalf("AN UNREADABLE HISTORY WAS REPLACED (%s): %v", c.name, lost)
		}
		if _, err := os.Lstat(filepath.Join(l.Dir, "elsewhere")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("AN OPEN WROTE THROUGH A SYMLINKED HISTORY (%s): %v", c.name, err)
		}
	}
}
