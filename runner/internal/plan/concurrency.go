package plan

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Concurrency is always emitted by this compiler. A missing field on an old
// wire means unknown; this object with Group == nil means known, no group.
// Expressions use the same bounded AST as job conditions and output templates.
type Concurrency struct {
	Group            *Output    `json:"group"`
	CancelInProgress *Condition `json:"cancelInProgress"`
	Refusal          string     `json:"refusal"`
}

type WorkflowConcurrency struct {
	Workflow    string      `json:"workflow"`
	Name        string      `json:"name"`
	Concurrency Concurrency `json:"concurrency"`
}

func ConcurrencyOf(data []byte, file string) WorkflowConcurrency {
	w := WorkflowConcurrency{Workflow: file, Name: file}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		w.Concurrency.Refusal = "concurrency: invalid YAML: " + err.Error()
		return w
	}
	top := topMapping(&doc)
	if top == nil {
		w.Concurrency.Refusal = "concurrency: workflow is not a mapping"
		return w
	}
	if n := resolved(mappingValue(top, "name")); n != nil && n.Kind == yaml.ScalarNode && n.Value != "" {
		w.Name = n.Value
	}
	w.Concurrency = compileConcurrency(resolved(mappingValue(top, "concurrency")))
	return w
}

func compileConcurrency(n *yaml.Node) Concurrency {
	c := Concurrency{}
	if n == nil {
		return c
	}
	refuse := func(why string) Concurrency {
		c.Refusal = "workflow concurrency: " + why
		return c
	}
	group, cancel := n, (*yaml.Node)(nil)
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if seen[k] || (k != "group" && k != "cancel-in-progress") {
				return refuse("unknown or repeated key " + k)
			}
			seen[k] = true
		}
		group = resolved(mappingValue(n, "group"))
		cancel = resolved(mappingValue(n, "cancel-in-progress"))
	}
	if group == nil || group.Kind != yaml.ScalarNode || group.ShortTag() != "!!str" {
		return refuse("group must be a string or expression")
	}
	g := CompileOutput("group", group.Value, group.Line, group.Column)
	if g.Refusal == "" && len(g.Parts) == 0 {
		empty := ""
		g.Parts = []Part{{Lit: &empty}}
	}
	c.Group = &g
	if g.Refusal != "" {
		return refuse("group: " + g.Refusal)
	}
	for _, p := range g.Parts {
		if why := concurrencyContext(p.Expr); why != "" {
			return refuse("group: " + why)
		}
	}
	if cancel != nil {
		if cancel.Kind != yaml.ScalarNode || (cancel.ShortTag() != "!!bool" && !strings.Contains(cancel.Value, "${{")) {
			return refuse("cancel-in-progress must be a boolean or expression")
		}
		v := CompileCondition(strings.ToLower(cancel.Value), cancel.Line, cancel.Column)
		// Preserve case in expression literals and input names.
		if cancel.ShortTag() != "!!bool" {
			v = CompileCondition(cancel.Value, cancel.Line, cancel.Column)
		}
		c.CancelInProgress = &v
		if v.Refusal != "" {
			return refuse("cancel-in-progress: " + v.Refusal)
		}
		if why := concurrencyContext(v.Expr); why != "" {
			return refuse("cancel-in-progress: " + why)
		}
	}
	return c
}

func concurrencyContext(n *Node) string {
	if n == nil {
		return ""
	}
	if n.K == "ctx" && n.S != "github" && n.S != "inputs" && n.S != "vars" {
		return "only github, inputs and vars contexts are available"
	}
	if n.K == "call" && statusFunctions[strings.ToLower(n.S)] {
		return "status functions are not available in workflow concurrency"
	}
	for _, child := range children(n) {
		if why := concurrencyContext(child); why != "" {
			return why
		}
	}
	return ""
}
