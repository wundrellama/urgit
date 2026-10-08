package launcher

// Stage 01, recovery ruling A: a teardown that begins after its cleanup
// obligation's deadline (the job's deadline plus the allowance) is a missed
// obligation — an incident, quarantined and charged. The accepted tests
// that let a deadline pass by jumping the clock two minutes (expire)
// against the test allowance of 400 ms meant "the deadline passes, and the
// reaper (a create's step, a connect) runs after it" — in time; they now
// say so exactly with pass. Missed obligations are exercised on their own
// (recovery_test.go), where the jump stays.

import (
	"testing"
	"time"
)

// pass moves the clock just past the latest deadline of the records ids
// name, never backwards: the deadline has passed, and what runs next runs
// within the obligation's allowance.
func (c *testClock) pass(t *testing.T, s *Service, ids ...string) {
	t.Helper()
	var latest int64
	s.mu.Lock()
	for _, id := range ids {
		e, ok := s.vms[id]
		if !ok {
			s.mu.Unlock()
			t.Fatalf("pass: %s is not held", id)
		}
		latest = max(latest, e.rec.DeadlineUnix)
	}
	s.mu.Unlock()
	if d := time.Unix(latest, 0).Add(10 * time.Millisecond).Sub(c.now()); d > 0 {
		c.advance(d)
	}
}
