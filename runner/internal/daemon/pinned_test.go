package daemon

// Pinned addresses shown per run (CI-P4-NET-1, 2026-10-08): a sandbox
// whose launcher pinned granted DNS names reports them to the ship, once,
// before anything runs in it; a run the ship refuses to record is not run.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/sandbox"
)

// pinBox is the fake sandbox with a launcher that pinned names
type pinBox struct {
	*fakeBox
	pins []launcher.Pin
}

func (p *pinBox) Pinned(sandbox.Handle) []launcher.Pin { return p.pins }

var reportedPins = []launcher.Pin{
	{Name: "archive.ubuntu.com", Addrs: []string{"91.189.91.82", "185.125.190.81"}},
	{Name: "bootstrap.urbit.org", Addrs: []string{"104.21.64.85"}},
}

const passingStream = `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"

func TestPinsAreReportedBeforeTheJobRuns(t *testing.T) {
	sh := &fakeShip{}
	box := &pinBox{fakeBox: &fakeBox{stream: passingStream}, pins: reportedPins}
	d := newTestDaemon(t, box.fakeBox, sh, 1)
	d.box = box
	d.handle(context.Background(), jobAssignment)
	if len(sh.pinned) != 1 {
		t.Fatalf("THE PINS WERE NOT REPORTED ONCE: %q", sh.pinned)
	}
	var got struct {
		Pinned []launcher.Pin `json:"pinned"`
	}
	if err := json.Unmarshal([]byte(sh.pinned[0]), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(got.Pinned, reportedPins, func(a, b launcher.Pin) bool { return a.Name == b.Name && slices.Equal(a.Addrs, b.Addrs) }) {
		t.Fatalf("the report is not what the launcher pinned: %s", sh.pinned[0])
	}
	if len(sh.results) != 1 {
		t.Fatalf("a run whose pins were recorded did not finish: results %v abandons %v", sh.results, sh.abandons)
	}
}

// The report happens before act: it is in the ship's record from the run's
// start, so a run that never finishes still shows what it could reach.
func TestThePinReportPrecedesTheRun(t *testing.T) {
	sh := &fakeShip{pinnedStatus: 409}
	box := &pinBox{fakeBox: &fakeBox{stream: passingStream}, pins: reportedPins}
	d := newTestDaemon(t, box.fakeBox, sh, 1)
	d.box = box
	d.handle(context.Background(), jobAssignment)
	if len(sh.pinned) != 1 {
		t.Fatalf("pins: %q", sh.pinned)
	}
	for _, op := range box.ops {
		if strings.HasPrefix(op, "run ") || strings.HasPrefix(op, "copy ") {
			t.Fatalf("A RUN THE SHIP REFUSED TO RECORD WENT ON: %q", box.ops)
		}
	}
	if len(sh.results) != 0 || len(sh.abandons) != 1 || !strings.Contains(sh.abandons[0], "pinned addresses could not be recorded") {
		t.Fatalf("a refused pin report is not an abandon naming why: results %v abandons %v", sh.results, sh.abandons)
	}
	if !slices.ContainsFunc(box.ops, func(op string) bool { return strings.HasPrefix(op, "destroy ") }) {
		t.Fatalf("the refused run's sandbox was not destroyed: %q", box.ops)
	}
}

// A sandbox granted no name reports nothing: the ship's record stays as it
// was before named destinations.
func TestNoPinsNoReport(t *testing.T) {
	sh := &fakeShip{}
	box := &pinBox{fakeBox: &fakeBox{stream: passingStream}}
	d := newTestDaemon(t, box.fakeBox, sh, 1)
	d.box = box
	d.handle(context.Background(), jobAssignment)
	if len(sh.pinned) != 0 || len(sh.results) != 1 {
		t.Fatalf("pins %q results %v", sh.pinned, sh.results)
	}
}
