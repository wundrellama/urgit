package state

// Independent review 12, R12-1 (runner/launcher/INTEGRATION.md §11.16): a
// field named twice, found by the correction's own audit after its first
// final runs. Go's decoder takes two names it matches to one field — the
// same name, or one in another case — as that field, keeps the last, and
// drops the other, which the next save erases. A transition record that
// names a field twice is no transition: it loads, says why, and is kept as
// the file holds it, both names, through every save. Elsewhere in the state
// file — its transition named twice among them — a field named twice refuses
// the load, and the file is left as it is: no runner writes one, and which
// is meant is not known. The values kept as the file holds them, the
// transition record and each superseded record, are read for no field.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var namedTwiceRecords = map[string]struct{ raw, why string }{
	"its epoch, 0 then 1":             {`{"epoch":0,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000,"epoch":1}`, "names its epoch more than once"},
	"its epoch, the same twice":       {`{"epoch":1,"epoch":1,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000}`, "names its epoch more than once"},
	"its epoch, spelt with an escape": {`{"epoch":0,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000,"\u0065poch":1}`, "names its epoch more than once"},
	"its command":                     {`{"epoch":1,"command":"hist","command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000}`, "names its command more than once"},
	"its evidence":                    {`{"epoch":1,"command":"0v5hist","evidence":"` + strings.Repeat("e", 64) + `","evidence":"` + recordEvidence + `","at":1790000000}`, "names its evidence more than once"},
	"its time":                        {`{"epoch":1,"command":"0v5hist","evidence":"` + recordEvidence + `","at":0,"at":1790000000}`, "names its at more than once"},
}

func TestARecordNamingAFieldTwiceIsNoTransition(t *testing.T) {
	for name, c := range namedTwiceRecords {
		p := fileWithTransition(t, c.raw)
		st, err := Load(p)
		if err != nil || st.Transition == nil {
			t.Fatalf("A STATE FILE WHOSE TRANSITION RECORD NAMES A FIELD TWICE DID NOT LOAD (%s): %+v %v", name, st, err)
		}
		if why := st.Transition.Problem(); !strings.Contains(why, c.why) {
			t.Fatalf("A RECORD NAMING A FIELD TWICE WAS TAKEN FOR A TRANSITION, OR NOT SAID WHY (%s): %q, want %q", name, why, c.why)
		}
		// kept as the file held it, both names, through two saves
		for i := 0; i < 2; i++ {
			q := filepath.Join(t.TempDir(), "state.json")
			if err := Save(q, st); err != nil {
				t.Fatal(err)
			}
			if got := transitionOf(t, q); !sameJSON(got, []byte(c.raw)) {
				t.Fatalf("A RECORD NAMING A FIELD TWICE WAS NOT KEPT AS THE FILE HELD IT (%s, save %d): wrote %s, kept %s", name, i+1, c.raw, got)
			}
			if st, err = Load(q); err != nil {
				t.Fatalf("A STATE FILE THIS VERSION SAVED, A RECORD NAMING A FIELD TWICE IN IT, DID NOT LOAD (%s): %v", name, err)
			}
		}
	}
}

func TestAStateFileNamingAFieldTwiceIsRefusedAndLeftAsItIs(t *testing.T) {
	valid := `{"epoch":1,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000}`
	head := `{"daemon_id":"0v1.d","bearer":"b","format":1,"ledger":true,`
	for name, file := range map[string]string{
		"its transition, malformed then valid": head + `"transition":{"epoch":0},"transition":` + valid + `}`,
		"its transition, valid then malformed": head + `"transition":` + valid + `,"transition":{}}`,
		"its transition, in another case":      head + `"transition":{},"Transition":` + valid + `}`,
		"its superseded records":               head + `"superseded_transitions":[{"record":{},"problem":"p","by":"0v1","at":1}],"superseded_transitions":[]}`,
		"a superseded record's entry":          head + `"superseded_transitions":[{"record":{"epoch":0},"record":{},"problem":"p","by":"0v1","at":1}]}`,
		"its bearer":                           `{"daemon_id":"0v1.d","bearer":"a","bearer":"b"}`,
		"a retention's handle":                 head + `"quarantined":[{"handle":"ci-1","reason":"r","at":1,"handle":"ci-2"}]}`,
		"a refusal's command, in another case": head + `"refused":[{"command":"0v1","selection":"s","revision":1,"evidence":"e","reason":"r","at":1,"Command":"0v2"}]}`,
	} {
		p := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(p, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := Load(p)
		if err == nil || !strings.Contains(err.Error(), "more than once") {
			t.Fatalf("A STATE FILE NAMING A FIELD TWICE WAS NOT REFUSED (%s): %+v %v", name, st, err)
		}
		if data, _ := os.ReadFile(p); !bytes.Equal(data, []byte(file)) {
			t.Fatalf("A REFUSED STATE FILE WAS NOT LEFT AS IT IS (%s): %s", name, data)
		}
	}
	// the values kept as the file holds them are read for no field: a name
	// twice inside them refuses nothing
	for name, file := range map[string]string{
		"inside its transition":                           head + `"transition":{"epoch":0,"epoch":1}}`,
		"inside a superseded record":                      head + `"superseded_transitions":[{"record":{"epoch":0,"epoch":1},"problem":"p","by":"0v1","at":1}]}`,
		"a transition and a superseded record, each once": head + `"transition":` + valid + `,"superseded_transitions":[{"record":{},"problem":"p","by":"0v1","at":1}]}`,
	} {
		p := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(p, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		if st, err := Load(p); err != nil || st == nil {
			t.Fatalf("A STATE FILE NAMING NO FIELD TWICE WAS REFUSED (%s): %v", name, err)
		}
	}
}
