package state

// Independent review 12, R12-1 (runner/launcher/INTEGRATION.md §11.16):
// what the correction adds, read after it — Transition.Problem, the one rule
// every consumer asks. A record is a transition only when it is one this
// version writes: an object of exactly its four fields, each of its type, an
// epoch of 1 or more, the command a @uv in the ship's spelling, the evidence
// a sha256 in hex, and its time. Each rule, broken alone, is named.

import (
	"strings"
	"testing"
)

func TestATransitionRecordIsATransitionOnlyWhenItIsOneThisVersionWrites(t *testing.T) {
	e := recordEvidence
	valid := `{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":1790000000}`
	st, err := Load(fileWithTransition(t, valid))
	if err != nil || st.Transition == nil || st.Transition.Problem() != "" {
		t.Fatalf("A TRANSITION THIS VERSION WRITES WAS TAKEN FOR NONE: %+v %v", st, err)
	}
	// its fields in another order are the same transition
	st, err = Load(fileWithTransition(t, `{"at":1790000000,"evidence":"`+e+`","command":"0v5hist","epoch":2}`))
	if err != nil || st.Transition.Problem() != "" || st.Transition.Epoch != 2 {
		t.Fatalf("A TRANSITION WITH ITS FIELDS IN ANOTHER ORDER WAS TAKEN FOR NONE: %+v %v", st, err)
	}
	// one made in memory, as the daemon makes it
	made := &Transition{Epoch: 1, Command: "0v5hist", Evidence: e, At: 1790000000}
	if p := made.Problem(); p != "" {
		t.Fatalf("A TRANSITION MADE IN MEMORY WAS TAKEN FOR NONE: %s", p)
	}
	for name, c := range map[string]struct{ raw, why string }{
		"an empty object":                     {`{}`, "the record has no epoch"},
		"epoch 0":                             {`{"epoch":0,"command":"0v5hist","evidence":"` + e + `","at":1790000000}`, "authorization epoch is 0"},
		"an epoch of another type":            {`{"epoch":"1","command":"0v5hist","evidence":"` + e + `","at":1790000000}`, "not of its type"},
		"an epoch that is no integer":         {`{"epoch":1.5,"command":"0v5hist","evidence":"` + e + `","at":1790000000}`, "not of its type"},
		"no command":                          {`{"epoch":1,"evidence":"` + e + `","at":1790000000}`, "the record has no command"},
		"a command in another spelling":       {`{"epoch":1,"command":"0v5.hist","evidence":"` + e + `","at":1790000000}`, "no @uv in the ship's spelling"},
		"a command that is no atom":           {`{"epoch":1,"command":"hist","evidence":"` + e + `","at":1790000000}`, "no @uv in the ship's spelling"},
		"no evidence":                         {`{"epoch":1,"command":"0v5hist","at":1790000000}`, "the record has no evidence"},
		"evidence that is no sha256":          {`{"epoch":1,"command":"0v5hist","evidence":"` + e[:12] + `","at":1790000000}`, "no sha256 in hex"},
		"evidence in upper case":              {`{"epoch":1,"command":"0v5hist","evidence":"` + strings.ToUpper(e) + `","at":1790000000}`, "no sha256 in hex"},
		"no time":                             {`{"epoch":1,"command":"0v5hist","evidence":"` + e + `"}`, "the record has no at"},
		"a time of 0":                         {`{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":0}`, "its time is not recorded"},
		"a time before 1970":                  {`{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":-5}`, "its time is not recorded"},
		"a field this version does not write": {`{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":1790000000,"trusted":true}`, "a field this version does not write"},
		"a field in another case":             {`{"EPOCH":1,"command":"0v5hist","evidence":"` + e + `","at":1790000000}`, "the record has no epoch"},
		"a string":                            {`"a transition"`, "no object"},
		"a number":                            {`1`, "no object"},
		"a list":                              {`[{"epoch":1}]`, "no object"},
	} {
		st, err := Load(fileWithTransition(t, c.raw))
		if err != nil || st.Transition == nil {
			t.Fatalf("A STATE FILE WITH A MALFORMED TRANSITION RECORD DID NOT LOAD (%s): %+v %v", name, st, err)
		}
		p := st.Transition.Problem()
		if p == "" {
			t.Fatalf("A RECORD THAT IS NO TRANSITION WAS TAKEN FOR ONE (%s): %s", name, c.raw)
		}
		if !strings.Contains(p, c.why) {
			t.Fatalf("A RECORD THAT IS NO TRANSITION WAS NOT SAID WHY (%s): %q, want %q", name, p, c.why)
		}
	}
	// made in memory, each rule alone
	for name, tr := range map[string]*Transition{
		"epoch 0":            {Command: "0v5hist", Evidence: e, At: 1},
		"another spelling":   {Epoch: 1, Command: "0v5.hist", Evidence: e, At: 1},
		"evidence too short": {Epoch: 1, Command: "0v5hist", Evidence: "ab", At: 1},
		"no time":            {Epoch: 1, Command: "0v5hist", Evidence: e},
	} {
		if tr.Problem() == "" {
			t.Fatalf("A RECORD THAT IS NO TRANSITION WAS TAKEN FOR ONE, MADE IN MEMORY (%s)", name)
		}
	}
}

func TestARecordKeptAsTheFileHeldItIsWhatRecordGives(t *testing.T) {
	raw := `{"epoch":0,"command":"0v5hist","trusted":true}`
	st, err := Load(fileWithTransition(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Transition.Record(); !sameJSON(got, []byte(raw)) {
		t.Fatalf("THE RECORD IS NOT GIVEN AS THE FILE HELD IT: %s, want %s", got, raw)
	}
	made := &Transition{Epoch: 1, Command: "0v5hist", Evidence: recordEvidence, At: 7}
	if got := made.Record(); !sameJSON(got, []byte(`{"epoch":1,"command":"0v5hist","evidence":"`+recordEvidence+`","at":7}`)) {
		t.Fatalf("A TRANSITION MADE IN MEMORY IS NOT GIVEN AS THIS VERSION WRITES IT: %s", got)
	}
}
