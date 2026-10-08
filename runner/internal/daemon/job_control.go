package daemon

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"urgit/runner/internal/relay"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
)

// C9 cancellation budget: 5 poll + 3 request + 2 TERM + 5 grace +
// 2 KILL + 5 exit + 2 relay drain + 80 teardown = 104s < 120s.
// Normal/deadline teardown retains teardownBound. Tests shorten each phase.
type jobTiming struct {
	poll, request, signal, term, exit, drain, destroy time.Duration
}

func (d *Daemon) jobBounds() jobTiming {
	t := d.jobTiming
	defaults := jobTiming{5 * time.Second, 3 * time.Second, 2 * time.Second, 5 * time.Second, 5 * time.Second, 2 * time.Second, 80 * time.Second}
	fields := []*time.Duration{&t.poll, &t.request, &t.signal, &t.term, &t.exit, &t.drain, &t.destroy}
	values := []time.Duration{defaults.poll, defaults.request, defaults.signal, defaults.term, defaults.exit, defaults.drain, defaults.destroy}
	for i, field := range fields {
		if *field <= 0 {
			*field = values[i]
		}
	}
	return t
}

// boundedJobCall also bounds backends whose guest control write does not
// honor ctx. A timed-out destroy is retained, never counted as released.
// The outstanding operation names only this handle/incarnation.
func boundedJobCall(limit time.Duration, call func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	answer := make(chan error, 1)
	go func() { answer <- call(ctx) }()
	select {
	case err := <-answer:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (d *Daemon) destroyStoppedJob(h sandbox.Handle) error {
	return boundedJobCall(d.jobBounds().destroy, func(ctx context.Context) error { return d.box.Destroy(ctx, h) })
}

// closedAttempt is a positive allow-list, not "anything but running".
// urgit-ci.hoon:3082-3090 enumerates the attempt statuses; :3726-3734
// serializes status. :3205-3222 closes results; :2106-2121 cancels;
// :2992 records reoffered with finished. Unknown/absent/error stays open.
func closedAttempt(status string) bool {
	switch status {
	case "passed", "failed", "skipped", "reoffered", "cancelled", "infrastructure-error":
		return true
	}
	return false
}

func (d *Daemon) watchAttempt(ctx context.Context, attempt string, t jobTiming, closed func()) {
	ticker := time.NewTicker(t.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			query, cancel := context.WithTimeout(ctx, t.request)
			status, found, err := d.client.AttemptStatus(query, attempt)
			cancel()
			if err == nil && found && closedAttempt(status) {
				closed()
				return
			}
		}
	}
}

// relayJob keeps control independent of a silent pipe or an event POST.
// Only this goroutine receives EXIT. The relay owns and closes its log.
func (d *Daemon) relayJob(ctx context.Context, a *ship.Assignment, h sandbox.Handle, stream io.ReadCloser, done <-chan int, path string, values []string, logf func(string, ...any)) (relay.Summary, error, int, bool) {
	t := d.jobBounds()
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	scrubbed := relay.Scrub(stream, relay.ScrubForms(values))
	closeStream := func() {
		_ = stream.Close()
		if closer, ok := scrubbed.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	defer closeStream()
	closed := make(chan struct{}, 1)
	notifyClosed := func() {
		select {
		case closed <- struct{}{}:
		default:
		}
	}
	go d.watchAttempt(rctx, a.Attempt, t, notifyClosed)
	type relayed struct {
		summary relay.Summary
		err     error
	}
	relayDone := make(chan relayed, 1)
	go func() {
		var output io.Writer = io.Discard
		streamLog, err := os.Create(path)
		if err == nil {
			defer streamLog.Close()
			output = streamLog
		}
		summary, err := relay.Relay(rctx, io.TeeReader(scrubbed, output), func(ctx context.Context, line []byte) (int, []byte, error) {
			resp, err := d.client.Event(ctx, a.Attempt, line)
			if err == nil && resp.Status == 409 && strings.Contains(resp.Error(), "attempt is closed") {
				notifyClosed()
			}
			return resp.Status, resp.Body, err
		}, func(msg string) { logf("%s", msg) })
		relayDone <- relayed{summary, err}
	}()
	var result relayed
	code, haveCode, haveRelay := 255, false, false
	var exitWait <-chan time.Time
	var exitTimer *time.Timer
	defer func() {
		if exitTimer != nil {
			exitTimer.Stop()
		}
	}()
	stop := func(reason string) (relay.Summary, error, int, bool) {
		logf("%s; stopping act", reason)
		cancel()
		closeStream() // unblock the scanner and any scrub pipe, even in silence
		send := func(signal string) {
			if err := boundedJobCall(t.signal, func(ctx context.Context) error { return d.box.Signal(ctx, h, "act", signal) }); err != nil {
				logf("act %s: %v", signal, err)
			}
		}
		if !haveCode {
			send("TERM")
			grace := time.NewTimer(t.term)
			select {
			case code = <-done:
				haveCode = true
			case <-grace.C:
			}
			grace.Stop()
			if !haveCode {
				send("KILL")
				wait := time.NewTimer(t.exit)
				select {
				case code = <-done:
					haveCode = true
				case <-wait.C:
					logf("act exit not received within bound; tearing down sandbox")
				}
				wait.Stop()
			}
		}
		if !haveRelay {
			drain := time.NewTimer(t.drain)
			select {
			case result = <-relayDone:
			case <-drain.C:
			}
			drain.Stop()
		}
		return result.summary, result.err, code, true
	}
	for {
		if ctx.Err() != nil {
			return stop("daemon/reporting context ended")
		}
		select {
		case <-closed:
			return stop("ship closed the attempt")
		default:
		}
		if haveRelay && haveCode {
			return result.summary, result.err, code, false
		}
		select {
		case <-ctx.Done():
			return stop("daemon/reporting context ended")
		case <-closed:
			return stop("ship closed the attempt")
		case code = <-done:
			haveCode = true
			done = nil
		case result = <-relayDone:
			haveRelay = true
			relayDone = nil
			if !haveCode {
				exitTimer = time.NewTimer(t.exit)
				exitWait = exitTimer.C
			}
		case <-exitWait:
			return stop("act stream ended without an exit code")
		}
	}
}
