package plan

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TriggerCatalogVersion is the version of the trigger catalog a resolver
// posts beside the lock (Delivery 1C). The ship refuses another version.
const TriggerCatalogVersion = 1

// Native bounds on a catalog (the reference documents none).
const (
	MaxTriggerEvents    = 64
	MaxSchedules        = 32
	MaxCatalogWorkflows = 256
	MaxActivityTypes    = 32
)

// TriggerCatalog is what the pinned compiler reads of every workflow file's
// `on:` at a revision (Delivery 1C): the events each declares with their
// compiled filters and schedules, and the offset table of every timezone an
// accepted schedule names. The ship evaluates it; it never reads the YAML.
type TriggerCatalog struct {
	Version   int               `json:"version"`
	Workflows []TriggerWorkflow `json:"workflows"`
	Zones     map[string]Zone   `json:"zones"`
}

// TriggerWorkflow is one workflow file's events, in the order `on:` lists
// them. Refusal is why its `on:` cannot be read at all; then it lists no
// event and nothing runs it automatically.
type TriggerWorkflow struct {
	Workflow string         `json:"workflow"`
	Name     string         `json:"name"`
	Events   []TriggerEvent `json:"events"`
	Refusal  string         `json:"refusal"`
}

// TriggerEvent is one event of `on:` and its configuration, compiled.
// Refusal is why the compiler refuses this event (a malformed or mutually
// exclusive filter, an event this forge has no source for); a refused event
// never selects its workflow, and the workflow's other events are unaffected.
// Types are a pull_request's activity types (the reference's defaults when
// TypesDeclared is false). Line and Col place the event in the file.
type TriggerEvent struct {
	Event         string     `json:"event"`
	Refusal       string     `json:"refusal"`
	Branches      *Filter    `json:"branches"`
	Tags          *Filter    `json:"tags"`
	Paths         *Filter    `json:"paths"`
	Types         []string   `json:"types"`
	TypesDeclared bool       `json:"typesDeclared"`
	Schedules     []Schedule `json:"schedules"`
	Line          int        `json:"line"`
	Col           int        `json:"col"`
}

// Filter is one branches/tags/paths list: an ordered include list (whose
// `!` patterns exclude again, and later positive ones re-include), or an
// `-ignore` list (Ignore) whose every pattern excludes.
type Filter struct {
	Ignore   bool      `json:"ignore"`
	Patterns []Pattern `json:"patterns"`
}

// the events of the captured GitHub reference
var githubEvents = map[string]bool{
	"branch_protection_rule": true, "check_run": true, "check_suite": true, "create": true, "delete": true,
	"deployment": true, "deployment_status": true, "discussion": true, "discussion_comment": true, "fork": true,
	"gollum": true, "image_version": true, "issue_comment": true, "issues": true, "label": true, "merge_group": true,
	"milestone": true, "page_build": true, "public": true, "pull_request": true, "pull_request_review": true,
	"pull_request_review_comment": true, "pull_request_target": true, "push": true, "registry_package": true,
	"release": true, "repository_dispatch": true, "schedule": true, "status": true, "watch": true,
	"workflow_call": true, "workflow_dispatch": true, "workflow_run": true,
}

// the pull_request activity types of the captured reference, and its
// defaults
var pullRequestTypes = map[string]bool{
	"assigned": true, "unassigned": true, "labeled": true, "unlabeled": true, "opened": true, "edited": true,
	"closed": true, "reopened": true, "synchronize": true, "converted_to_draft": true, "locked": true,
	"unlocked": true, "enqueued": true, "dequeued": true, "milestoned": true, "demilestoned": true,
	"ready_for_review": true, "review_requested": true, "review_request_removed": true,
	"auto_merge_enabled": true, "auto_merge_disabled": true,
}

var defaultPullRequestTypes = []string{"opened", "synchronize", "reopened"}

func resolved(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// TriggersOf compiles one workflow file's `on:` (Delivery 1C). It never
// fails: what cannot be read is a refusal of the workflow or of one event,
// carried with its reason, so the catalog lists every file.
func TriggersOf(data []byte, fileName string) TriggerWorkflow {
	w := TriggerWorkflow{Workflow: fileName, Name: fileName, Events: []TriggerEvent{}}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		w.Refusal = "the workflow is not valid YAML: " + err.Error()
		return w
	}
	top := topMapping(&doc)
	if top == nil {
		w.Refusal = "the workflow is not a mapping"
		return w
	}
	if name := resolved(mappingValue(top, "name")); name != nil && name.Kind == yaml.ScalarNode && name.Value != "" {
		w.Name = name.Value
	}
	on := resolved(mappingValue(top, "on"))
	if on == nil || isNull(on) {
		w.Refusal = "the workflow declares no on: events"
		return w
	}
	type named struct {
		name   string
		config *yaml.Node
		at     *yaml.Node
	}
	var events []named
	switch on.Kind {
	case yaml.ScalarNode:
		if on.ShortTag() != "!!str" {
			w.Refusal = fmt.Sprintf("on: %s is not an event name", on.Value)
			return w
		}
		events = append(events, named{on.Value, nil, on})
	case yaml.SequenceNode:
		for _, item := range on.Content {
			item = resolved(item)
			if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
				w.Refusal = "on: must be an event name, a list of event names or a mapping of events"
				return w
			}
			events = append(events, named{item.Value, nil, item})
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			events = append(events, named{on.Content[i].Value, resolved(on.Content[i+1]), on.Content[i]})
		}
	default:
		w.Refusal = "on: must be an event name, a list of event names or a mapping of events"
		return w
	}
	if len(events) > MaxTriggerEvents {
		w.Refusal = fmt.Sprintf("on: lists %d events; the bound is %d", len(events), MaxTriggerEvents)
		return w
	}
	seen := map[string]bool{}
	for _, e := range events {
		if seen[e.name] {
			w.Refusal = fmt.Sprintf("on: lists %s twice", e.name)
			w.Events = []TriggerEvent{}
			return w
		}
		seen[e.name] = true
		w.Events = append(w.Events, compileEvent(e.name, e.config, e.at.Line, e.at.Column))
	}
	return w
}

func compileEvent(name string, config *yaml.Node, line, col int) TriggerEvent {
	e := TriggerEvent{Event: name, Line: line, Col: col}
	switch name {
	case "push":
		e.Refusal = compileFilters(&e, config, []string{"branches", "branches-ignore", "tags", "tags-ignore", "paths", "paths-ignore"})
	case "pull_request":
		e.Refusal = compileFilters(&e, config, []string{"types", "branches", "branches-ignore", "paths", "paths-ignore"})
		if e.Refusal == "" && !e.TypesDeclared {
			e.Types = append([]string(nil), defaultPullRequestTypes...)
		}
	case "schedule":
		e.Refusal = compileSchedules(&e, config)
	case "workflow_dispatch":
		// its inputs are the manual-run inventory's (DispatchOf); no
		// filter of any other event applies to a manual run
	case "pull_request_target":
		e.Refusal = "pull_request_target is not implemented: this forge has no privileged pull request path; use pull_request, which runs unprivileged"
	case "workflow_call":
		e.Refusal = "workflow_call: reusable workflows are not implemented in this release"
	default:
		if githubEvents[name] {
			e.Refusal = name + ": this forge has no native source for this event; it never runs here"
		} else {
			e.Refusal = name + " is not an event of the captured GitHub reference"
		}
	}
	if e.Refusal != "" {
		e.Branches, e.Tags, e.Paths, e.Types, e.TypesDeclared, e.Schedules = nil, nil, nil, nil, false, nil
	}
	return e
}

// compileFilters reads a push or pull_request configuration: a mapping of
// the given keys, or nothing
func compileFilters(e *TriggerEvent, config *yaml.Node, keys []string) string {
	if config == nil || isNull(config) {
		return ""
	}
	if config.Kind != yaml.MappingNode {
		return e.Event + ": its configuration must be a mapping of filters"
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	present := map[string]*yaml.Node{}
	for i := 0; i+1 < len(config.Content); i += 2 {
		k := config.Content[i].Value
		if !allowed[k] {
			return e.Event + ": unknown key " + k
		}
		present[k] = resolved(config.Content[i+1])
	}
	for _, base := range []string{"branches", "tags", "paths"} {
		if present[base] != nil && present[base+"-ignore"] != nil {
			return fmt.Sprintf("%s: %s and %s-ignore cannot both filter one event", e.Event, base, base)
		}
	}
	for _, k := range keys {
		node, ok := present[k]
		if !ok {
			continue
		}
		if k == "types" {
			if why := compileTypes(e, node); why != "" {
				return why
			}
			continue
		}
		f, why := compileFilter(e.Event, k, node)
		if why != "" {
			return why
		}
		switch strings.TrimSuffix(k, "-ignore") {
		case "branches":
			e.Branches = f
		case "tags":
			e.Tags = f
		case "paths":
			e.Paths = f
		}
	}
	return ""
}

func scalars(node *yaml.Node) ([]*yaml.Node, bool) {
	if node == nil || isNull(node) {
		return nil, false
	}
	if node.Kind == yaml.ScalarNode {
		return []*yaml.Node{node}, true
	}
	if node.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := []*yaml.Node{}
	for _, item := range node.Content {
		item = resolved(item)
		if item.Kind != yaml.ScalarNode {
			return nil, false
		}
		out = append(out, item)
	}
	return out, true
}

func compileFilter(event, key string, node *yaml.Node) (*Filter, string) {
	items, ok := scalars(node)
	if !ok {
		return nil, fmt.Sprintf("%s: %s must be a list of patterns", event, key)
	}
	if len(items) == 0 {
		return nil, fmt.Sprintf("%s: %s lists no pattern", event, key)
	}
	if len(items) > MaxPatterns {
		return nil, fmt.Sprintf("%s: %s lists %d patterns; the bound is %d", event, key, len(items), MaxPatterns)
	}
	ignore := strings.HasSuffix(key, "-ignore")
	f := &Filter{Ignore: ignore, Patterns: []Pattern{}}
	positive := false
	for _, item := range items {
		p, err := CompileGlob(item.Value)
		if err != nil {
			return nil, fmt.Sprintf("%s: %s pattern %q: %v", event, key, item.Value, err)
		}
		if ignore && p.Negated {
			return nil, fmt.Sprintf("%s: %s pattern %q: a ! pattern in %s is not defined by the reference; use %s with ! patterns", event, key, item.Value, key, strings.TrimSuffix(key, "-ignore"))
		}
		positive = positive || !p.Negated
		f.Patterns = append(f.Patterns, p)
	}
	if !ignore && !positive {
		return nil, fmt.Sprintf("%s: %s has only ! patterns; a ! pattern excludes what a positive pattern before it included (use %s-ignore to only exclude)", event, key, key)
	}
	return f, ""
}

func compileTypes(e *TriggerEvent, node *yaml.Node) string {
	items, ok := scalars(node)
	if !ok {
		return e.Event + ": types must be a list of activity types"
	}
	if len(items) == 0 {
		return e.Event + ": types lists no activity type"
	}
	if len(items) > MaxActivityTypes {
		return fmt.Sprintf("%s: types lists %d activity types; the bound is %d", e.Event, len(items), MaxActivityTypes)
	}
	seen := map[string]bool{}
	e.Types = []string{}
	for _, item := range items {
		if !pullRequestTypes[item.Value] {
			return fmt.Sprintf("%s: %s is not a pull_request activity type of the captured reference", e.Event, item.Value)
		}
		if !seen[item.Value] {
			seen[item.Value] = true
			e.Types = append(e.Types, item.Value)
		}
	}
	e.TypesDeclared = true
	return ""
}

// compileSchedules reads `schedule:`: a list of {cron, timezone} entries,
// each refused alone when it cannot run
func compileSchedules(e *TriggerEvent, config *yaml.Node) string {
	if config == nil || config.Kind != yaml.SequenceNode {
		return "schedule: must be a list of {cron, timezone} entries"
	}
	if len(config.Content) == 0 {
		return "schedule lists no cron entry"
	}
	if len(config.Content) > MaxSchedules {
		return fmt.Sprintf("schedule lists %d entries; the bound is %d", len(config.Content), MaxSchedules)
	}
	e.Schedules = []Schedule{}
	for _, item := range config.Content {
		e.Schedules = append(e.Schedules, compileScheduleEntry(resolved(item)))
	}
	return ""
}

func compileScheduleEntry(item *yaml.Node) Schedule {
	if item.Kind != yaml.MappingNode {
		return Schedule{Cron: item.Value, Refusal: "a schedule entry must be a mapping with a cron key"}
	}
	var cron, tz *yaml.Node
	for i := 0; i+1 < len(item.Content); i += 2 {
		v := resolved(item.Content[i+1])
		switch k := item.Content[i].Value; k {
		case "cron":
			cron = v
		case "timezone":
			tz = v
		default:
			return Schedule{Refusal: "a schedule entry has the unknown key " + k + "; it takes cron and timezone"}
		}
	}
	if cron == nil || cron.Kind != yaml.ScalarNode || isNull(cron) {
		return Schedule{Refusal: "a schedule entry names no cron"}
	}
	zone := ""
	if tz != nil && !isNull(tz) {
		if tz.Kind != yaml.ScalarNode {
			return Schedule{Cron: cron.Value, Refusal: "a schedule's timezone must be an IANA timezone name"}
		}
		zone = tz.Value
	}
	s, err := ParseCron(cron.Value)
	if err != nil {
		return Schedule{Cron: cron.Value, Timezone: zone, Refusal: err.Error()}
	}
	if zone != "" {
		if _, err := ZoneTable(zone); err != nil {
			return Schedule{Cron: cron.Value, Timezone: zone, Refusal: err.Error()}
		}
	}
	s.Timezone = zone
	return s
}

// CatalogOf compiles every workflow file, in the order given, and the zone
// table of every timezone an accepted schedule names.
func CatalogOf(files []string, read func(string) ([]byte, error)) (TriggerCatalog, error) {
	cat := TriggerCatalog{Version: TriggerCatalogVersion, Workflows: []TriggerWorkflow{}, Zones: map[string]Zone{}}
	if len(files) > MaxCatalogWorkflows {
		return cat, fmt.Errorf("the revision has %d workflow files; the bound is %d", len(files), MaxCatalogWorkflows)
	}
	for _, file := range files {
		data, err := read(file)
		if err != nil {
			return cat, fmt.Errorf("%s: %w", file, err)
		}
		w := TriggersOf(data, file)
		for _, e := range w.Events {
			for _, s := range e.Schedules {
				if s.Refusal != "" || s.Timezone == "" {
					continue
				}
				if _, ok := cat.Zones[s.Timezone]; ok {
					continue
				}
				z, err := ZoneTable(s.Timezone)
				if err != nil {
					return cat, err
				}
				cat.Zones[s.Timezone] = z
			}
		}
		cat.Workflows = append(cat.Workflows, w)
	}
	return cat, nil
}
