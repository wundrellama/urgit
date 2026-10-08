package state

// An unsettled admission (runner/launcher/INTEGRATION.md §11.10;
// settled-admission ruling 01) is its reserve request and nothing else: it
// is never merged with another request's admission of its handle — a stale
// or competing request never absorbs another — nor with a retention of the
// reservation it may have made, or one recorded before requests: each
// follows its own disposition. A selection names its request.

import (
	"errors"
	"strings"
	"testing"
)

func TestAnAdmissionIsItsRequestOnly(t *testing.T) {
	first := Quarantine{Handle: "ci-0v1.att", Backend: "microvm", Attempt: "0v1.att", Request: strings.Repeat("1", 32), At: 1}
	second := first
	second.Request = strings.Repeat("2", 32)
	identity := Quarantine{Handle: "ci-0v1.att", Backend: "microvm", Attempt: "0v1.att", VM: "t-x", Incarnation: strings.Repeat("3", 32), CID: 3, Created: 4, At: 1}
	legacy := Quarantine{Handle: "ci-0v1.att", Backend: "microvm", Attempt: "0v1.att", At: 1}
	// same says whether q is o as it was recorded, nothing taken from another
	same := func(q, o Quarantine) bool {
		return q.Selection() == o.Selection() && q.Attempt == o.Attempt && q.Label == o.Label
	}
	t.Run("two requests of one handle", func(t *testing.T) {
		s := &State{Quarantined: []Quarantine{first, second}}
		if n := s.Dedupe(); n != 0 || len(s.Quarantined) != 2 {
			t.Fatalf("A REQUEST'S ADMISSION WAS MERGED INTO ANOTHER'S: merged %d, kept %+v", n, s.Quarantined)
		}
		s = &State{Quarantined: []Quarantine{first}}
		if !s.Retain(second) || len(s.Quarantined) != 2 {
			t.Fatalf("A REQUEST'S ADMISSION WAS MERGED INTO ANOTHER'S: %+v", s.Quarantined)
		}
	})
	t.Run("an admission and another retention of its handle", func(t *testing.T) {
		for _, other := range []Quarantine{identity, legacy} {
			for _, order := range [][]Quarantine{{first, other}, {other, first}} {
				s := &State{Quarantined: append([]Quarantine(nil), order...)}
				n := s.Dedupe()
				if n != 0 || len(s.Quarantined) != 2 || !same(s.Quarantined[0], order[0]) || !same(s.Quarantined[1], order[1]) {
					t.Fatalf("AN ADMISSION WAS MERGED WITH ANOTHER RETENTION OF ITS HANDLE: merged %d, kept %+v", n, s.Quarantined)
				}
			}
		}
	})
	t.Run("the same request recorded twice", func(t *testing.T) {
		s := &State{Quarantined: []Quarantine{first, first}}
		if n := s.Dedupe(); n != 1 || len(s.Quarantined) != 1 || !same(s.Quarantined[0], first) {
			t.Fatalf("an admission recorded twice is one: merged %d, kept %+v", n, s.Quarantined)
		}
	})
	t.Run("its selection names its request", func(t *testing.T) {
		s := &State{Quarantined: []Quarantine{first, second}}
		if i, err := s.Find(second.Selection(), true); err != nil || i != 1 {
			t.Fatalf("A SELECTION REACHED ANOTHER REQUEST'S ADMISSION: %d %v", i, err)
		}
		third := first
		third.Request = strings.Repeat("4", 32)
		if _, err := s.Find(third.Selection(), true); !errors.Is(err, ErrStale) {
			t.Fatalf("A SELECTION OF A REQUEST NOT RECORDED FOUND AN ENTRY: %v", err)
		}
	})
}
