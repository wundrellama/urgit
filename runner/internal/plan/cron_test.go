package plan

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func span(a, b int) []int {
	var out []int
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}

func every(a, b, step int) []int {
	var out []int
	for i := a; i <= b; i += step {
		out = append(out, i)
	}
	return out
}

// the captured schedule reference's grammar: five fields, *, lists, ranges,
// steps (`20/15` runs at 20, 35 and 50), month and weekday names
func TestCronParsesTheDocumentedGrammar(t *testing.T) {
	type want struct {
		min, hour, day, month, wday []int
		dayStar, wdayStar           bool
	}
	cases := map[string]want{
		"15 4,5 * * *":                {[]int{15}, []int{4, 5}, span(1, 31), span(1, 12), span(0, 6), true, true},
		"20/15 * * * *":               {[]int{20, 35, 50}, span(0, 23), span(1, 31), span(1, 12), span(0, 6), true, true},
		"30 4-6 * * *":                {[]int{30}, []int{4, 5, 6}, span(1, 31), span(1, 12), span(0, 6), true, true},
		"30 5 * * 1-5":                {[]int{30}, []int{5}, span(1, 31), span(1, 12), span(1, 5), true, false},
		"0 0 * jan,FEB mon-fri":       {[]int{0}, []int{0}, span(1, 31), []int{1, 2}, span(1, 5), true, false},
		"*/10 */6 1,15 * *":           {every(0, 59, 10), []int{0, 6, 12, 18}, []int{1, 15}, span(1, 12), span(0, 6), false, true},
		"0 12 */2 * 1":                {[]int{0}, []int{12}, every(1, 31, 2), span(1, 12), []int{1}, true, false},
		"5-20/5 * * * *":              {[]int{5, 10, 15, 20}, span(0, 23), span(1, 31), span(1, 12), span(0, 6), true, true},
		"0 0 1-7 * sun":               {[]int{0}, []int{0}, span(1, 7), span(1, 12), []int{0}, false, false},
		"  0\t0  * *   SAT,sun ":      {[]int{0}, []int{0}, span(1, 31), span(1, 12), []int{0, 6}, true, false},
		"0 0 * DEC-dec */3":           {[]int{0}, []int{0}, span(1, 31), []int{12}, []int{0, 3, 6}, true, true},
		"0,30 9-17/4 * MAR-MAY,OCT *": {[]int{0, 30}, []int{9, 13, 17}, span(1, 31), []int{3, 4, 5, 10}, span(0, 6), true, true},
	}
	for text, w := range cases {
		s, err := ParseCron(text)
		if err != nil {
			t.Errorf("ParseCron(%q): %v", text, err)
			continue
		}
		got := want{s.Minutes, s.Hours, s.Days, s.Months, s.Weekdays, s.DayStar, s.WeekdayStar}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("ParseCron(%q) = %+v, want %+v", text, got, w)
		}
		if s.Cron != text {
			t.Errorf("ParseCron(%q) keeps the text as written, got %q", text, s.Cron)
		}
	}
}

// what the reference does not define, or GitHub does not accept, is refused
// with its reason; nothing is clamped
func TestCronRefusesWithAReason(t *testing.T) {
	refused := map[string]string{
		"@daily":            "non-standard",
		"@reboot":           "non-standard",
		"@hourly":           "non-standard",
		"* * * *":           "five fields",
		"0 0 * * * *":       "five fields",
		"":                  "five fields",
		"60 * * * *":        "minute",
		"0 24 * * *":        "hour",
		"0 0 0 * *":         "day of the month",
		"0 0 32 * *":        "day of the month",
		"0 0 * 13 *":        "month",
		"0 0 * 0 *":         "month",
		"0 0 * * 7":         "day of the week",
		"0 0 * foo *":       "month",
		"0 0 * * funday":    "day of the week",
		"5-1 * * * *":       "range",
		"*/0 * * * *":       "step",
		"0 0 * * mon-":      "range",
		"a * * * *":         "minute",
		"0,,5 * * * *":      "empty",
		"-5 * * * *":        "range",
		"1/x * * * *":       "step",
		"*/4 * * * *":       "five minutes",
		"* * * * *":         "five minutes",
		"0,3 * * * *":       "five minutes",
		"2,58 * * * *":      "five minutes",
		"0,58 0,23 * * *":   "five minutes",
		"0,58 4,5 * * *":    "five minutes",
		"0 0 30 2 *":        "never",
		"0 0 31 4,6,9,11 *": "never",
		"0 0 30,31 2 *":     "never",
	}
	for text, want := range refused {
		_, err := ParseCron(text)
		if err == nil {
			t.Errorf("ParseCron(%q) should be refused", text)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ParseCron(%q) = %q, want a reason naming %q", text, err, want)
		}
	}
	for _, ok := range []string{"*/5 * * * *", "0 * * * *", "58 * * * *", "0,30 * * * *", "0 0 29 2 *", "0 0 31 * *", "55 23 * * *", "0,55 0,12 * * *", "0,5 0 * * *"} {
		if _, err := ParseCron(ok); err != nil {
			t.Errorf("ParseCron(%q) should be accepted: %v", ok, err)
		}
	}
}

// an independent oracle: every minute after the instant, in UTC, until one
// matches the compiled fields under the either/both day rule
func bruteNextUTC(s Schedule, after time.Time) time.Time {
	in := func(v int, set []int) bool {
		for _, x := range set {
			if x == v {
				return true
			}
		}
		return false
	}
	at := after.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 60*24*366*9; i++ {
		u := at.Add(time.Duration(i) * time.Minute)
		dom, dow := in(u.Day(), s.Days), in(int(u.Weekday()), s.Weekdays)
		day := dom || dow
		if s.DayStar || s.WeekdayStar {
			day = dom && dow
		}
		if in(u.Minute(), s.Minutes) && in(u.Hour(), s.Hours) && in(int(u.Month()), s.Months) && day {
			return u
		}
	}
	return time.Time{}
}

func utc(text string) time.Time {
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestCronNextOccurrenceInUTCAgreesWithTheMinuteOracle(t *testing.T) {
	crons := []string{"15 4,5 * * *", "*/5 * * * *", "30 5 * * 1-5", "0 0 13 * 5", "0 0 29 2 *", "0 12 */2 * 1", "0 0 31 * *", "30 23 * * 6", "0 0 1 1 *", "45 */7 * * *"}
	afters := []string{"2026-10-16T00:00:00Z", "2026-10-16T04:15:00Z", "2026-12-31T23:59:30Z", "2027-02-28T12:00:00Z", "2028-02-28T00:00:00Z", "2026-10-16T04:15:00.5Z"}
	for _, c := range crons {
		s, err := ParseCron(c)
		if err != nil {
			t.Fatalf("%q: %v", c, err)
		}
		for _, a := range afters {
			after := utc(a)
			got, err := NextOccurrence(s, nil, after)
			if err != nil {
				t.Errorf("%q after %s: %v", c, a, err)
				continue
			}
			want := bruteNextUTC(s, after)
			if !got.Equal(want) {
				t.Errorf("%q after %s = %s, oracle %s", c, a, got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
		}
	}
}

// the day rule, on dates known by hand: 2026-10-16 is a Friday, 2026-10-19
// a Monday, and 2028 is the next leap year
func TestCronDayRuleAndLeapDay(t *testing.T) {
	cases := []struct{ cron, after, want string }{
		// both restricted: either the 13th or a Friday
		{"0 0 13 * 5", "2026-10-15T12:00:00Z", "2026-10-16T00:00:00Z"},
		// a day of the month that begins with * is unrestricted for the
		// rule: an odd day AND a Monday (either would be 2026-10-17)
		{"0 12 */2 * 1", "2026-10-16T00:00:00Z", "2026-10-19T12:00:00Z"},
		{"0 0 29 2 *", "2026-10-16T00:00:00Z", "2028-02-29T00:00:00Z"},
		// strictly after: an instant that is itself due is not next
		{"15 4,5 * * *", "2026-10-16T04:15:00Z", "2026-10-16T05:15:00Z"},
	}
	for _, c := range cases {
		s, err := ParseCron(c.cron)
		if err != nil {
			t.Fatal(err)
		}
		got, err := NextOccurrence(s, nil, utc(c.after))
		if err != nil || !got.Equal(utc(c.want)) {
			t.Errorf("%q after %s = %v (%v), want %s", c.cron, c.after, got, err, c.want)
		}
	}
}

func mustZone(t *testing.T, name string) *Zone {
	t.Helper()
	z, err := ZoneTable(name)
	if err != nil {
		t.Fatalf("ZoneTable(%q): %v", name, err)
	}
	return &z
}

// DST, hand-computed for America/New_York (2027-03-14 02:00 EST springs to
// 03:00 EDT, 07:00Z; 2027-11-07 02:00 EDT falls back to 01:00 EST, 06:00Z):
// a skipped local time runs at the transition (the captured note: 2:30
// advances to 3:00), several skipped times collapse into that one instant,
// and a repeated local time runs once, at its first instance (native
// choice; the reference documents none)
func TestCronTimezoneDaylightSaving(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	cases := []struct{ cron, after, want string }{
		{"30 2 * * *", "2027-03-13T12:00:00Z", "2027-03-14T07:00:00Z"},
		{"30 2 * * *", "2027-03-14T07:00:00Z", "2027-03-15T06:30:00Z"},
		{"*/15 2 * * *", "2027-03-14T06:50:00Z", "2027-03-14T07:00:00Z"},
		{"*/15 2 * * *", "2027-03-14T07:00:00Z", "2027-03-15T06:00:00Z"},
		{"0,30 2,3 * * *", "2027-03-14T06:00:00Z", "2027-03-14T07:00:00Z"},
		{"0,30 2,3 * * *", "2027-03-14T07:00:00Z", "2027-03-14T07:30:00Z"},
		{"0 3 * * *", "2027-03-14T06:00:00Z", "2027-03-14T07:00:00Z"},
		{"30 1 * * *", "2027-11-07T04:00:00Z", "2027-11-07T05:30:00Z"},
		{"30 1 * * *", "2027-11-07T05:30:00Z", "2027-11-08T06:30:00Z"},
		{"30 1 * * *", "2027-11-07T06:15:00Z", "2027-11-08T06:30:00Z"},
		{"0 2 * * *", "2027-11-07T05:00:00Z", "2027-11-07T07:00:00Z"},
		// the reference's own example: 5:30 in New York, Monday to Friday
		{"30 5 * * 1-5", "2026-10-16T12:00:00Z", "2026-10-19T09:30:00Z"},
		{"30 5 * * 1-5", "2026-12-04T12:00:00Z", "2026-12-07T10:30:00Z"},
	}
	for _, c := range cases {
		s, err := ParseCron(c.cron)
		if err != nil {
			t.Fatal(err)
		}
		got, err := NextOccurrence(s, ny, utc(c.after))
		if err != nil || !got.Equal(utc(c.want)) {
			t.Errorf("%q NY after %s = %v (%v), want %s", c.cron, c.after, got.UTC(), err, c.want)
		}
	}
	// a zone without daylight saving
	kol := mustZone(t, "Asia/Kolkata")
	s, _ := ParseCron("30 5 * * *")
	if got, err := NextOccurrence(s, kol, utc("2026-10-16T01:00:00Z")); err != nil || !got.Equal(utc("2026-10-17T00:00:00Z")) {
		t.Errorf("05:30 in Kolkata after 01:00Z = %v (%v)", got, err)
	}
}

// a half-hour DST shift (Lord Howe springs 02:00 → 02:30 on the first
// Sunday of October): a skipped 02:15 runs at the transition instant
func TestCronHalfHourDaylightSaving(t *testing.T) {
	lh := mustZone(t, "Australia/Lord_Howe")
	var spring int64
	for _, tr := range lh.Transitions {
		at := time.Unix(tr[0], 0).UTC()
		if at.Year() == 2026 && at.Month() == time.October {
			spring = tr[0]
		}
	}
	if spring == 0 {
		t.Fatal("no October 2026 transition in the Lord Howe table")
	}
	s, _ := ParseCron("15 2 * * *")
	got, err := NextOccurrence(s, lh, time.Unix(spring-3600, 0))
	if err != nil || got.Unix() != spring {
		t.Fatalf("02:15 on the spring-forward day = %v (%v), want the transition %s", got.UTC(), err, time.Unix(spring, 0).UTC())
	}
}

// the horizon is fixed and named: an occurrence beyond it refuses, it never
// falls back to UTC or the host's zone
func TestCronZoneHorizonRefuses(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	s, _ := ParseCron("0 0 * * *")
	if got, err := NextOccurrence(s, ny, utc("2039-12-30T04:00:00Z")); err != nil || !got.Equal(utc("2039-12-30T05:00:00Z")) {
		t.Errorf("inside the horizon: %v (%v)", got, err)
	}
	if _, err := NextOccurrence(s, ny, utc("2039-12-31T06:00:00Z")); err == nil || !strings.Contains(err.Error(), "horizon") {
		t.Errorf("past the horizon must refuse naming it: %v", err)
	}
}
