package daemon

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"urgit/runner/internal/sandbox"
)

// A real silent pipe, with neither context handling nor an EXIT reply.
// Destroy is the last-resort owner of the VM, not the fake process.
type silentJobBox struct {
	*fakeBox
	reader  *io.PipeReader
	writer  *io.PipeWriter
	done    chan int
	started chan struct{}
}

func newSilentJobBox(t *testing.T) *silentJobBox {
	t.Helper()
	r, w := io.Pipe()
	b := &silentJobBox{fakeBox: &fakeBox{}, reader: r, writer: w, done: make(chan int, 1), started: make(chan struct{})}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
		select {
		case b.done <- 255:
		default:
		}
	})
	return b
}
func (b *silentJobBox) Run(context.Context, sandbox.Handle, string, []string, []string) (io.ReadCloser, <-chan int, error) {
	b.record("run")
	close(b.started)
	return b.reader, b.done, nil
}
func (b *silentJobBox) Signal(_ context.Context, _ sandbox.Handle, process, signal string) error {
	b.record("signal " + process + " " + signal)
	return nil
}
func (b *silentJobBox) Destroy(ctx context.Context, h sandbox.Handle) error {
	b.record("destroy " + h.ID)
	return nil
}
func fastJobTiming() jobTiming {
	return jobTiming{poll: 5 * time.Millisecond, request: 20 * time.Millisecond, signal: 10 * time.Millisecond, term: 20 * time.Millisecond, exit: 20 * time.Millisecond, drain: 20 * time.Millisecond, destroy: 50 * time.Millisecond}
}

func TestC9ClosedSilentJob(t *testing.T) {
	for _, status := range []string{"passed", "failed", "skipped", "reoffered", "cancelled", "infrastructure-error"} {
		t.Run(status, func(t *testing.T) {
			b := newSilentJobBox(t)
			sh := &fakeShip{}
			d := newTestDaemon(t, b.fakeBox, sh, 1)
			d.box, d.jobTiming = b, fastJobTiming()
			var queries atomic.Int32
			d.client.HTTP.Transport = inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/attempt/0v1.att") {
					queries.Add(1)
					io.WriteString(w, `{"status":"`+status+`"}`)
					return
				}
				sh.handler(t).ServeHTTP(w, r)
			})}
			finished := make(chan struct{})
			go func() { d.handle(context.Background(), jobAssignment); close(finished) }()
			select {
			case <-finished:
			case <-time.After(300 * time.Millisecond):
				t.Fatal("closed silent attempt did not stop within bound")
			}
			b.mu.Lock()
			ops := strings.Join(b.ops, "\n")
			b.mu.Unlock()
			if queries.Load() == 0 || !strings.Contains(ops, "signal act TERM\nsignal act KILL\ndestroy ci-0v1.att") {
				t.Fatalf("queries=%d ops=%s", queries.Load(), ops)
			}
			sh.mu.Lock()
			defer sh.mu.Unlock()
			if len(sh.results) != 0 || len(sh.abandons) != 0 || len(sh.events) != 0 {
				t.Fatal("closed silent attempt sent reporting traffic")
			}
		})
	}
}

func TestC9DaemonStopSilentJob(t *testing.T) {
	b := newSilentJobBox(t)
	d := newTestDaemon(t, b.fakeBox, &fakeShip{}, 1)
	d.box = b
	d.jobTiming = fastJobTiming()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() { d.handle(ctx, jobAssignment); close(finished) }()
	select {
	case <-b.started:
	case <-time.After(time.Second):
		t.Fatal("job never started")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("daemon stop left silent job blocked; no bounded teardown")
	}
	b.mu.Lock()
	ops := strings.Join(b.ops, "\n")
	b.mu.Unlock()
	if !strings.Contains(ops, "signal act TERM\nsignal act KILL\ndestroy ci-0v1.att") {
		t.Fatalf("missing TERM, KILL, Destroy in order: %s", ops)
	}
}
