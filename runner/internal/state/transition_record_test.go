package state

// Independent review 12, R12-1 (runner/launcher/INTEGRATION.md §11.16): a
// transition record is read, never trusted, and never erased. A state file
// whose record this version did not write — an empty object, epoch 0, a
// field of another type, an extra field, no object at all — loads, and
// every save writes the record back as the file held it: the same JSON,
// token for token. A record this version writes round-trips as it is, and
// a null is no record.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fileWithTransition writes a state file whose transition is raw, as
// written, and returns its path.
func fileWithTransition(t *testing.T, raw string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"daemon_id":"0v1.d","bearer":"b","format":2,"ledger":true,"transition":` + raw + "}\n")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// transitionOf is the file's transition value as it holds it, or nil.
func transitionOf(t *testing.T, p string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file["transition"]
}

// sameJSON says whether a and b are the same JSON, token for token.
func sameJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

const recordEvidence = "4f8b7c2a1e9d3b5f6a0c8e2d4b6f8a1c3e5d7b9f0a2c4e6d8b0f1a3c5e7d9b2f"

var malformedRecords = map[string]string{
	"an empty object":                     `{}`,
	"epoch 0":                             `{"epoch":0,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000}`,
	"an epoch of another type":            `{"epoch":"1","command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000}`,
	"a field this version does not write": `{"epoch":1,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000,"trusted":true}`,
	"no evidence":                         `{"epoch":1,"command":"0v5hist","at":1790000000}`,
	"a string":                            `"a transition"`,
	"a number":                            `1`,
	"a list":                              `[{"epoch":1}]`,
}

func TestATransitionRecordIsKeptAsTheFileHoldsIt(t *testing.T) {
	for name, raw := range malformedRecords {
		p := fileWithTransition(t, raw)
		st, err := Load(p)
		if err != nil {
			t.Fatalf("A STATE FILE WITH A MALFORMED TRANSITION RECORD DID NOT LOAD (%s): %v", name, err)
		}
		q := filepath.Join(t.TempDir(), "state.json")
		if err := Save(q, st); err != nil {
			t.Fatal(err)
		}
		if got := transitionOf(t, q); !sameJSON(got, []byte(raw)) {
			t.Fatalf("A MALFORMED TRANSITION RECORD WAS NOT KEPT AS THE FILE HELD IT (%s): wrote %s, kept %s", name, raw, got)
		}
		// and again, over the file just saved: the record is kept every time
		st2, err := Load(q)
		if err != nil {
			t.Fatalf("A STATE FILE WITH A MALFORMED TRANSITION RECORD DID NOT LOAD AGAIN (%s): %v", name, err)
		}
		r := filepath.Join(t.TempDir(), "state.json")
		if err := Save(r, st2); err != nil {
			t.Fatal(err)
		}
		if got := transitionOf(t, r); !sameJSON(got, []byte(raw)) {
			t.Fatalf("A MALFORMED TRANSITION RECORD WAS NOT KEPT AS THE FILE HELD IT, SAVED AGAIN (%s): wrote %s, kept %s", name, raw, got)
		}
	}
}

func TestATransitionRecordThisVersionWritesRoundTrips(t *testing.T) {
	raw := `{"epoch":3,"command":"0v5hist","evidence":"` + recordEvidence + `","at":1790000000}`
	st, err := Load(fileWithTransition(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if tr := st.Transition; tr == nil || tr.Epoch != 3 || tr.Command != "0v5hist" || tr.Evidence != recordEvidence || tr.At != 1790000000 {
		t.Fatalf("A TRANSITION THIS VERSION WRITES WAS NOT READ: %+v", st.Transition)
	}
	q := filepath.Join(t.TempDir(), "state.json")
	if err := Save(q, st); err != nil {
		t.Fatal(err)
	}
	if got := transitionOf(t, q); !sameJSON(got, []byte(raw)) {
		t.Fatalf("A TRANSITION THIS VERSION WRITES DID NOT ROUND-TRIP: wrote %s, kept %s", raw, got)
	}
	// a null is no record: the runner reads none, and writes none
	st, err = Load(fileWithTransition(t, `null`))
	if err != nil || st.Transition != nil {
		t.Fatalf("A NULL TRANSITION WAS READ AS A RECORD: %+v %v", st, err)
	}
}
