package plan

// The job-expression compiler (CI-EXPR-2, R2.1-A/R2.2-A): a job-level `if`
// and a job's declared `outputs` compile here, outside the ship, to a
// bounded, versioned tree the ship validates and evaluates; the ship never
// parses expression text. The grammar is the one act's own parser
// (actionlint 1.7.7) lexes, with GitHub's documented precedence: `!`, then
// < <= > >=, then == !=, then &&, then ||, each left-associative. What the
// ship cannot evaluate faithfully is refused here by name, never compiled
// to something that reads false.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ExprVersion is the wire version of a compiled expression (CI-EXPR-2).
const ExprVersion = 2

// the bounds both the compiler and the ship enforce
const (
	MaxExprBytes  = 4096 // the source text of one if or one output
	MaxExprNodes  = 256  // the nodes of one if, or of every part of one output
	MaxExprDepth  = 32   // the longest root-to-leaf path of one expression
	MaxJobOutputs = 64   // the declared outputs of one job
	shownBytes    = 512  // what a refused, oversized text keeps for diagnosis
)

// Node is one node of a compiled expression. K names the kind; the other
// fields are the kind's own (see MarshalJSON for the wire of each).
type Node struct {
	K    string
	B    bool
	N    float64
	S    string // a string literal, a context, property or function name, a comparison
	Of   *Node  // the receiver of prop/index/star, the operand of not
	At   *Node  // the index of index
	L, R *Node  // cmp, and, or
	Args []*Node
}

// MarshalJSON writes the one wire shape of each kind, keys in order.
func (n *Node) MarshalJSON() ([]byte, error) {
	switch n.K {
	case "null":
		return json.Marshal(struct {
			K string `json:"k"`
		}{n.K})
	case "bool":
		return json.Marshal(struct {
			K string `json:"k"`
			B bool   `json:"b"`
		}{n.K, n.B})
	case "num":
		return json.Marshal(struct {
			K string  `json:"k"`
			N float64 `json:"n"`
		}{n.K, n.N})
	case "str":
		return json.Marshal(struct {
			K string `json:"k"`
			S string `json:"s"`
		}{n.K, n.S})
	case "ctx":
		return json.Marshal(struct {
			K    string `json:"k"`
			Name string `json:"name"`
		}{n.K, n.S})
	case "prop":
		return json.Marshal(struct {
			K    string `json:"k"`
			Of   *Node  `json:"of"`
			Name string `json:"name"`
		}{n.K, n.Of, n.S})
	case "index":
		return json.Marshal(struct {
			K  string `json:"k"`
			Of *Node  `json:"of"`
			At *Node  `json:"at"`
		}{n.K, n.Of, n.At})
	case "star":
		return json.Marshal(struct {
			K  string `json:"k"`
			Of *Node  `json:"of"`
		}{n.K, n.Of})
	case "not":
		return json.Marshal(struct {
			K string `json:"k"`
			A *Node  `json:"a"`
		}{n.K, n.Of})
	case "cmp":
		return json.Marshal(struct {
			K  string `json:"k"`
			Op string `json:"op"`
			L  *Node  `json:"l"`
			R  *Node  `json:"r"`
		}{n.K, n.S, n.L, n.R})
	case "and", "or":
		return json.Marshal(struct {
			K string `json:"k"`
			L *Node  `json:"l"`
			R *Node  `json:"r"`
		}{n.K, n.L, n.R})
	case "call":
		args := n.Args
		if args == nil {
			args = []*Node{}
		}
		return json.Marshal(struct {
			K    string  `json:"k"`
			Fn   string  `json:"fn"`
			Args []*Node `json:"args"`
		}{n.K, n.S, args})
	}
	return nil, fmt.Errorf("unknown expression node %q", n.K)
}

// Condition is a compiled job-level `if`: its text and where the workflow
// wrote it (for diagnosis, never execution), and either the tree or the
// reason it is refused.
type Condition struct {
	V       int    `json:"v"`
	Text    string `json:"text"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Expr    *Node  `json:"expr,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

// Part is one part of a template: literal text, or an expression whose
// value is interpolated as text.
type Part struct {
	Lit  *string `json:"lit,omitempty"`
	Expr *Node   `json:"expr,omitempty"`
}

// Output is one declared `jobs.<id>.outputs.<name>`, compiled: the ship
// resolves it from the accepted attempt at the end of the job.
type Output struct {
	Name    string `json:"name"`
	V       int    `json:"v"`
	Text    string `json:"text"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Parts   []Part `json:"parts,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

// the field an expression is written in decides what it may use: a
// job-level if, a job's declared output, or a workflow's concurrency group
// and cancel-in-progress (CI-CONCURRENCY-1), which GitHub evaluates with
// github, inputs and vars alone
type field int

const (
	fieldIf field = iota
	fieldOutput
	fieldConcurrency
)

// the github properties the ship holds for a run, as act and GitHub name
// them; anything else is refused rather than read as null
var githubKeys = map[string]bool{
	"event_name": true, "event": true, "ref": true, "ref_name": true, "ref_type": true, "sha": true,
	"actor": true, "triggering_actor": true, "workflow": true, "job": true, "base_ref": true, "head_ref": true,
}

// the contexts GitHub defines, and the functions with their display names
var knownContexts = map[string]bool{
	"github": true, "env": true, "vars": true, "job": true, "jobs": true, "steps": true, "runner": true,
	"secrets": true, "strategy": true, "matrix": true, "needs": true, "inputs": true,
}

var functionNames = map[string]string{
	"contains": "contains", "startswith": "startsWith", "endswith": "endsWith", "format": "format",
	"join": "join", "tojson": "toJSON", "fromjson": "fromJSON", "hashfiles": "hashFiles", "case": "case",
	"success": "success", "failure": "failure", "always": "always", "cancelled": "cancelled",
}

var statusFunctions = map[string]bool{"success": true, "failure": true, "always": true, "cancelled": true}

// CompileCondition compiles a job-level `if` written at line:col.
func CompileCondition(text string, line, col int) Condition {
	c := Condition{V: ExprVersion, Text: text, Line: line, Col: col}
	refuse := func(reason string) Condition {
		if len(c.Text) > MaxExprBytes {
			c.Text = c.Text[:shownBytes]
		}
		c.Refusal = reason
		return c
	}
	if strings.TrimSpace(text) == "" {
		return refuse("the if is empty")
	}
	if len(text) > MaxExprBytes {
		return refuse(fmt.Sprintf("the expression is %d bytes; the bound is %d", len(text), MaxExprBytes))
	}
	src, from, to := text, 0, len(text)
	if strings.Contains(text, "${{") {
		segs, err := splitTemplate(text)
		if err != nil {
			return refuse(err.Error())
		}
		var exprs []segment
		for _, s := range segs {
			if s.expr {
				exprs = append(exprs, s)
			} else if strings.TrimSpace(s.text) != "" {
				exprs = nil
				break
			}
		}
		if len(exprs) != 1 {
			return refuse("the if mixes text with ${{ }}: it would be read as text, which is always true; write the condition inside one ${{ }} or none")
		}
		from, to = exprs[0].from, exprs[0].to
	}
	n, err := parseRange(src, from, to)
	if err != nil {
		return refuse(err.Error())
	}
	if reason := bounds([]*Node{n}); reason != "" {
		return refuse(reason)
	}
	if reason := check(n, fieldIf); reason != "" {
		return refuse(reason)
	}
	c.Expr = n
	return c
}

// CompileOutput compiles one declared job output, its template text
// written at line:col.
func CompileOutput(name, text string, line, col int) Output {
	o := Output{Name: name, V: ExprVersion, Text: text, Line: line, Col: col}
	refuse := func(reason string) Output {
		if len(o.Text) > MaxExprBytes {
			o.Text = o.Text[:shownBytes]
		}
		o.Parts, o.Refusal = nil, reason
		return o
	}
	if !outputNameValid(name) {
		return refuse("output name " + name + " starts with a letter or _ and holds only letters, digits, - and _")
	}
	if len(text) > MaxExprBytes {
		return refuse(fmt.Sprintf("the output is %d bytes; the bound is %d", len(text), MaxExprBytes))
	}
	segs, err := splitTemplate(text)
	if err != nil {
		return refuse(err.Error())
	}
	var nodes []*Node
	for _, s := range segs {
		if !s.expr {
			lit := s.text
			o.Parts = append(o.Parts, Part{Lit: &lit})
			continue
		}
		n, err := parseRange(text, s.from, s.to)
		if err != nil {
			return refuse(err.Error())
		}
		if reason := check(n, fieldOutput); reason != "" {
			return refuse(reason)
		}
		nodes = append(nodes, n)
		o.Parts = append(o.Parts, Part{Expr: n})
	}
	if reason := bounds(nodes); reason != "" {
		return refuse(reason)
	}
	if o.Parts == nil {
		o.Parts = []Part{}
	}
	return o
}

func outputNameValid(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isAlpha(c) || c == '_' || (i > 0 && (isDigit(c) || c == '-')) {
			continue
		}
		return false
	}
	return true
}

// bounds: the node count over every tree, then each tree's depth
func bounds(nodes []*Node) string {
	total := 0
	for _, n := range nodes {
		total += count(n)
	}
	if total > MaxExprNodes {
		return fmt.Sprintf("the expression has %d nodes; the bound is %d", total, MaxExprNodes)
	}
	for _, n := range nodes {
		if d := depth(n); d > MaxExprDepth {
			return fmt.Sprintf("the expression nests %d deep; the bound is %d", d, MaxExprDepth)
		}
	}
	return ""
}

func children(n *Node) []*Node {
	var out []*Node
	for _, c := range []*Node{n.Of, n.At, n.L, n.R} {
		if c != nil {
			out = append(out, c)
		}
	}
	return append(out, n.Args...)
}

func count(n *Node) int {
	total := 1
	for _, c := range children(n) {
		total += count(c)
	}
	return total
}

func depth(n *Node) int {
	deepest := 0
	for _, c := range children(n) {
		if d := depth(c); d > deepest {
			deepest = d
		}
	}
	return deepest + 1
}

// accessor is one step of a context access chain: a property name (or a
// string-literal index), a dynamic index, or the * filter
type accessor struct {
	name    string
	literal bool
	star    bool
}

// check walks the tree in source order and names the first construct the
// field may not use
func check(n *Node, f field) string {
	switch n.K {
	case "prop", "index", "star":
		root, chain := accessChain(n)
		if root.K == "ctx" {
			if reason := checkContext(root.S, chain, f); reason != "" {
				return reason
			}
			// the dynamic indexes of the chain are expressions of their own
			for c := n; c != root; c = c.Of {
				if c.K == "index" && !(c.At.K == "str") {
					if reason := check(c.At, f); reason != "" {
						return reason
					}
				}
			}
			return ""
		}
		if reason := check(root, f); reason != "" {
			return reason
		}
		for c := n; c != root; c = c.Of {
			if c.K == "index" {
				if reason := check(c.At, f); reason != "" {
					return reason
				}
			}
		}
		return ""
	case "ctx":
		return checkContext(n.S, nil, f)
	case "call":
		if reason := checkCall(n, f); reason != "" {
			return reason
		}
	}
	for _, c := range children(n) {
		if reason := check(c, f); reason != "" {
			return reason
		}
	}
	return ""
}

// accessChain splits a prop/index/star node into its root and the
// accessors applied to it, outermost last
func accessChain(n *Node) (*Node, []accessor) {
	var chain []accessor
	for n.K == "prop" || n.K == "index" || n.K == "star" {
		var a accessor
		switch n.K {
		case "prop":
			a = accessor{name: n.S, literal: true}
		case "index":
			if n.At.K == "str" {
				a = accessor{name: n.At.S, literal: true}
			}
		case "star":
			a = accessor{star: true}
		}
		chain = append([]accessor{a}, chain...)
		n = n.Of
	}
	return n, chain
}

func checkContext(name string, chain []accessor, f field) string {
	allowed := map[string]bool{"github": true, "needs": true, "vars": true, "inputs": true}
	if f == fieldOutput {
		allowed["steps"] = true
	}
	if f == fieldConcurrency {
		allowed = map[string]bool{"github": true, "vars": true, "inputs": true}
	}
	if !allowed[name] {
		if !knownContexts[name] {
			return "unknown context " + name
		}
		if f == fieldIf {
			return "context " + name + " is not available in a job-level if (github, needs, vars and inputs are)"
		}
		if f == fieldConcurrency {
			return "context " + name + " is not available to a workflow's concurrency (github, inputs and vars are)"
		}
		return "context " + name + " is not available to a job output on the ship in this release (github, needs, vars, inputs and steps.<id>.outputs are)"
	}
	switch name {
	case "github":
		if len(chain) == 0 {
			return "the whole github context is not available on the ship; name one of its properties"
		}
		if !chain[0].literal {
			return "github is read by property name only on the ship"
		}
		if key := strings.ToLower(chain[0].name); !githubKeys[key] {
			return "github." + key + " is not available on the ship in this release"
		}
		if f == fieldConcurrency && strings.EqualFold(chain[0].name, "job") {
			return "github.job is not defined for a workflow's concurrency: it is set only inside a job"
		}
	case "steps":
		if len(chain) < 2 || !chain[0].literal || !chain[1].literal {
			return "steps is read as steps.<id>.outputs on the ship"
		}
		if !strings.EqualFold(chain[1].name, "outputs") {
			return "steps." + chain[0].name + "." + strings.ToLower(chain[1].name) + " is not available on the ship; only steps.<id>.outputs"
		}
	}
	return ""
}

func checkCall(n *Node, f field) string {
	display, known := functionNames[n.S]
	if !known {
		return "unknown function " + n.S
	}
	if statusFunctions[n.S] && f != fieldIf {
		return "function " + display + " is only valid in an if"
	}
	if n.S == "hashfiles" {
		if f == fieldIf {
			return "function hashFiles is not available in a job-level if"
		}
		if f == fieldConcurrency {
			return "function hashFiles is not available to a workflow's concurrency"
		}
		return "function hashFiles is not available to a job output on the ship"
	}
	got := len(n.Args)
	switch n.S {
	case "contains", "startswith", "endswith":
		if got != 2 {
			return fmt.Sprintf("%s takes 2 arguments, not %d", display, got)
		}
	case "tojson", "fromjson":
		if got != 1 {
			return fmt.Sprintf("%s takes 1 argument, not %d", display, got)
		}
	case "success", "failure", "always", "cancelled":
		if got != 0 {
			return fmt.Sprintf("%s takes 0 arguments, not %d", display, got)
		}
	case "format":
		if got < 1 {
			return "format takes at least 1 argument, not 0"
		}
	case "join":
		if got < 1 || got > 2 {
			return fmt.Sprintf("join takes 1 or 2 arguments, not %d", got)
		}
	case "case":
		if got < 3 || got%2 == 0 {
			return fmt.Sprintf("case takes an odd number of arguments, at least 3, not %d", got)
		}
	}
	return ""
}

// --- templates --------------------------------------------------------

// segment is literal text or the source range of one ${{ }} expression
type segment struct {
	expr     bool
	text     string // the literal
	from, to int    // the expression's range in the text
}

// splitTemplate splits text at its ${{ }} expressions. An expression ends
// at the first }} that is not inside a string literal, as act's own
// interpolation reads it.
func splitTemplate(text string) ([]segment, error) {
	var out []segment
	pos := 0
	for {
		at := strings.Index(text[pos:], "${{")
		if at < 0 {
			if pos < len(text) {
				out = append(out, segment{text: text[pos:]})
			}
			return out, nil
		}
		if at > 0 {
			out = append(out, segment{text: text[pos : pos+at]})
		}
		from := pos + at + 3
		l := lexer{src: text, pos: from}
		for {
			t, err := l.next()
			if err != nil {
				return nil, err
			}
			if t.kind == tEOF {
				return nil, errors.New("an ${{ is not closed by }}")
			}
			if t.kind == tClose {
				out = append(out, segment{expr: true, from: from, to: t.start})
				pos = t.end
				break
			}
		}
	}
}

// --- the needs adapter -------------------------------------------------

// NeedsCarrier is the act variable the single-job projection reads the
// ship's signed needs context from (see RewriteNeedsExpression).
const NeedsCarrier = "URGIT_NEEDS"

const needsReplacement = "fromJSON(vars." + NeedsCarrier + ")"

// RewriteNeedsExpression rewrites every `needs` context root in one
// expression to read the signed carrier as data: act's planner gives a
// projected job no needs context of its own (pkg/runner/expression.go
// builds it only from upstream jobs in the same run), so the projection
// reads it from `vars`, which a step cannot change and which the CLI sets
// ahead of any repository .vars file. Property names, string literals,
// function names and every other context are kept exactly as written.
func RewriteNeedsExpression(src string) (string, int, error) {
	return rewriteRange(src, 0, len(src))
}

// RewriteNeedsTemplate rewrites the needs roots in every ${{ }} of a
// template. An expression that cannot be read is left for act as written,
// unless it reads needs: then the job cannot be given its context, and
// the projection is refused.
func RewriteNeedsTemplate(text string) (string, int, error) {
	var out strings.Builder
	total, pos := 0, 0
	for {
		at := strings.Index(text[pos:], "${{")
		if at < 0 {
			out.WriteString(text[pos:])
			return out.String(), total, nil
		}
		from := pos + at + 3
		out.WriteString(text[pos:from])
		l := lexer{src: text, pos: from}
		to := -1
		next := 0
		for {
			t, err := l.next()
			if err != nil || t.kind == tEOF {
				break
			}
			if t.kind == tClose {
				to, next = t.start, t.end
				break
			}
		}
		if to < 0 {
			if strings.Contains(strings.ToLower(text[from:]), "needs") {
				return "", 0, fmt.Errorf("the expression at byte %d reads needs and cannot be read: its needs context cannot be carried", from)
			}
			out.WriteString(text[from:])
			return out.String(), total, nil
		}
		rewritten, n, err := rewriteRange(text, from, to)
		if err != nil {
			return "", 0, err
		}
		out.WriteString(rewritten)
		out.WriteString(text[to:next])
		total += n
		pos = next
	}
}

// rewriteRange rewrites the needs roots of the expression text[from:to]
func rewriteRange(text string, from, to int) (string, int, error) {
	l := lexer{src: text[:to], pos: from}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			if strings.Contains(strings.ToLower(text[from:to]), "needs") {
				return "", 0, err
			}
			return text[from:to], 0, nil
		}
		if t.kind == tEOF {
			break
		}
		toks = append(toks, t)
	}
	var out strings.Builder
	at, n := from, 0
	for i, t := range toks {
		if t.kind != tIdent || !strings.EqualFold(t.text, "needs") {
			continue
		}
		if i > 0 && toks[i-1].kind == tDot {
			continue
		}
		if i+1 < len(toks) && toks[i+1].kind == tLParen {
			continue
		}
		out.WriteString(text[at:t.start])
		out.WriteString(needsReplacement)
		at, n = t.end, n+1
	}
	out.WriteString(text[at:to])
	return out.String(), n, nil
}

// --- lexer ---------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tClose
	tIdent
	tNumber
	tString
	tLParen
	tRParen
	tLBracket
	tRBracket
	tDot
	tStar
	tComma
	tNot
	tLt
	tLe
	tGt
	tGe
	tEq
	tNe
	tAnd
	tOr
)

type token struct {
	kind       tokKind
	text       string
	start, end int
}

// syntaxError is a refusal at a byte offset of the text being compiled
type syntaxError struct {
	at  int
	msg string
}

func (e *syntaxError) Error() string {
	return fmt.Sprintf("syntax error at column %d: %s", e.at+1, e.msg)
}

type lexer struct {
	src string
	pos int
}

func isAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isDigit(c byte) bool { return '0' <= c && c <= '9' }
func isHex(c byte) bool   { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }

func (l *lexer) peekAt(i int) byte {
	if i < len(l.src) {
		return l.src[i]
	}
	return 0
}

func (l *lexer) tok(kind tokKind, start int) (token, error) {
	return token{kind: kind, text: l.src[start:l.pos], start: start, end: l.pos}, nil
}

func (l *lexer) fail(at int, format string, args ...any) (token, error) {
	return token{}, &syntaxError{at: at, msg: fmt.Sprintf(format, args...)}
}

func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\n', '\r':
			l.pos++
			continue
		}
		break
	}
	start := l.pos
	if l.pos >= len(l.src) {
		return l.tok(tEOF, start)
	}
	c := l.src[l.pos]
	if isAlpha(c) || c == '_' {
		for l.pos++; l.pos < len(l.src); l.pos++ {
			d := l.src[l.pos]
			if !(isAlpha(d) || isDigit(d) || d == '_' || d == '-') {
				break
			}
		}
		return l.tok(tIdent, start)
	}
	if isDigit(c) || c == '-' {
		return l.number(start)
	}
	two := func(second byte, both, one tokKind) (token, error) {
		if l.peekAt(l.pos+1) == second {
			l.pos += 2
			return l.tok(both, start)
		}
		l.pos++
		return l.tok(one, start)
	}
	switch c {
	case '\'':
		for l.pos++; ; l.pos++ {
			if l.pos >= len(l.src) {
				return l.fail(start, "unterminated string")
			}
			if l.src[l.pos] == '\'' {
				if l.peekAt(l.pos+1) == '\'' {
					l.pos++
					continue
				}
				l.pos++
				return l.tok(tString, start)
			}
		}
	case '"':
		return l.fail(start, "double quotes are not string delimiters; use single quotes")
	case '}':
		if l.peekAt(l.pos+1) == '}' {
			l.pos += 2
			return l.tok(tClose, start)
		}
		return l.fail(start, "a single } is not an operator")
	case '!':
		return two('=', tNe, tNot)
	case '<':
		return two('=', tLe, tLt)
	case '>':
		return two('=', tGe, tGt)
	case '=':
		if l.peekAt(l.pos+1) == '=' {
			l.pos += 2
			return l.tok(tEq, start)
		}
		return l.fail(start, "= is not an operator; use ==")
	case '&':
		if l.peekAt(l.pos+1) == '&' {
			l.pos += 2
			return l.tok(tAnd, start)
		}
		return l.fail(start, "& is not an operator; use &&")
	case '|':
		if l.peekAt(l.pos+1) == '|' {
			l.pos += 2
			return l.tok(tOr, start)
		}
		return l.fail(start, "| is not an operator; use ||")
	}
	singles := map[byte]tokKind{'(': tLParen, ')': tRParen, '[': tLBracket, ']': tRBracket, '.': tDot, '*': tStar, ',': tComma}
	if k, ok := singles[c]; ok {
		l.pos++
		return l.tok(k, start)
	}
	return l.fail(start, "unexpected character %q", c)
}

// number lexes as actionlint does: an optional -, 0 or a digit run without
// a leading zero (0x and hex digits for a hex integer), an optional
// fraction and an optional exponent with an optional -; nothing
// alphanumeric may follow
func (l *lexer) number(start int) (token, error) {
	if l.src[l.pos] == '-' {
		l.pos++
		if !isDigit(l.peekAt(l.pos)) {
			return l.fail(l.pos, "a - must begin a number")
		}
	}
	if l.src[l.pos] == '0' {
		l.pos++
		if l.peekAt(l.pos) == 'x' {
			l.pos++
			if !isHex(l.peekAt(l.pos)) {
				return l.fail(l.pos, "a hex number needs digits after 0x")
			}
			for isHex(l.peekAt(l.pos)) {
				l.pos++
			}
			return l.numberEnd(start)
		}
	} else {
		for isDigit(l.peekAt(l.pos)) {
			l.pos++
		}
	}
	if l.peekAt(l.pos) == '.' {
		l.pos++
		if !isDigit(l.peekAt(l.pos)) {
			return l.fail(l.pos, "a number's fraction needs digits after .")
		}
		for isDigit(l.peekAt(l.pos)) {
			l.pos++
		}
	}
	if e := l.peekAt(l.pos); e == 'e' || e == 'E' {
		l.pos++
		if l.peekAt(l.pos) == '-' {
			l.pos++
		}
		if !isDigit(l.peekAt(l.pos)) {
			return l.fail(l.pos, "a number's exponent needs digits")
		}
		for isDigit(l.peekAt(l.pos)) {
			l.pos++
		}
	}
	return l.numberEnd(start)
}

func (l *lexer) numberEnd(start int) (token, error) {
	if c := l.peekAt(l.pos); isAlpha(c) || isDigit(c) {
		return l.fail(l.pos, "a number cannot continue with %c", c)
	}
	return l.tok(tNumber, start)
}

// --- parser --------------------------------------------------------------

type parser struct {
	toks []token
	i    int
	end  int // the byte offset where the expression ends
}

func (p *parser) peek() token {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return token{kind: tEOF, start: p.end, end: p.end}
}

func (p *parser) advance() token {
	t := p.peek()
	if p.i < len(p.toks) {
		p.i++
	}
	return t
}

func (p *parser) fail(t token, format string, args ...any) (*Node, error) {
	return nil, &syntaxError{at: t.start, msg: fmt.Sprintf(format, args...)}
}

// parseExpr parses a whole expression: the parser's own tests use it
func parseExpr(src string) (*Node, error) {
	return parseRange(src, 0, len(src))
}

// parseRange parses the expression text[from:to]; error columns count
// from the start of text
func parseRange(text string, from, to int) (*Node, error) {
	l := lexer{src: text[:to], pos: from}
	p := &parser{end: to}
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		if t.kind == tEOF {
			break
		}
		if t.kind == tClose {
			return p.fail(t, "unexpected }}")
		}
		p.toks = append(p.toks, t)
	}
	if len(p.toks) == 0 {
		return p.fail(p.peek(), "unexpected end of expression")
	}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return p.fail(t, "unexpected %s", t.text)
	}
	return n, nil
}

func (p *parser) parseOr() (*Node, error) {
	l, err := p.parseAnd()
	for err == nil && p.peek().kind == tOr {
		p.advance()
		var r *Node
		if r, err = p.parseAnd(); err == nil {
			l = &Node{K: "or", L: l, R: r}
		}
	}
	return l, err
}

func (p *parser) parseAnd() (*Node, error) {
	l, err := p.parseEquality()
	for err == nil && p.peek().kind == tAnd {
		p.advance()
		var r *Node
		if r, err = p.parseEquality(); err == nil {
			l = &Node{K: "and", L: l, R: r}
		}
	}
	return l, err
}

var comparisons = map[tokKind]string{tEq: "eq", tNe: "ne", tLt: "lt", tLe: "le", tGt: "gt", tGe: "ge"}

func (p *parser) parseEquality() (*Node, error) {
	l, err := p.parseRelational()
	for err == nil && (p.peek().kind == tEq || p.peek().kind == tNe) {
		op := comparisons[p.advance().kind]
		var r *Node
		if r, err = p.parseRelational(); err == nil {
			l = &Node{K: "cmp", S: op, L: l, R: r}
		}
	}
	return l, err
}

func (p *parser) parseRelational() (*Node, error) {
	l, err := p.parseUnary()
	for err == nil {
		k := p.peek().kind
		if k != tLt && k != tLe && k != tGt && k != tGe {
			break
		}
		op := comparisons[p.advance().kind]
		var r *Node
		if r, err = p.parseUnary(); err == nil {
			l = &Node{K: "cmp", S: op, L: l, R: r}
		}
	}
	return l, err
}

func (p *parser) parseUnary() (*Node, error) {
	if p.peek().kind == tNot {
		p.advance()
		a, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Node{K: "not", Of: a}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (*Node, error) {
	n, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case tDot:
			p.advance()
			t := p.peek()
			switch t.kind {
			case tStar:
				p.advance()
				n = &Node{K: "star", Of: n}
			case tIdent:
				p.advance()
				n = &Node{K: "prop", Of: n, S: t.text}
			default:
				return p.fail(t, "expected a property name or * after .")
			}
		case tLBracket:
			p.advance()
			at, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			if t := p.peek(); t.kind != tRBracket {
				return p.fail(t, "expected ] to close [")
			}
			p.advance()
			n = &Node{K: "index", Of: n, At: at}
		default:
			return n, nil
		}
	}
}

func (p *parser) parsePrimary() (*Node, error) {
	t := p.peek()
	switch t.kind {
	case tIdent:
		p.advance()
		if p.peek().kind == tLParen {
			p.advance()
			call := &Node{K: "call", S: strings.ToLower(t.text), Args: []*Node{}}
			if p.peek().kind == tRParen {
				p.advance()
				return call, nil
			}
			for {
				arg, err := p.parseOr()
				if err != nil {
					return nil, err
				}
				call.Args = append(call.Args, arg)
				switch next := p.peek(); next.kind {
				case tComma:
					p.advance()
				case tRParen:
					p.advance()
					return call, nil
				default:
					return p.fail(next, "expected , or ) in the arguments of %s", t.text)
				}
			}
		}
		switch t.text {
		case "true", "false":
			return &Node{K: "bool", B: t.text == "true"}, nil
		case "null":
			return &Node{K: "null"}, nil
		}
		return &Node{K: "ctx", S: strings.ToLower(t.text)}, nil
	case tLParen:
		p.advance()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if c := p.peek(); c.kind != tRParen {
			return p.fail(c, "expected ) to close (")
		}
		p.advance()
		return n, nil
	case tNumber:
		p.advance()
		v, err := numberValue(t.text)
		if err != nil {
			return p.fail(t, "%v", err)
		}
		return &Node{K: "num", N: v}, nil
	case tString:
		p.advance()
		s := t.text[1 : len(t.text)-1]
		return &Node{K: "str", S: strings.ReplaceAll(s, "''", "'")}, nil
	case tEOF:
		return p.fail(t, "unexpected end of expression")
	}
	return p.fail(t, "unexpected %s", t.text)
}

// numberValue: a hex integer, or a decimal one, as a double; out of range
// is refused, never read as infinity
func numberValue(text string) (float64, error) {
	neg := strings.HasPrefix(text, "-")
	body := strings.TrimPrefix(text, "-")
	if strings.HasPrefix(body, "0x") {
		u, err := strconv.ParseUint(body[2:], 16, 64)
		if err != nil {
			return 0, fmt.Errorf("number %s is out of range", text)
		}
		v := float64(u)
		if neg {
			v = -v
		}
		return v, nil
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(v, 0) {
		return 0, fmt.Errorf("number %s is out of range", text)
	}
	return v, nil
}
