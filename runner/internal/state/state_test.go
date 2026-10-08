package state

import (
	"path/filepath"
	"testing"
)

// a quarantine is cleared only by name (or all), by the operator; the
// others stay, and the file round-trips
func TestClearQuarantineByHandleAndAll(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := &State{DaemonID: "0v1.d", Bearer: "b", Quarantined: []Quarantine{{Handle: "ci-a", Reason: "x", At: 1}, {Handle: "ci-b", Reason: "y", At: 2}}}
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || len(got.Quarantined) != 2 {
		t.Fatalf("load: %v %+v", err, got)
	}
	if c := got.ClearQuarantine("ci-zzz"); len(c) != 0 || len(got.Quarantined) != 2 {
		t.Fatalf("an unknown handle clears nothing: %v %+v", c, got.Quarantined)
	}
	if c := got.ClearQuarantine("ci-a"); len(c) != 1 || c[0] != "ci-a" || len(got.Quarantined) != 1 || got.Quarantined[0].Handle != "ci-b" {
		t.Fatalf("by handle: %v %+v", c, got.Quarantined)
	}
	if err := Save(p, got); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(p)
	if len(again.Quarantined) != 1 {
		t.Fatalf("the cleared record must not come back: %+v", again.Quarantined)
	}
	if c := again.ClearQuarantine("all"); len(c) != 1 || len(again.Quarantined) != 0 {
		t.Fatalf("all: %v %+v", c, again.Quarantined)
	}
}
