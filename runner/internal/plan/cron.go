package plan

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Schedule is one `on.schedule` entry, compiled (Delivery 1C): the cron
// text as written, the IANA timezone it names (empty for the UTC default), and
// each field as its sorted values. DayStar and WeekdayStar say whether the
// day-of-month and day-of-week fields begin with `*` (`*` or `*/n`): then a
// day must match both, else either (the Vixie/cronie rule POSIX describes
// for lists). Refusal is why the compiler refuses the entry; a refused
// entry carries no values and never fires.
type Schedule struct {
	Cron        string `json:"cron"`
	Timezone    string `json:"timezone"`
	Refusal     string `json:"refusal"`
	Minutes     []int  `json:"minutes"`
	Hours       []int  `json:"hours"`
	Days        []int  `json:"days"`
	Months      []int  `json:"months"`
	Weekdays    []int  `json:"weekdays"`
	DayStar     bool   `json:"dayStar"`
	WeekdayStar bool   `json:"weekdayStar"`
}

// MinScheduleGap is the shortest interval the captured reference allows.
const MinScheduleGap = 5

// MaxSearchDays bounds a next-occurrence search: eight years and two days,
// so a 29 February schedule finds its day across a century's skipped leap
// year.
const MaxSearchDays = 8*366 + 2

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var weekdayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// ParseCron compiles the five fields of one cron line: `*`, value lists,
// ascending ranges, steps (`*/n`, `a-b/n`, and `a/n`, which runs from a to
// the field's end: the reference's `20/15` runs at 20, 35 and 50), and
// three-letter month and weekday names in any case. It refuses the
// non-standard @ macros, a field outside its documented range (day of the
// week is 0-6), a schedule more frequent than every five minutes (never
// clamped), and one that never occurs.
func ParseCron(text string) (Schedule, error) {
	s := Schedule{Cron: text}
	if strings.ContainsAny(text, "\n\r\v\f") {
		return s, errors.New("the cron line holds a line break; it has five fields on one line")
	}
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '\t' })
	if len(fields) == 1 && strings.HasPrefix(fields[0], "@") {
		return s, fmt.Errorf("%s is non-standard cron syntax that GitHub Actions does not support", fields[0])
	}
	if len(fields) != 5 {
		return s, fmt.Errorf("a cron line has five fields (minute, hour, day of the month, month, day of the week); this one has %d", len(fields))
	}
	var err error
	if s.Minutes, err = cronField(fields[0], 0, 59, nil, "minute"); err != nil {
		return s, err
	}
	if s.Hours, err = cronField(fields[1], 0, 23, nil, "hour"); err != nil {
		return s, err
	}
	if s.Days, err = cronField(fields[2], 1, 31, nil, "day of the month"); err != nil {
		return s, err
	}
	if s.Months, err = cronField(fields[3], 1, 12, monthNames, "month"); err != nil {
		return s, err
	}
	if s.Weekdays, err = cronField(fields[4], 0, 6, weekdayNames, "day of the week"); err != nil {
		return s, err
	}
	s.DayStar = strings.HasPrefix(fields[2], "*")
	s.WeekdayStar = strings.HasPrefix(fields[4], "*")
	if gap := s.minGap(); gap < MinScheduleGap {
		return s, fmt.Errorf("the schedule runs %d minute(s) apart; GitHub Actions runs scheduled workflows at most once every five minutes, and this one is refused rather than clamped", gap)
	}
	if !s.occurs() {
		return s, errors.New("the schedule never occurs: none of its months has that day of the month")
	}
	return s, nil
}

func cronField(text string, lo, hi int, names map[string]int, label string) ([]int, error) {
	value := func(v string) (int, error) {
		if n, ok := names[strings.ToLower(v)]; ok && len(v) == 3 {
			return n, nil
		}
		for _, r := range v {
			if r < '0' || r > '9' {
				if names != nil {
					return 0, fmt.Errorf("%s %q is not a number or a %s name", label, v, label)
				}
				return 0, fmt.Errorf("%s %q is not a number", label, v)
			}
		}
		n, err := strconv.Atoi(v)
		if err != nil || v == "" {
			return 0, fmt.Errorf("%s %q is not a number", label, v)
		}
		if n < lo || n > hi {
			return 0, fmt.Errorf("%s %d is outside %d-%d", label, n, lo, hi)
		}
		return n, nil
	}
	set := map[int]bool{}
	for _, item := range strings.Split(text, ",") {
		if item == "" {
			return nil, fmt.Errorf("the %s field has an empty list item", label)
		}
		base, step, stepped := item, 1, false
		if i := strings.IndexByte(item, '/'); i >= 0 {
			base, stepped = item[:i], true
			n, err := strconv.Atoi(item[i+1:])
			if err != nil || n < 1 || strings.ContainsAny(item[i+1:], "+-") {
				return nil, fmt.Errorf("the %s step %q is not a positive whole number", label, item[i+1:])
			}
			step = n
		}
		var a, b int
		switch {
		case base == "*":
			a, b = lo, hi
		case strings.Contains(base, "-"):
			parts := strings.Split(base, "-")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("the %s range %q is not two values joined by -", label, base)
			}
			var err error
			if a, err = value(parts[0]); err != nil {
				return nil, err
			}
			if b, err = value(parts[1]); err != nil {
				return nil, err
			}
			if a > b {
				return nil, fmt.Errorf("the %s range %q descends", label, base)
			}
		default:
			var err error
			if a, err = value(base); err != nil {
				return nil, err
			}
			b = a
			if stepped {
				b = hi
			}
		}
		for v := a; v <= b; v += step {
			set[v] = true
		}
	}
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

// minGap is the shortest number of minutes between two consecutive times of
// a day the schedule selects: within an hour, and from the last minute of an
// hour to the first of the next selected hour (23 to 0 counted, whatever
// the days: a conservative rule both the compiler and the ship apply)
func (s Schedule) minGap() int {
	gap := 60
	for i := 0; i+1 < len(s.Minutes); i++ {
		if d := s.Minutes[i+1] - s.Minutes[i]; d < gap {
			gap = d
		}
	}
	hours := map[int]bool{}
	for _, h := range s.Hours {
		hours[h] = true
	}
	for _, h := range s.Hours {
		if hours[(h+1)%24] {
			if d := 60 - s.Minutes[len(s.Minutes)-1] + s.Minutes[0]; d < gap {
				gap = d
			}
			break
		}
	}
	return gap
}

// whether any date satisfies the day fields: under the either rule (both
// restricted) or a day-of-month field beginning with `*` (which holds the
// 1st), always; else some month must have one of the days (every real date
// falls on every weekday in some year)
func (s Schedule) occurs() bool {
	if s.DayStar || !s.WeekdayStar {
		return true
	}
	maxDay := map[int]int{1: 31, 2: 29, 3: 31, 4: 30, 5: 31, 6: 30, 7: 31, 8: 31, 9: 30, 10: 31, 11: 30, 12: 31}
	for _, m := range s.Months {
		for _, d := range s.Days {
			if d <= maxDay[m] {
				return true
			}
		}
	}
	return false
}

func contains(set []int, v int) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

// dayMatches is the day rule for one calendar date
func (s Schedule) dayMatches(d time.Time) bool {
	if !contains(s.Months, int(d.Month())) {
		return false
	}
	dom, dow := contains(s.Days, d.Day()), contains(s.Weekdays, int(d.Weekday()))
	if s.DayStar || s.WeekdayStar {
		return dom && dow
	}
	return dom || dow
}

// localToUTC maps a wall-clock time of the zone (as seconds, read as if it
// were UTC) to its instant: the earliest instant showing it (a time repeated
// at fall-back fires at its first instance), or, for a time skipped at
// spring-forward, the transition instant (the first valid time after the
// gap). ok is false when the time falls before the table's start; beyond is
// true past its end.
func localToUTC(z *Zone, local int64) (at int64, ok bool, beyond bool) {
	type period struct{ from, to, off int64 }
	periods := []period{}
	from, off := z.Start, z.Offset
	for _, tr := range z.Transitions {
		periods = append(periods, period{from, tr[0], off})
		from, off = tr[0], tr[1]
	}
	periods = append(periods, period{from, z.End, off})
	found := false
	for _, p := range periods {
		u := local - p.off
		if u >= p.from && u < p.to && (!found || u < at) {
			at, found = u, true
		}
	}
	if found {
		return at, true, false
	}
	for k := 0; k+1 < len(periods); k++ {
		b := periods[k].to
		if local >= b+periods[k].off && local < b+periods[k+1].off {
			return b, true, false
		}
	}
	last := periods[len(periods)-1]
	if local-last.off >= last.to {
		return 0, false, true
	}
	return 0, false, false
}

// offsetAt is the zone's offset at an instant inside the horizon
func offsetAt(z *Zone, at int64) int64 {
	off := z.Offset
	for _, tr := range z.Transitions {
		if at >= tr[0] {
			off = tr[1]
		}
	}
	return off
}

// NextOccurrence is the first due instant strictly after after: in UTC when
// z is nil, else in the zone over its horizon (refusing an instant outside
// it, never another zone). Local wall-clock times are searched in order;
// their instants never decrease, so the first after after is the next.
func NextOccurrence(s Schedule, z *Zone, after time.Time) (time.Time, error) {
	if s.Refusal != "" {
		return time.Time{}, errors.New("a refused schedule has no occurrence")
	}
	var off int64
	if z != nil {
		if after.Unix() < z.Start || after.Unix() >= z.End {
			return time.Time{}, fmt.Errorf("%s is outside the timezone horizon %s to %s of the pinned table", after.UTC().Format(time.RFC3339), time.Unix(z.Start, 0).UTC().Format(time.RFC3339), time.Unix(z.End, 0).UTC().Format(time.RFC3339))
		}
		off = offsetAt(z, after.Unix())
	}
	local := after.Unix() + off
	first := time.Unix(local, 0).UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	for d := 0; d <= MaxSearchDays; d++ {
		day := first.AddDate(0, 0, d)
		if !s.dayMatches(day) {
			continue
		}
		for _, h := range s.Hours {
			for _, m := range s.Minutes {
				wall := day.Unix() + int64(h)*3600 + int64(m)*60
				at := wall
				if z != nil {
					var ok, beyond bool
					at, ok, beyond = localToUTC(z, wall)
					if beyond {
						return time.Time{}, fmt.Errorf("the next occurrence is beyond the timezone horizon %s of the pinned table: resolve the workflow again with a newer compiler", time.Unix(z.End, 0).UTC().Format(time.RFC3339))
					}
					if !ok {
						continue
					}
				}
				if t := time.Unix(at, 0).UTC(); t.After(after) {
					return t, nil
				}
			}
		}
	}
	return time.Time{}, fmt.Errorf("the schedule has no occurrence within %d days", MaxSearchDays)
}
