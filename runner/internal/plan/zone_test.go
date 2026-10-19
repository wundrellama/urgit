package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// the pinned timezone database: Go 1.27.1's zoneinfo.zip, byte for byte the
// copy the parent verified (REFERENCE-INDEX.json)
const pinnedZoneinfoSHA256 = "b2d18a7c8fa8142097a48c99609fb3c92db5ee98bc740294e57eab8ae9f94779"

func TestZoneDatabaseIsThePinnedCopy(t *testing.T) {
	sum := sha256.Sum256(zoneinfoZip)
	if hex.EncodeToString(sum[:]) != pinnedZoneinfoSHA256 {
		t.Fatalf("embedded zoneinfo.zip sha256 %x, pinned %s", sum, pinnedZoneinfoSHA256)
	}
}

func TestZoneTableNewYork(t *testing.T) {
	z, err := ZoneTable("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if z.Start != ZoneHorizonStart || z.End != ZoneHorizonEnd {
		t.Fatalf("horizon %d..%d, want %d..%d", z.Start, z.End, ZoneHorizonStart, ZoneHorizonEnd)
	}
	if z.Offset != -18000 {
		t.Fatalf("offset at 2020-01-01 %d, want -18000 (EST)", z.Offset)
	}
	want := map[int64]int64{
		utc("2021-03-14T07:00:00Z").Unix(): -14400,
		utc("2021-11-07T06:00:00Z").Unix(): -18000,
		utc("2027-03-14T07:00:00Z").Unix(): -14400,
		utc("2027-11-07T06:00:00Z").Unix(): -18000,
		utc("2039-11-06T06:00:00Z").Unix(): -18000,
	}
	got := map[int64]int64{}
	prev := z.Start
	for _, tr := range z.Transitions {
		if tr[0] <= prev || tr[0] >= z.End {
			t.Fatalf("transitions must ascend inside the horizon: %v", z.Transitions)
		}
		prev = tr[0]
		got[tr[0]] = tr[1]
	}
	for at, off := range want {
		if got[at] != off {
			t.Errorf("transition at %s: %d, want %d", time.Unix(at, 0).UTC(), got[at], off)
		}
	}
	if len(z.Transitions) != 40 {
		t.Errorf("%d transitions in 2020-2039, want 40", len(z.Transitions))
	}
}

// the extracted table agrees with Go's own reading of the same pinned data
// at every hour of the horizon and at each transition's second and the
// second before it: an independent oracle for the sampler
func TestZoneTableAgreesWithTheZoneEngineEverywhere(t *testing.T) {
	for _, name := range []string{"America/New_York", "Australia/Lord_Howe", "Europe/London", "Australia/Sydney", "America/Sao_Paulo", "Asia/Tehran", "Pacific/Apia", "Africa/Casablanca", "America/Santiago"} {
		data, err := zoneData(name)
		if err != nil {
			t.Fatal(err)
		}
		loc, err := time.LoadLocationFromTZData(name, data)
		if err != nil {
			t.Fatal(err)
		}
		z, err := ZoneTable(name)
		if err != nil {
			t.Fatal(err)
		}
		check := func(at int64) {
			_, want := time.Unix(at, 0).In(loc).Zone()
			if got := offsetAt(&z, at); got != int64(want) {
				t.Fatalf("%s at %s: table %d, engine %d", name, time.Unix(at, 0).UTC().Format(time.RFC3339), got, want)
			}
		}
		for at := z.Start; at < z.End; at += 3600 {
			check(at)
		}
		for _, tr := range z.Transitions {
			check(tr[0])
			check(tr[0] - 1)
		}
	}
}

func TestZoneTableFixedAndRefused(t *testing.T) {
	for name, off := range map[string]int64{"UTC": 0, "Etc/UTC": 0, "Asia/Kolkata": 19800} {
		z, err := ZoneTable(name)
		if err != nil || z.Offset != off || len(z.Transitions) != 0 {
			t.Errorf("%s: %+v %v", name, z, err)
		}
	}
	lh, err := ZoneTable("Australia/Lord_Howe")
	if err != nil || len(lh.Transitions) == 0 {
		t.Fatalf("Lord Howe: %v", err)
	}
	for _, tr := range lh.Transitions {
		if tr[1] != 37800 && tr[1] != 39600 {
			t.Fatalf("Lord Howe offsets are +10:30 and +11:00: %v", tr)
		}
	}
	for _, bad := range []string{"Mars/Olympus", "", "../etc/passwd", "america/new_york", "Local"} {
		if _, err := ZoneTable(bad); err == nil || !strings.Contains(err.Error(), "pinned") {
			t.Errorf("ZoneTable(%q) must refuse naming the pinned database: %v", bad, err)
		}
	}
}
