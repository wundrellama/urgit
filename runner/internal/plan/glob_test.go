package plan

import (
	"strings"
	"testing"
)

// The filter pattern cheat sheet of the captured GitHub workflow syntax
// reference (.scratch reference github-workflow-syntax.md §"Filter pattern
// cheat sheet", captured 2026-10-16): every example row, matched and not.

type globCase struct {
	pattern string
	match   []string
	miss    []string
}

// the tables are shared with the ship: vector_pin_test.go pins them, with
// their compiled tokens, as the glob table desk/gen/ci-trigger-vector checks
var globBranchCases = []globCase{
	{"feature/*", []string{"feature/my-branch", "feature/your-branch"}, []string{"feature/beta-a/my-branch", "feature", "main"}},
	{"feature/**", []string{"feature/beta-a/my-branch", "feature/your-branch", "feature/mona/the/octocat"}, []string{"features/x", "main"}},
	{"main", []string{"main"}, []string{"mainline", "a/main", "Main"}},
	{"releases/mona-the-octocat", []string{"releases/mona-the-octocat"}, []string{"releases/mona"}},
	{"*", []string{"main", "releases"}, []string{"releases/10", "feature/x"}},
	{"**", []string{"all/the/branches", "every/tag", "main"}, nil},
	{"*feature", []string{"mona-feature", "feature", "ver-10-feature"}, []string{"feature-x", "a/feature"}},
	{"v2*", []string{"v2", "v2.0", "v2.9"}, []string{"v1", "v3.0", "release/v2"}},
	{"v[12].[0-9]+.[0-9]+", []string{"v1.10.1", "v2.0.0"}, []string{"v3.0.0", "v1.x.1", "v1..1", "v1.0."}},
	{"releases/**-alpha", []string{"releases/10-alpha", "releases/beta/3-alpha"}, []string{"releases/10", "releases/beta/mona"}},
}

func TestGlobBranchAndTagCheatSheet(t *testing.T) {
	runGlobCases(t, globBranchCases)
}

var globPathCases = []globCase{
	{"*", []string{"README.md", "server.rb"}, []string{"docs/README.md"}},
	{"*.jsx?", []string{"page.js", "page.jsx"}, []string{"page.jsxx", "src/page.js"}},
	{"**", []string{"all/the/files.md", "x"}, nil},
	{"*.js", []string{"app.js", "index.js"}, []string{"js/index.js", "app.jsx"}},
	{"**.js", []string{"index.js", "js/index.js", "src/js/app.js"}, []string{"index.jsx"}},
	{"docs/*", []string{"docs/README.md", "docs/file.txt"}, []string{"docs/mona/octocat.txt", "README.md"}},
	{"docs/**", []string{"docs/README.md", "docs/mona/octocat.txt"}, []string{"doc/x", "x/docs/y"}},
	{"docs/**/*.md", []string{"docs/README.md", "docs/mona/hello-world.md", "docs/a/markdown/file.md"}, []string{"docs/a.txt", "x/docs/a.md"}},
	{"**/docs/**", []string{"docs/hello.md", "dir/docs/my-file.txt", "space/docs/plan/space.doc"}, []string{"docsx/a", "dir/mydocs/a"}},
	{"**/README.md", []string{"README.md", "js/README.md"}, []string{"README.mdx", "xREADME.md"}},
	{"**/*src/**", []string{"a/src/app.js", "my-src/code/js/app.js"}, []string{"src", "a/source/x"}},
	{"**/*-post.md", []string{"my-post.md", "path/their-post.md"}, []string{"post.md", "path/post.md"}},
	{"**/migrate-*.sql", []string{"migrate-10909.sql", "db/migrate-v1.0.sql", "db/sept/migrate-v1.sql"}, []string{"migrate.sql", "db/migrate-v1/x.sql"}},
	{"sub-project/**", []string{"sub-project/index.js", "sub-project/src/index.js", "sub-project/docs/readme.md"}, []string{"sub-project2/x"}},
}

func TestGlobPathCheatSheet(t *testing.T) {
	runGlobCases(t, globPathCases)
}

// a backslash escapes the next character: `release\*` is the name
// `release*`, and `[CB]at` is a class
var globEscapeCases = []globCase{
	{`release\*`, []string{"release*"}, []string{"release", "release-1"}},
	{"[CB]at", []string{"Cat", "Bat"}, []string{"cat", "Hat", "CBat"}},
	{"[1-2]00", []string{"100", "200"}, []string{"300", "1200"}},
	{"[0-9a-z]x", []string{"5x", "qx"}, []string{"Qx", "-x"}},
	{`a\?b`, []string{"a?b"}, []string{"ab", "b"}},
}

func TestGlobEscapesAndClasses(t *testing.T) {
	runGlobCases(t, globEscapeCases)
}

func runGlobCases(t *testing.T, cases []globCase) {
	t.Helper()
	for _, c := range cases {
		p, err := CompileGlob(c.pattern)
		if err != nil {
			t.Errorf("CompileGlob(%q): %v", c.pattern, err)
			continue
		}
		for _, name := range c.match {
			if !p.Match(name) {
				t.Errorf("%q should match %q", c.pattern, name)
			}
		}
		for _, name := range c.miss {
			if p.Match(name) {
				t.Errorf("%q should not match %q", c.pattern, name)
			}
		}
	}
}

// a leading ! negates; it has no special meaning elsewhere
func TestGlobNegationIsALeadingBangOnly(t *testing.T) {
	p, err := CompileGlob("!README.md")
	if err != nil || !p.Negated || p.Text != "!README.md" || !p.Match("README.md") {
		t.Fatalf("!README.md compiles negated and matches README.md: %+v %v", p, err)
	}
	q, err := CompileGlob("a!b")
	if err != nil || q.Negated || !q.Match("a!b") {
		t.Fatalf("a!b is literal: %+v %v", q, err)
	}
}

// the forms the reference does not define are refused, never guessed
func TestGlobRefusesMalformedPatterns(t *testing.T) {
	refused := map[string]string{
		"":                                     "empty",
		"!":                                    "empty",
		"?abc":                                 "nothing before",
		"+abc":                                 "nothing before",
		"a*?":                                  "wildcard",
		"a**+":                                 "wildcard",
		"a??":                                  "quantifier",
		"a+?":                                  "quantifier",
		"[a-":                                  "class",
		"[]x":                                  "class",
		"[z-a]":                                "class",
		"[a-9]":                                "class",
		"[_]":                                  "class",
		`trailing\`:                            "escape",
		"é?":                                   "non-ASCII",
		strings.Repeat("a", MaxPatternBytes+1): "bytes",
	}
	for text, want := range refused {
		_, err := CompileGlob(text)
		if err == nil {
			t.Errorf("CompileGlob(%q) should be refused", text)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("CompileGlob(%q) = %q, want a reason naming %q", text, err, want)
		}
	}
}

// a pattern of the bound itself compiles; a non-ASCII literal is kept bytewise
func TestGlobBoundAndNonASCIILiteral(t *testing.T) {
	if _, err := CompileGlob(strings.Repeat("a", MaxPatternBytes)); err != nil {
		t.Fatalf("a pattern of exactly %d bytes compiles: %v", MaxPatternBytes, err)
	}
	p, err := CompileGlob("docs/é*")
	if err != nil || !p.Match("docs/éte") || p.Match("docs/ete") {
		t.Fatalf("a non-ASCII literal matches bytewise: %v", err)
	}
}

// the tokens are the ship's wire: a fixed shape for each construct
func TestGlobTokens(t *testing.T) {
	p, err := CompileGlob(`**/a*[0-2]+\?**`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Token{
		{T: "dirs"},
		{T: "lit", C: "a"},
		{T: "star"},
		{T: "class", Set: "012"},
		{T: "many", Of: &Token{T: "class", Set: "012"}},
		{T: "lit", C: "?"},
		{T: "globstar"},
	}
	if len(p.Tokens) != len(want) {
		t.Fatalf("tokens %+v, want %+v", p.Tokens, want)
	}
	for i := range want {
		got, w := p.Tokens[i], want[i]
		if got.T != w.T || got.C != w.C || got.Set != w.Set || (got.Of == nil) != (w.Of == nil) || (got.Of != nil && (got.Of.T != w.Of.T || got.Of.Set != w.Of.Set || got.Of.C != w.Of.C)) {
			t.Fatalf("token %d = %+v, want %+v", i, got, w)
		}
	}
	q, err := CompileGlob("x?")
	if err != nil || len(q.Tokens) != 1 || q.Tokens[0].T != "opt" || q.Tokens[0].Of == nil || q.Tokens[0].Of.C != "x" {
		t.Fatalf("x? is one optional literal: %+v %v", q.Tokens, err)
	}
	// literal text travels as runs, each whole UTF-8; a quantifier takes the
	// run's last byte alone
	runs := map[string][]string{
		"abc?":    {"lit:ab", "opt:c"},
		"docs/é*": {"lit:docs/é", "star:"},
		`a\?b`:    {"lit:a?b"},
		"ab+c":    {"lit:ab", "many:b", "lit:c"},
	}
	for text, want := range runs {
		r, err := CompileGlob(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		var got []string
		for _, tk := range r.Tokens {
			v := tk.C
			if tk.Of != nil {
				v = tk.Of.C
			}
			got = append(got, tk.T+":"+v)
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%q tokens %v, want %v", text, got, want)
		}
	}
}
