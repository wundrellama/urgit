package plan

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The ship's vectors embed the pinned wires as cords: `=/  <base>-1=@t`
// … `=/  <base>-N=@t` blocks of '…' chunks, joined as `=/  <base>=@t`.
// A pin rewritten after a reviewed change must be embedded again, byte for
// byte (CI-CONCURRENCY-1 added a field to every catalog workflow): this
// reads each vector's embedded text back and compares it with its pin.

var cordLiteral = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'`)

func embeddedText(t *testing.T, vector, base string) string {
	t.Helper()
	data, err := os.ReadFile(vector)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	first := regexp.MustCompile(`(?m)^=/  ` + regexp.QuoteMeta(base) + `-1=@t\n`).FindStringIndex(src)
	join := regexp.MustCompile(`(?m)^=/  ` + regexp.QuoteMeta(base) + `=@t\n`).FindStringIndex(src)
	if first == nil || join == nil || join[0] < first[0] {
		t.Fatalf("%s embeds no %s", vector, base)
	}
	var out strings.Builder
	for _, m := range cordLiteral.FindAllStringSubmatch(src[first[0]:join[0]], -1) {
		lit := m[1]
		for i := 0; i < len(lit); i++ {
			if lit[i] == '\\' && i+1 < len(lit) {
				i++
			}
			out.WriteByte(lit[i])
		}
	}
	return out.String()
}

func TestHoonVectorsEmbedThePins(t *testing.T) {
	gen := "../../../desk/gen/"
	for _, c := range []struct{ vector, base, pin string }{
		{gen + "ci-trigger-vector.hoon", "catalog", "testdata/selection/catalog.json"},
		{gen + "ci-trigger-vector.hoon", "globs", "testdata/selection/globs.json"},
		{gen + "ci-cron-vector.hoon", "schedules", "testdata/schedules/schedules.json"},
		{gen + "ci-cron-vector.hoon", "occurrences", "testdata/schedules/occurrences.json"},
		{gen + "ci-concurrency-vector.hoon", "capture-catalog", "../daemon/testdata/concurrency/catalog.json"},
		{gen + "ci-concurrency-vector.hoon", "capture-plan-jobs", "../daemon/testdata/concurrency/plan-jobs.json"},
	} {
		pin, err := os.ReadFile(c.pin)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := embeddedText(t, c.vector, c.base), strings.TrimSuffix(string(pin), "\n"); got != want {
			t.Errorf("%s embeds %s as %d bytes that are not %s's %d", c.vector, c.base, len(got), c.pin, len(want))
		}
	}
}
