package launcher

// Process custody in the core (runner/launcher/INTEGRATION.md §11.2;
// independent review 01, R1): a VMM whose state the host cannot verify is
// never signalled and never taken for gone — nothing is removed from under
// it and the record stays charged — and a retry's scan that is incomplete
// resolves nothing, though it stops the VMMs it did verify.

import (
	"errors"
	"strings"
	"testing"
)

// A known pid the host cannot verify: the owner's destroy neither
// signals it nor removes anything, quarantines the record saying why, and
// inspection shows the answer; once the host can verify it again, the
// operator's retry stops it and cleans up.
func TestAnUnverifiableVMMIsNeverTakenForGone(t *testing.T) {
	h := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0vunk", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.unknown[r.ID] = true
	h.mu.Unlock()
	if rec, err := s.Inspect(owner1, r.ID); err != nil || rec.Alive || !strings.HasPrefix(rec.Liveness, "unknown") {
		t.Fatalf("AN UNVERIFIABLE VMM WAS SHOWN ALIVE, OR ITS STATE HIDDEN: %+v %v", rec, err)
	}
	err := s.Destroy(owner1, r.ID)
	rec, ok := held(s, r.ID)
	if !errors.Is(err, ErrQuarantined) || !ok || rec.State != StateQuarantined || !rec.HasVMM || used(s)[2] != 1 {
		t.Fatalf("AN UNVERIFIABLE VMM WAS TAKEN FOR GONE: %v; held %v %+v", err, ok, rec)
	}
	if h.count("kill") != 0 {
		t.Fatalf("AN UNVERIFIED PROCESS WAS SIGNALLED: %d kills", h.count("kill"))
	}
	if !h.exists("cgroup", r.ID) || !h.exists("jail", r.ID) || h.count("rmcgroup")+h.count("rmdisk")+h.count("rmjail") != 0 {
		t.Fatalf("RESOURCES WERE REMOVED FROM UNDER AN UNVERIFIABLE VMM: %v", h.leaks(r.ID))
	}
	if !strings.Contains(rec.Reason, "cannot be verified") {
		t.Fatalf("the quarantine does not say why: %q", rec.Reason)
	}
	h.mu.Lock()
	delete(h.unknown, r.ID)
	h.mu.Unlock()
	if _, err := s.RetryCleanup(selectionOf(rec)); err != nil || len(h.leaks(r.ID)) != 0 {
		t.Fatalf("the retry once the VMM can be verified: %v; leaks %v", err, h.leaks(r.ID))
	}
}

// partialFinder is a host whose scan verifies the model's VMM but could
// not classify everything: an incomplete look.
type partialFinder struct{ *modelHost }

func (h partialFinder) FindVMMs(id string) ([]int, error) {
	pids, err := h.modelHost.FindVMMs(id)
	return pids, errors.Join(err, errors.New("pid 4242: its command line cannot be read"))
}

// A retry whose scan is incomplete stops the VMM it verified — its
// ownership is established — but resolves nothing: the holding stays and
// nothing is removed.
func TestAnIncompleteScanStopsWhatItVerifiedAndResolvesNothing(t *testing.T) {
	mh := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), partialFinder{mh}, nil)
	rec := startUncertain(t, s, mh, "0vpart", "after")
	_, err := s.RetryCleanup(selectionOf(rec))
	after, _ := held(s, rec.ID)
	if !errors.Is(err, ErrQuarantined) || !after.HasVMM || !mh.exists("cgroup", rec.ID) || !mh.exists("jail", rec.ID) {
		t.Fatalf("AN INCOMPLETE SCAN RESOLVED THE VMM: %v; record %+v; leaks %v", err, after, mh.leaks(rec.ID))
	}
	if mh.count("kill") == 0 || mh.exists("vmm", rec.ID) {
		t.Fatalf("THE VMM THE SCAN VERIFIED WAS NOT STOPPED: kills %d", mh.count("kill"))
	}
	if !strings.Contains(err.Error(), "incomplete") || !strings.Contains(err.Error(), "4242") {
		t.Fatalf("the retry does not say why: %v", err)
	}
}
