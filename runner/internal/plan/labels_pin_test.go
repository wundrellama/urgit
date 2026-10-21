package plan

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Pull request labels. ERPit's fixtures.yml decides its costly jobs with
// contains(github.event.pull_request.labels.*.name, 'ci:fixtures'), in the
// plan job's env. testdata/fixtures.yml is ERPit's file byte for byte; the
// expression is read from it, compiled by the pinned compiler as the ship
// receives compiled expressions, and pinned in testdata/labels. The ship's
// vector desk/gen/ci-concurrency-vector embeds the pin (checked by
// TestHoonVectorsEmbedThePins) and evaluates it with the ship's evaluator
// against a label run's real payload: true with the label, false without.

var labeledEnv = regexp.MustCompile(`(?m)^(\s+LABELED: )(\$\{\{ .* \}\})$`)

func TestERPitLabelExpressionIsPinned(t *testing.T) {
	data, err := os.ReadFile("testdata/fixtures.yml")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	m := labeledEnv.FindStringSubmatchIndex(src)
	if m == nil {
		t.Fatal("fixtures.yml has no LABELED env expression")
	}
	text := src[m[4]:m[5]]
	if text != "${{ contains(github.event.pull_request.labels.*.name, 'ci:fixtures') }}" {
		t.Fatalf("ERPit's expression is %q", text)
	}
	line := strings.Count(src[:m[4]], "\n") + 1
	col := m[4] - strings.LastIndex(src[:m[4]], "\n")
	c := CompileCondition(text, line, col)
	if c.Refusal != "" || c.Expr == nil {
		t.Fatalf("the compiler refuses ERPit's expression: %s", c.Refusal)
	}
	pinCompact(t, "testdata/labels/erpit-labeled.json", c)
}
