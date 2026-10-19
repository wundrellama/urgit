package plan

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Dispatch is what one workflow file declares for a manual run
// (`on.workflow_dispatch`), as the resolver reports it beside the lock:
// the ship lists it in the Run workflow form and admits a run's inputs
// against it. The compiler carries the declaration; the ship decides
// whether each definition is valid.
type Dispatch struct {
	Workflow string          `json:"workflow"` // the file name under .github/workflows
	Name     string          `json:"name"`     // the workflow's own name, else the file name
	Inputs   []DispatchInput `json:"inputs"`
	// Refusal names what this compiler cannot carry faithfully: the
	// workflow is listed, and cannot be run manually
	Refusal string `json:"refusal,omitempty"`
}

// DispatchInput is one declared input, in declaration order. Default is
// the text the workflow wrote (`false`, `0`, `warning`); nil when it
// declares none, which is not the same as an empty default.
type DispatchInput struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Default     *string  `json:"default"`
	Options     []string `json:"options"`
}

// DispatchOf reads one workflow file's manual-run declaration: nil when
// the workflow does not trigger on workflow_dispatch (in `on:` as a
// string, a list or a mapping). An input's type is string unless it says
// otherwise, as on GitHub.
func DispatchOf(data []byte, fileName string) (*Dispatch, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	top := topMapping(&doc)
	if top == nil {
		return nil, errors.New("yaml: the workflow is not a mapping")
	}
	declared, config := dispatchTrigger(mappingValue(top, "on"))
	if !declared {
		return nil, nil
	}
	d := &Dispatch{Workflow: fileName, Name: fileName, Inputs: []DispatchInput{}}
	if name := mappingValue(top, "name"); name != nil && name.Kind == yaml.ScalarNode && name.Value != "" {
		d.Name = name.Value
	}
	inputs, refusal := dispatchInputs(config)
	if refusal == "" {
		d.Inputs = inputs
		refusal = jobRefusal(data, top)
	}
	d.Refusal = refusal
	return d, nil
}

// dispatchTrigger says whether `on:` names workflow_dispatch, and gives
// its configuration node when `on:` is a mapping
func dispatchTrigger(on *yaml.Node) (bool, *yaml.Node) {
	if on == nil {
		return false, nil
	}
	switch on.Kind {
	case yaml.ScalarNode:
		return on.Value == "workflow_dispatch", nil
	case yaml.SequenceNode:
		for _, item := range on.Content {
			if item.Kind == yaml.ScalarNode && item.Value == "workflow_dispatch" {
				return true, nil
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Value == "workflow_dispatch" {
				return true, on.Content[i+1]
			}
		}
	}
	return false, nil
}

// the keys act's schema allows on one input (workflow-dispatch-input)
var inputKeys = map[string]bool{"description": true, "type": true, "required": true, "default": true, "options": true}

func dispatchInputs(config *yaml.Node) ([]DispatchInput, string) {
	out := []DispatchInput{}
	if config == nil || isNull(config) {
		return out, ""
	}
	if config.Kind != yaml.MappingNode {
		return nil, "workflow_dispatch must be a mapping"
	}
	for i := 0; i+1 < len(config.Content); i += 2 {
		if key := config.Content[i].Value; key != "inputs" {
			return nil, "workflow_dispatch: unknown key " + key
		}
	}
	inputs := mappingValue(config, "inputs")
	if inputs == nil || isNull(inputs) {
		return out, ""
	}
	if inputs.Kind != yaml.MappingNode {
		return nil, "workflow_dispatch inputs must be a mapping of input ids"
	}
	for i := 0; i+1 < len(inputs.Content); i += 2 {
		id, node := inputs.Content[i].Value, inputs.Content[i+1]
		in, refusal := dispatchInput(id, node)
		if refusal != "" {
			return nil, refusal
		}
		out = append(out, in)
	}
	return out, ""
}

func dispatchInput(id string, node *yaml.Node) (DispatchInput, string) {
	in := DispatchInput{ID: id, Type: "string", Options: []string{}}
	if node.Kind != yaml.MappingNode {
		return in, "input " + id + ": must be a mapping"
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if !inputKeys[key] {
			return in, "input " + id + ": unknown key " + key
		}
		switch key {
		case "description", "type":
			if value.Kind != yaml.ScalarNode {
				return in, "input " + id + ": " + key + " must be text"
			}
			if key == "description" {
				in.Description = value.Value
			} else if !isNull(value) {
				in.Type = value.Value
			}
		case "required":
			// an unquoted true or false, as act decodes it into a boolean
			if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!bool" {
				return in, "input " + id + ": required must be true or false"
			}
			in.Required = strings.EqualFold(value.Value, "true")
		case "default":
			if value.Kind != yaml.ScalarNode {
				return in, "input " + id + ": default must be a single value"
			}
			// `default:` with nothing, or null, is no default: act reads it
			// as the empty text an absent one gives too
			if !isNull(value) {
				text := value.Value
				in.Default = &text
			}
		case "options":
			if value.Kind != yaml.SequenceNode {
				return in, "input " + id + ": options must be a list of values"
			}
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode {
					return in, "input " + id + ": options must be a list of values"
				}
				in.Options = append(in.Options, item.Value)
			}
		}
	}
	return in, ""
}

// jobRefusal names the first job, in document order, whose job-level
// construct the ship does not run in this release, as the plan's own
// validation would refuse it after admission
func jobRefusal(data []byte, top *yaml.Node) string {
	jobs := mappingValue(top, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return "the workflow has no jobs mapping"
	}
	walked, err := Walk(data)
	if err != nil {
		return err.Error()
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		id := jobs.Content[i].Value
		info := walked[id]
		if info.Matrix {
			return "job " + id + ": a matrix strategy is not supported in this release"
		}
		for _, label := range info.RunsOn {
			if strings.Contains(label, "${{") {
				return "job " + id + ": a runs-on expression is not supported in this release"
			}
		}
		if info.Cond != nil && info.Cond.Refusal != "" {
			return "job " + id + ": the job-level if " + info.Cond.Text + " is refused: " + info.Cond.Refusal
		}
		if len(info.Outputs) > MaxJobOutputs {
			return fmt.Sprintf("job %s declares %d outputs; the bound is %d", id, len(info.Outputs), MaxJobOutputs)
		}
		for _, o := range info.Outputs {
			if o.Refusal != "" {
				return "job " + id + ": output " + o.Name + " is refused: " + o.Refusal
			}
		}
	}
	return ""
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}
