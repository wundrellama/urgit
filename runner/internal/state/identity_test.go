package state

// Independent review 08, R8-1 (runner/launcher/INTEGRATION.md §11.12, "One
// command, one identity"): a recorded refusal is found by its command's
// atom, whatever spelling it was recorded or is asked under; a record whose
// command is not a @uv matches its exact text only.

import "testing"

func TestARefusalIsFoundByItsCommandsAtom(t *testing.T) {
	s := &State{Refused: []Refusal{{Command: "0v05cmd", Reason: "recorded under a leading zero"}}}
	for _, id := range []string{"0v5cmd", "0v5.cmd", "0v05cmd", "0v5c.md"} {
		if r, ok := s.RefusalOf(id); !ok || r.Reason != "recorded under a leading zero" {
			t.Fatalf("A REFUSAL WAS NOT FOUND BY ITS COMMAND'S ATOM (%s): %+v %v", id, r, ok)
		}
	}
	if _, ok := s.RefusalOf("0v6cmd"); ok {
		t.Fatal("another command's refusal was found")
	}
	odd := &State{Refused: []Refusal{{Command: "not-an-atom"}}}
	if _, ok := odd.RefusalOf("not-an-atom"); !ok {
		t.Fatal("a record whose command is not a @uv is not found by its exact text")
	}
	if _, ok := odd.RefusalOf("0v5cmd"); ok {
		t.Fatal("a record whose command is not a @uv matched an atom")
	}
}
