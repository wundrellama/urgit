package plan

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"sync"
	"time"
)

// the pinned timezone database (tzdata/README.md): the only source of zone
// rules, never the host's database, ZONEINFO or the local zone
//
//go:embed tzdata/zoneinfo.zip
var zoneinfoZip []byte

// ZoneHorizonStart and ZoneHorizonEnd bound every zone table (Delivery 1C,
// a fixed, named horizon): the UTC instants 2020-01-01T00:00:00Z and
// 2040-01-01T00:00:00Z. An occurrence outside it refuses; resolving again
// with a newer compiler is the refresh path.
const (
	ZoneHorizonStart int64 = 1577836800
	ZoneHorizonEnd   int64 = 2208988800
)

// Zone is one IANA timezone's UTC offsets over the horizon, as the ship
// reads them (lib/ci-cron): the offset in seconds east of UTC at Start,
// then each transition [instant, offset from it on], ascending, strictly
// inside the horizon.
type Zone struct {
	Name        string     `json:"-"`
	Start       int64      `json:"start"`
	End         int64      `json:"end"`
	Offset      int64      `json:"offset"`
	Transitions [][2]int64 `json:"transitions"`
}

var (
	zoneOnce  sync.Once
	zoneFiles map[string]*zip.File
	zoneErr   error
)

func zoneData(name string) ([]byte, error) {
	zoneOnce.Do(func() {
		r, err := zip.NewReader(bytes.NewReader(zoneinfoZip), int64(len(zoneinfoZip)))
		if err != nil {
			zoneErr = err
			return
		}
		zoneFiles = map[string]*zip.File{}
		for _, f := range r.File {
			zoneFiles[f.Name] = f
		}
	})
	if zoneErr != nil {
		return nil, fmt.Errorf("the pinned timezone database cannot be read: %w", zoneErr)
	}
	f, ok := zoneFiles[name]
	if !ok || name == "" {
		return nil, fmt.Errorf("timezone %q is not in the pinned timezone database (Go 1.27.1 zoneinfo)", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// ZoneTable reads one zone from the pinned database and lists its offsets
// over the horizon. Go's Location keeps its transitions private, so the
// table is found by sampling every six hours and narrowing each change to
// its exact second; no zone of the database changes twice within six
// hours inside the horizon.
func ZoneTable(name string) (Zone, error) {
	data, err := zoneData(name)
	if err != nil {
		return Zone{}, err
	}
	loc, err := time.LoadLocationFromTZData(name, data)
	if err != nil {
		return Zone{}, fmt.Errorf("timezone %q in the pinned timezone database is unreadable: %v", name, err)
	}
	offset := func(at int64) int64 {
		_, off := time.Unix(at, 0).In(loc).Zone()
		return int64(off)
	}
	z := Zone{Name: name, Start: ZoneHorizonStart, End: ZoneHorizonEnd, Offset: offset(ZoneHorizonStart), Transitions: [][2]int64{}}
	const step = 6 * 3600
	prev := z.Offset
	for at := ZoneHorizonStart; at < ZoneHorizonEnd; at += step {
		next := at + step
		if next > ZoneHorizonEnd {
			next = ZoneHorizonEnd
		}
		if offset(next-1) == prev {
			continue
		}
		// the first second of [at, next) with the new offset: lo is the
		// last second known to have the old one (the second before at,
		// whose offset the previous interval ended with; at itself for
		// the horizon's first second), so a transition exactly at a
		// sample instant is found at that instant
		lo, hi := at-1, next-1
		if at == ZoneHorizonStart {
			lo = at
		}
		for lo+1 < hi {
			mid := lo + (hi-lo)/2
			if offset(mid) == prev {
				lo = mid
			} else {
				hi = mid
			}
		}
		prev = offset(hi)
		z.Transitions = append(z.Transitions, [2]int64{hi, prev})
	}
	return z, nil
}
