package sig

import (
	"math/big"
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// canonicalShape is a @uv as Hoon writes it (scot %uv): 0v0, or 0v and the
// base-32 digits without a leading zero, grouped by five from the right.
var canonicalShape = regexp.MustCompile(`^0v(0|[1-9a-v][0-9a-v]{0,4}(\.[0-9a-v]{5})*)$`)

// Every spelling ParseUV reads of one atom — a separator anywhere, leading
// zeros — has one canonical spelling, FormatUV's; a text that is no @uv has
// none (independent review 08, R8-1).
func TestCanonicalUVIsTheOneSpellingOfAnAtom(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"0v0", "0v0"},
		{"0v00", "0v0"},
		{"0v0.00000", "0v0"},
		{"0v5cmd", "0v5cmd"},
		{"0v5.cmd", "0v5cmd"},
		{"0v05cmd", "0v5cmd"},
		{"0v5c.md", "0v5cmd"},
		{"0v0.05cmd", "0v5cmd"},
		{"0v.5cmd.", "0v5cmd"},
		{"0vvvvvv", "0vvvvvv"},
		{"0v10000.0", "0v1.00000"},
		{"0vabcdef", "0va.bcdef"},
		{"0vabcdefghij", "0vabcde.fghij"},
		{"0v1abcde", "0v1.abcde"},
		{"0v1.abcde", "0v1.abcde"},
	} {
		got, err := CanonicalUV(c.text)
		if err != nil || got != c.want {
			t.Errorf("A SPELLING OF AN ATOM WAS NOT GIVEN THE ONE HOON WRITES: CanonicalUV(%q) = %q, %v; want %q", c.text, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "0v", "0v.", "0x1", "5cmd", "0v1w", "0V5cmd", "0v5 cmd"} {
		if got, err := CanonicalUV(bad); err == nil {
			t.Errorf("A TEXT THAT IS NO @UV WAS GIVEN A SPELLING: CanonicalUV(%q) = %q", bad, got)
		}
	}
	if got := FormatUV(new(big.Int).Lsh(big.NewInt(1), 50)); got != "0v1.00000.00000" {
		t.Errorf("A SPELLING OF AN ATOM WAS NOT GIVEN THE ONE HOON WRITES: FormatUV(32^10) = %q", got)
	}
}

// FormatUV of any atom has the canonical shape and reads back as that
// atom; any respelling of it reads back to the same canonical spelling.
func TestFormatUVRoundTripsAndRespellingsAgree(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	for i := 0; i < 500; i++ {
		v := new(big.Int).Rand(r, new(big.Int).Lsh(big.NewInt(1), uint(1+r.Intn(160))))
		text := FormatUV(v)
		if !canonicalShape.MatchString(text) {
			t.Fatalf("AN ATOM WAS NOT SPELLED IN THE SHAPE HOON WRITES: FormatUV(%s) = %q", v, text)
		}
		back, err := ParseUV(text)
		if err != nil || back.Cmp(v) != 0 {
			t.Fatalf("AN ATOM'S SPELLING DID NOT READ BACK AS THE ATOM: ParseUV(FormatUV(%s)) = %v, %v", v, back, err)
		}
		digits := strings.ReplaceAll(text[2:], ".", "")
		cut := r.Intn(len(digits) + 1)
		alias := "0v" + strings.Repeat("0", r.Intn(3)) + digits[:cut] + "." + digits[cut:]
		if got, err := CanonicalUV(alias); err != nil || got != text {
			t.Fatalf("A RESPELLING OF AN ATOM WAS NOT GIVEN ITS ONE SPELLING: CanonicalUV(%q) = %q, %v; want %q", alias, got, err, text)
		}
	}
}
