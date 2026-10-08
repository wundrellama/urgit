package daemon

// The daemon's reconcile takes the launcher's list as its proof that a
// held orphan is gone. A list the launcher refuses — as a reopened launcher
// whose records directory is not certified does (runner/launcher/
// INTEGRATION.md §11.6) — frees nothing; a list that answers without the
// orphan frees its slot.

import (
	"context"
	"errors"
	"testing"
)

// listingBox is the fake box whose Orphans is the launcher's list as the
// test sets it.
type listingBox struct {
	*fakeBox
	ids []string
	err error
}

func (b *listingBox) Orphans(context.Context) ([]string, error) { return b.ids, b.err }

func TestARefusedListFreesNoHeldSlot(t *testing.T) {
	fb := &fakeBox{}
	d := newTestDaemon(t, fb, &fakeShip{}, 2)
	box := &listingBox{fakeBox: fb, err: errors.New("launcher: state change is not durable: the records directory is not certified (input/output error), so a record's absence from the list is no proof of a release")}
	d.box = box
	d.hold(fb.HandleFor("ci-0v5.att"), "the ship calls its attempt running")
	before := d.free()
	err := d.Reconcile(context.Background())
	if d.free() != before {
		t.Fatalf("A REFUSED LIST FREED A HELD SLOT: free %d, then %d (reconcile: %v)", before, d.free(), err)
	}
	if err == nil {
		t.Fatal("a refused list is the reconcile's error")
	}
	box.err = nil // the list answers, without the orphan: it is gone
	if err := d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.free() != before+1 {
		t.Fatalf("a list without the orphan: free %d, want %d", d.free(), before+1)
	}
}
