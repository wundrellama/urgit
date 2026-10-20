package plan

// The concurrency compiler (CI-CONCURRENCY-1): a workflow's top-level
// `concurrency:` compiles here, beside its `on:`, into the trigger catalog
// the resolver posts with the lock. The ship evaluates the group and
// cancel-in-progress when a run joins its group (GitHub evaluates them
// with the github, inputs and vars contexts) and never reads the YAML.
// What the ship cannot evaluate faithfully is refused here by name, and
// nothing is ever read as "no group".

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Concurrency is a workflow's compiled concurrency: "none" when it
// declares none, "group" with its group and cancel-in-progress, or
// "refused" with the reason and where the workflow wrote it. The
// compiler always sends it, so a catalog without it is one an older
// compiler read: unknown on the ship, never "no group".
type Concurrency struct {
	V       int       `json:"v"`
	Kind    string    `json:"kind"`
	Group   *Template `json:"group,omitempty"`
	Cancel  *Cancel   `json:"cancel,omitempty"`
	Text    string    `json:"text,omitempty"`
	Line    int       `json:"line,omitempty"`
	Col     int       `json:"col,omitempty"`
	Refusal string    `json:"refusal,omitempty"`
}

// Template is a compiled text template: literal text and ${{ }}
// expressions interpolated as text, with the text and place the workflow
// wrote it at.
type Template struct {
	Text  string `json:"text"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Parts []Part `json:"parts"`
}

// Cancel is cancel-in-progress: a fixed value (false when the workflow
// writes none), or one compiled expression the ship evaluates to true or
// false when the run joins.
type Cancel struct {
	Text  string `json:"text"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Value *bool  `json:"value,omitempty"`
	Expr  *Node  `json:"expr,omitempty"`
}

const concurrencyForms = "a group (a string), or a mapping with group and cancel-in-progress"

// ConcurrencyOf compiles the concurrency: of a workflow's top-level
// mapping; a workflow that cannot be read (nil) has a refused one.
func ConcurrencyOf(top *yaml.Node) *Concurrency {
	if top == nil {
		return &Concurrency{V: ExprVersion, Kind: "refused", Refusal: "the workflow cannot be read, so neither can its concurrency"}
	}
	var key, node *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "concurrency" {
			key, node = top.Content[i], top.Content[i+1]
		}
	}
	if key == nil {
		return &Concurrency{V: ExprVersion, Kind: "none"}
	}
	refuse := func(at *yaml.Node, text, reason string) *Concurrency {
		if len(text) > MaxExprBytes {
			text = text[:shownBytes]
		}
		return &Concurrency{V: ExprVersion, Kind: "refused", Text: text, Line: at.Line, Col: at.Column, Refusal: reason}
	}
	node = resolved(node)
	switch node.Kind {
	case yaml.ScalarNode:
		if isNull(node) {
			return refuse(key, "", "concurrency is empty: write "+concurrencyForms)
		}
		if node.ShortTag() != "!!str" {
			return refuse(node, node.Value, fmt.Sprintf("concurrency %s is not text: write %s", node.Value, concurrencyForms))
		}
		group, why := compileGroup(node)
		if why != "" {
			return refuse(node, node.Value, why)
		}
		fixed := false
		return &Concurrency{V: ExprVersion, Kind: "group", Group: group, Cancel: &Cancel{Value: &fixed}}
	case yaml.MappingNode:
		var group, cancel *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			switch k := node.Content[i].Value; k {
			case "group":
				group = resolved(node.Content[i+1])
			case "cancel-in-progress":
				cancel = resolved(node.Content[i+1])
			default:
				return refuse(node.Content[i], "", "concurrency has the unknown key "+k+": it takes group and cancel-in-progress")
			}
		}
		if group == nil || isNull(group) {
			return refuse(node, "", "concurrency names no group: a mapping takes group and cancel-in-progress")
		}
		if group.Kind != yaml.ScalarNode || group.ShortTag() != "!!str" {
			return refuse(group, group.Value, "the concurrency group is not text")
		}
		compiled, why := compileGroup(group)
		if why != "" {
			return refuse(group, group.Value, why)
		}
		flag, why := compileCancel(cancel)
		if why != "" {
			return refuse(cancel, cancel.Value, why)
		}
		return &Concurrency{V: ExprVersion, Kind: "group", Group: compiled, Cancel: flag}
	}
	return refuse(node, "", "concurrency must be "+concurrencyForms)
}

// compileGroup compiles a group's text as a template: GitHub's group is a
// non-empty string, an expression part interpolated as text
func compileGroup(n *yaml.Node) (*Template, string) {
	text := n.Value
	if len(text) > MaxExprBytes {
		return nil, fmt.Sprintf("the concurrency group is %d bytes; the bound is %d", len(text), MaxExprBytes)
	}
	if strings.TrimSpace(text) == "" {
		return nil, "the concurrency group is empty"
	}
	parts, why := compileParts(text, fieldConcurrency)
	if why != "" {
		return nil, why
	}
	return &Template{Text: text, Line: n.Line, Col: n.Column, Parts: parts}, ""
}

// compileParts compiles a template's text into its literal and expression
// parts for a field, bounded as one declared output is
func compileParts(text string, f field) ([]Part, string) {
	segs, err := splitTemplate(text)
	if err != nil {
		return nil, err.Error()
	}
	parts := []Part{}
	var nodes []*Node
	for _, s := range segs {
		if !s.expr {
			lit := s.text
			parts = append(parts, Part{Lit: &lit})
			continue
		}
		n, err := parseRange(text, s.from, s.to)
		if err != nil {
			return nil, err.Error()
		}
		if reason := check(n, f); reason != "" {
			return nil, reason
		}
		nodes = append(nodes, n)
		parts = append(parts, Part{Expr: n})
	}
	if reason := bounds(nodes); reason != "" {
		return nil, reason
	}
	return parts, ""
}

// compileCancel compiles cancel-in-progress: absent is false, a YAML
// boolean is that value, one ${{ }} expression (blanks around it aside)
// is compiled; anything else — a quoted 'true', text around an
// expression, a number — is refused, as GitHub's boolean field refuses it
func compileCancel(n *yaml.Node) (*Cancel, string) {
	if n == nil {
		fixed := false
		return &Cancel{Value: &fixed}, ""
	}
	if n.Kind == yaml.ScalarNode && isNull(n) {
		return nil, "cancel-in-progress is empty: write true, false or one ${{ }} expression"
	}
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool" {
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, "cancel-in-progress " + n.Value + " is not true or false"
		}
		return &Cancel{Text: n.Value, Line: n.Line, Col: n.Column, Value: &b}, ""
	}
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" && strings.Contains(n.Value, "${{") {
		e, why := compileSingle(n.Value, fieldConcurrency, "cancel-in-progress")
		if why != "" {
			return nil, why
		}
		return &Cancel{Text: n.Value, Line: n.Line, Col: n.Column, Expr: e}, ""
	}
	return nil, fmt.Sprintf("cancel-in-progress is true, false or one ${{ }} expression, not %q", n.Value)
}

// compileSingle compiles a field written as exactly one ${{ }} expression
// with nothing but blanks around it
func compileSingle(text string, f field, what string) (*Node, string) {
	if len(text) > MaxExprBytes {
		return nil, fmt.Sprintf("%s is %d bytes; the bound is %d", what, len(text), MaxExprBytes)
	}
	segs, err := splitTemplate(text)
	if err != nil {
		return nil, err.Error()
	}
	var exprs []segment
	for _, s := range segs {
		if s.expr {
			exprs = append(exprs, s)
		} else if strings.TrimSpace(s.text) != "" {
			return nil, what + " mixes text with ${{ }}: it would be text, never true or false; write one ${{ }} expression"
		}
	}
	if len(exprs) != 1 {
		return nil, fmt.Sprintf("%s holds %d ${{ }} expressions; write one", what, len(exprs))
	}
	n, err := parseRange(text, exprs[0].from, exprs[0].to)
	if err != nil {
		return nil, err.Error()
	}
	if reason := bounds([]*Node{n}); reason != "" {
		return nil, reason
	}
	if reason := check(n, f); reason != "" {
		return nil, reason
	}
	return n, ""
}
