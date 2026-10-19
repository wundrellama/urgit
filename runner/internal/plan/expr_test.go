package plan

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// wire renders a compiled condition as the plan carries it, for exact
// comparison with the structure the ship validates (CI-EXPR-2)
func wire(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// the documented job-condition grammar compiles to the bounded tree: every
// operator, literal, accessor and function, with GitHub's precedence
// (relational above equality, both left-associative) and the ${{ }}
// wrapper optional
func TestCompileConditionTree(t *testing.T) {
	cases := map[string]string{
		"always()": `{"k":"call","fn":"always","args":[]}`,
		"${{ needs.build.outputs.ok == 'true' }}": `{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"prop","of":{"k":"prop","of":{"k":"ctx","name":"needs"},"name":"build"},"name":"outputs"},"name":"ok"},"r":{"k":"str","s":"true"}}`,
		"!cancelled() && (failure() || inputs.force)": `{"k":"and","l":{"k":"not","a":{"k":"call","fn":"cancelled","args":[]}},"r":{"k":"or","l":{"k":"call","fn":"failure","args":[]},"r":{"k":"prop","of":{"k":"ctx","name":"inputs"},"name":"force"}}}`,
		"inputs.count == 0":                    `{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"ctx","name":"inputs"},"name":"count"},"r":{"k":"num","n":0}}`,
		"1 < 2 == true":                        `{"k":"cmp","op":"eq","l":{"k":"cmp","op":"lt","l":{"k":"num","n":1},"r":{"k":"num","n":2}},"r":{"k":"bool","b":true}}`,
		"a == b != c":                          `{"k":"cmp","op":"ne","l":{"k":"cmp","op":"eq","l":{"k":"ctx","name":"a"},"r":{"k":"ctx","name":"b"}},"r":{"k":"ctx","name":"c"}}`,
		"contains(needs.*.result, 'failure')":  `{"k":"call","fn":"contains","args":[{"k":"prop","of":{"k":"star","of":{"k":"ctx","name":"needs"}},"name":"result"},{"k":"str","s":"failure"}]}`,
		"needs['my-job'].result != null":       `{"k":"cmp","op":"ne","l":{"k":"prop","of":{"k":"index","of":{"k":"ctx","name":"needs"},"at":{"k":"str","s":"my-job"}},"name":"result"},"r":{"k":"null"}}`,
		"fromJSON('[1, 2]')[0x1] >= -2.5e-1":   `{"k":"cmp","op":"ge","l":{"k":"index","of":{"k":"call","fn":"fromjson","args":[{"k":"str","s":"[1, 2]"}]},"at":{"k":"num","n":1}},"r":{"k":"num","n":-0.25}}`,
		"format('It''s {0}', github.actor)":    `{"k":"call","fn":"format","args":[{"k":"str","s":"It's {0}"},{"k":"prop","of":{"k":"ctx","name":"github"},"name":"actor"}]}`,
		"TOJSON(Inputs.Dry_Run) == 'false'":    `{"k":"cmp","op":"eq","l":{"k":"call","fn":"tojson","args":[{"k":"prop","of":{"k":"ctx","name":"inputs"},"name":"Dry_Run"}]},"r":{"k":"str","s":"false"}}`,
		"case(github.ref == 'x', 'a', 'b')":    `{"k":"call","fn":"case","args":[{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"},"r":{"k":"str","s":"x"}},{"k":"str","s":"a"},{"k":"str","s":"b"}]}`,
		"github.event.inputs.dry_run == false": `{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"prop","of":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"event"},"name":"inputs"},"name":"dry_run"},"r":{"k":"bool","b":false}}`,
	}
	for src, want := range cases {
		// the context check is the next test's: a and b stand for any root;
		// the ${{ }} wrapper is the condition's to strip
		n, err := parseExpr(src)
		if strings.Contains(src, "${{") {
			c := CompileCondition(src, 1, 1)
			if n, err = c.Expr, nil; c.Refusal != "" {
				err = errors.New(c.Refusal)
			}
		}
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got := wire(t, n); got != want {
			t.Errorf("%q:\n got %s\nwant %s", src, got, want)
		}
	}
}

// a condition carries its text and where it was written; a refused one
// carries the concrete reason and no tree, so the ship can show it and
// never evaluate it
func TestCompileConditionWire(t *testing.T) {
	c := CompileCondition("needs.a.outputs.go == 'true' && success()", 15, 9)
	if got, want := wire(t, c), `{"v":2,"text":"needs.a.outputs.go == 'true' \u0026\u0026 success()","line":15,"col":9,"expr":{"k":"and","l":{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"prop","of":{"k":"prop","of":{"k":"ctx","name":"needs"},"name":"a"},"name":"outputs"},"name":"go"},"r":{"k":"str","s":"true"}},"r":{"k":"call","fn":"success","args":[]}}}`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	r := CompileCondition("hashFiles('**/go.sum') != ''", 3, 7)
	if r.Expr != nil || r.Refusal != "function hashFiles is not available in a job-level if" || r.Line != 3 || r.Col != 7 {
		t.Fatalf("hashFiles: %+v", r)
	}
}

// what a job-level if may not use is refused by name, never compiled to
// something that evaluates false: other contexts, the github properties
// the ship does not hold, unknown functions, wrong arity, text mixed
// with ${{ }}, syntax errors and every bound
func TestCompileConditionRefusals(t *testing.T) {
	cases := map[string]string{
		"env.CI == 'true'":                      "context env is not available in a job-level if (github, needs, vars and inputs are)",
		"steps.a.outputs.x":                     "context steps is not available in a job-level if (github, needs, vars and inputs are)",
		"secrets.TOKEN != ''":                   "context secrets is not available in a job-level if (github, needs, vars and inputs are)",
		"matrix.os == 'linux'":                  "context matrix is not available in a job-level if (github, needs, vars and inputs are)",
		"github.repository == 'o/r'":            "github.repository is not available on the ship in this release",
		"github.token != ''":                    "github.token is not available on the ship in this release",
		"toJSON(github) != ''":                  "the whole github context is not available on the ship; name one of its properties",
		"github[inputs.key] == 'x'":             "github is read by property name only on the ship",
		"frob(1)":                               "unknown function frob",
		"contains('a')":                         "contains takes 2 arguments, not 1",
		"always(1)":                             "always takes 0 arguments, not 1",
		"case(true, 'a')":                       "case takes an odd number of arguments, at least 3, not 2",
		"${{ inputs.a }} == ${{ inputs.b }}":    "the if mixes text with ${{ }}: it would be read as text, which is always true; write the condition inside one ${{ }} or none",
		"needs.a.outputs.go == \"true\"":        "syntax error at column 23: double quotes are not string delimiters; use single quotes",
		"needs.a.outputs.go ==":                 "syntax error at column 22: unexpected end of expression",
		"(always()":                             "syntax error at column 10: expected ) to close (",
		"0123 == 1":                             "syntax error at column 2: a number cannot continue with 1",
		"inputs.a = 1":                          "syntax error at column 10: = is not an operator; use ==",
		"'unterminated":                         "syntax error at column 1: unterminated string",
		"":                                      "the if is empty",
		strings.Repeat("a", 4097):               "the expression is 4097 bytes; the bound is 4096",
		strings.Repeat("!", 40) + "true":        "the expression nests 41 deep; the bound is 32",
		strings.Repeat("true && ", 130) + "true": "the expression has 261 nodes; the bound is 256",
	}
	for src, want := range cases {
		c := CompileCondition(src, 1, 1)
		if c.Expr != nil || c.Refusal != want {
			t.Errorf("%q:\n got %q (expr %v)\nwant %q", trimmed(src), c.Refusal, c.Expr != nil, want)
		}
	}
	// the refused text is kept for diagnosis, bounded
	long := CompileCondition(strings.Repeat("b", 5000), 1, 1)
	if len(long.Text) != 512 {
		t.Errorf("an oversized text is kept to 512 bytes, not %d", len(long.Text))
	}
}

func trimmed(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

// a declared job output is a template: literal text and ${{ }} parts, each
// expression compiled for the job-output contexts, steps.<id>.outputs among
// them; what the ship cannot resolve is refused by name
func TestCompileOutputTemplates(t *testing.T) {
	o := CompileOutput("value", "${{ steps.emit.outputs.raw }}", 7, 14)
	if got, want := wire(t, o), `{"name":"value","v":2,"text":"${{ steps.emit.outputs.raw }}","line":7,"col":14,"parts":[{"expr":{"k":"prop","of":{"k":"prop","of":{"k":"prop","of":{"k":"ctx","name":"steps"},"name":"emit"},"name":"outputs"},"name":"raw"}}]}`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	mixed := CompileOutput("label", "v=${{ steps.a.outputs.x }}; ok", 1, 1)
	if got, want := wire(t, mixed.Parts), `[{"lit":"v="},{"expr":{"k":"prop","of":{"k":"prop","of":{"k":"prop","of":{"k":"ctx","name":"steps"},"name":"a"},"name":"outputs"},"name":"x"}},{"lit":"; ok"}]`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	plain := CompileOutput("fixed", "no expression", 1, 1)
	if got := wire(t, plain.Parts); got != `[{"lit":"no expression"}]` {
		t.Fatalf("plain: %s", got)
	}
	quoted := CompileOutput("q", "${{ format('}}{0}', 'x') }}", 1, 1)
	if quoted.Refusal != "" || len(quoted.Parts) != 1 || quoted.Parts[0].Expr == nil {
		t.Fatalf("a }} inside a string literal does not end the expression: %+v", quoted)
	}
	refused := map[string]string{
		"${{ env.URL }}":               "context env is not available to a job output on the ship in this release (github, needs, vars, inputs and steps.<id>.outputs are)",
		"${{ steps.a.conclusion }}":    "steps.a.conclusion is not available on the ship; only steps.<id>.outputs",
		"${{ steps.*.outputs.x }}":     "steps is read as steps.<id>.outputs on the ship",
		"${{ success() }}":             "function success is only valid in an if",
		"${{ hashFiles('x') }}":        "function hashFiles is not available to a job output on the ship",
		"${{ steps.a.outputs.x":        "an ${{ is not closed by }}",
		"${{ steps.a.outputs.x }} }}x": "",
	}
	for text, want := range refused {
		got := CompileOutput("o", text, 1, 1)
		if got.Refusal != want {
			t.Errorf("%q: got %q, want %q", text, got.Refusal, want)
		}
		if want != "" && got.Parts != nil {
			t.Errorf("%q: a refused output carries no parts", text)
		}
	}
	if bad := CompileOutput("2bad", "x", 1, 1); bad.Refusal != "output name 2bad starts with a letter or _ and holds only letters, digits, - and _" {
		t.Errorf("name: %q", bad.Refusal)
	}
}

// the projection's adapter: every needs context root in an expression is
// read from the signed carrier instead, as data; property names, string
// literals, function names and the other contexts are left exactly as
// written
func TestRewriteNeedsExpression(t *testing.T) {
	cases := map[string]string{
		"needs.producer.outputs.value":                        "fromJSON(vars.URGIT_NEEDS).producer.outputs.value",
		"NEEDS.Producer.result == 'success'":                  "fromJSON(vars.URGIT_NEEDS).Producer.result == 'success'",
		"toJSON(needs)":                                       "toJSON(fromJSON(vars.URGIT_NEEDS))",
		"contains(needs.*.result, 'needs.x')":                 "contains(fromJSON(vars.URGIT_NEEDS).*.result, 'needs.x')",
		"github.event.needs || steps.needs.outputs.needs":     "github.event.needs || steps.needs.outputs.needs",
		"needs['a-b'].outputs['needs']":                       "fromJSON(vars.URGIT_NEEDS)['a-b'].outputs['needs']",
		"format('{0}', needs.a.outputs.x) == 'It''s needs'":   "format('{0}', fromJSON(vars.URGIT_NEEDS).a.outputs.x) == 'It''s needs'",
		"!needs.a.outputs.ok":                                 "!fromJSON(vars.URGIT_NEEDS).a.outputs.ok",
		"inputs.x":                                            "inputs.x",
	}
	for src, want := range cases {
		got, n, err := RewriteNeedsExpression(src)
		if err != nil || got != want {
			t.Errorf("%q: got %q (%v), want %q", src, got, err, want)
		}
		if (n > 0) != (got != src) {
			t.Errorf("%q: %d rewrite(s) reported for %q", src, n, got)
		}
	}
	tpl, n, err := RewriteNeedsTemplate("echo '${{ needs.a.outputs.v }}' and ${{ format('}}', needs.b.result) }} done")
	if err != nil || n != 2 || tpl != "echo '${{ fromJSON(vars.URGIT_NEEDS).a.outputs.v }}' and ${{ format('}}', fromJSON(vars.URGIT_NEEDS).b.result) }} done" {
		t.Fatalf("template: %q %d %v", tpl, n, err)
	}
	if _, _, err := RewriteNeedsTemplate("${{ needs.a.outputs.v "); err == nil {
		t.Fatal("an unclosed ${{ that reads needs cannot be carried and is refused")
	}
	if same, n, err := RewriteNeedsTemplate("${{ inputs.a "); err != nil || n != 0 || same != "${{ inputs.a " {
		t.Fatalf("an unclosed ${{ that does not read needs is left for act: %q %d %v", same, n, err)
	}
}
