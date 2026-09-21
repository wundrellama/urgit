// Package plan is the daemon's YAML walk: the job-level `needs` and `if`
// act -l does not print, compiled to the versioned structure the ship
// evaluates (CI-EXPR-1), the matrix flag, and the single-job projection
// act runs on (CI-PROJECT-1). The walk reads YAML; it executes nothing.
package plan

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Cond is the wire form of a compiled job-level condition, version 1.
type Cond struct {
	V       int    `json:"v"`
	Kind    string `json:"kind"`
	Job     string `json:"job,omitempty"`
	Output  string `json:"output,omitempty"`
	Literal string `json:"literal,omitempty"`
	Raw     string `json:"raw,omitempty"`
}

// JobInfo is what the walk learns about one job that act -l omits.
type JobInfo struct {
	Needs       []string
	Cond        *Cond
	Matrix      bool
	Environment string // the job's `environment:` name, "" when none
	// the job's `runs-on` (a string or a list; the ship treats a string
	// as a one-element set) and `timeout-minutes` (0 when none)
	RunsOn         []string
	TimeoutMinutes int
}

var outputEq = regexp.MustCompile(`^needs\.([A-Za-z0-9_-]+)\.outputs\.([A-Za-z0-9_-]+)\s*==\s*'([^']*)'$`)

// CompileIf turns a job-level `if` into the one supported shape,
// `needs.<job>.outputs.<name> == '<literal>'` (optionally inside
// `${{ }}`), or an %unsupported record carrying the raw text. The ship
// never sees the raw text as a condition.
func CompileIf(expr string) Cond {
	trimmed := strings.TrimSpace(expr)
	inner := trimmed
	if strings.HasPrefix(inner, "${{") && strings.HasSuffix(inner, "}}") {
		inner = strings.TrimSpace(inner[3 : len(inner)-2])
	}
	if m := outputEq.FindStringSubmatch(inner); m != nil {
		return Cond{V: 1, Kind: "output-eq", Job: m[1], Output: m[2], Literal: m[3]}
	}
	return Cond{V: 1, Kind: "unsupported", Raw: trimmed}
}

// Walk reads one workflow file's jobs: needs (string or list), the
// compiled if, and whether strategy.matrix is present.
func Walk(data []byte) (map[string]JobInfo, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	jobs := mappingValue(topMapping(&doc), "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil, errors.New("workflow has no jobs mapping")
	}
	out := map[string]JobInfo{}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		id := jobs.Content[i].Value
		job := jobs.Content[i+1]
		info := JobInfo{Needs: []string{}}
		if job.Kind == yaml.MappingNode {
			if needs := mappingValue(job, "needs"); needs != nil {
				switch needs.Kind {
				case yaml.ScalarNode:
					if needs.Value != "" {
						info.Needs = append(info.Needs, needs.Value)
					}
				case yaml.SequenceNode:
					for _, item := range needs.Content {
						info.Needs = append(info.Needs, item.Value)
					}
				}
			}
			if cond := mappingValue(job, "if"); cond != nil && cond.Kind == yaml.ScalarNode {
				c := CompileIf(cond.Value)
				info.Cond = &c
			}
			if strategy := mappingValue(job, "strategy"); strategy != nil && strategy.Kind == yaml.MappingNode {
				if mappingValue(strategy, "matrix") != nil {
					info.Matrix = true
				}
			}
			// `runs-on: ubuntu-latest` or `runs-on: [self-hosted, big-mem]`:
			// the labels the ship matches a daemon against (CI-P3-SCHED-A).
			// an expression here (`${{ … }}`) is carried as written; the
			// ship will find no daemon standing for it and say so.
			if runsOn := mappingValue(job, "runs-on"); runsOn != nil {
				switch runsOn.Kind {
				case yaml.ScalarNode:
					if runsOn.Value != "" {
						info.RunsOn = append(info.RunsOn, runsOn.Value)
					}
				case yaml.SequenceNode:
					for _, item := range runsOn.Content {
						if item.Kind == yaml.ScalarNode && item.Value != "" {
							info.RunsOn = append(info.RunsOn, item.Value)
						}
					}
				}
			}
			// `timeout-minutes: 30`: the job's own bound, which the ship
			// turns into the attempt's deadline (CI-DELIVERY-1.1 c)
			if timeout := mappingValue(job, "timeout-minutes"); timeout != nil && timeout.Kind == yaml.ScalarNode {
				if n, err := strconv.Atoi(strings.TrimSpace(timeout.Value)); err == nil && n > 0 {
					info.TimeoutMinutes = n
				}
			}
			// `environment: staging` or `environment: {name: staging, url: …}`;
			// the ship releases %env-scoped credentials by this name (D4)
			if env := mappingValue(job, "environment"); env != nil {
				switch env.Kind {
				case yaml.ScalarNode:
					info.Environment = env.Value
				case yaml.MappingNode:
					if name := mappingValue(env, "name"); name != nil && name.Kind == yaml.ScalarNode {
						info.Environment = name.Value
					}
				}
			}
		}
		out[id] = info
	}
	return out, nil
}

// PrereqEnv maps the ship's prereq-outputs to act --env assignments:
// NEEDS_<JOB>_OUTPUTS_<NAME>=<value>, upper-cased, non-alphanumerics
// replaced by `_`. Carried for a later phase; no ERPit step reads one.
func PrereqEnv(outputs map[string]map[string]string) []string {
	var env []string
	for job, values := range outputs {
		for name, value := range values {
			env = append(env, "NEEDS_"+envToken(job)+"_OUTPUTS_"+envToken(name)+"="+value)
		}
	}
	sort.Strings(env)
	return env
}

func envToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ProjectionName is the workflow name act sees for an attempt: the
// attempt id, a slash, the original name (CI-PROJECT-1.1). act names a
// job's container and volumes from sha256(workflow.Name/job.Name) and
// force-removes an existing container of that name, so two attempts on
// the same job on one Docker daemon must not share the workflow name.
func ProjectionName(attempt, original string) string {
	return attempt + "/" + original
}

// Project writes the single-job projection of a workflow: the original
// bytes with every job but jobID removed, jobID's own `needs` and
// job-level `if` keys removed, and the top-level `name:` rewritten to
// ProjectionName(attempt, original) — inserted when the workflow has
// none, where act would have used the file name. Nothing else is
// re-encoded: the projection is cut from the original text by the line
// numbers yaml.v3 reports, so every other kept line is byte-identical to
// the source. act then runs only what the ship admitted, under a name no
// other attempt shares.
func Project(data []byte, jobID, attempt, fileName string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	top := topMapping(&doc)
	if top == nil {
		return nil, errors.New("workflow is not a mapping")
	}
	lines := strings.SplitAfter(string(data), "\n")
	total := len(lines)
	// top-level keys in document order, to bound the jobs block; the
	// name key's own line (its scalar sits on it) is the one rewritten
	var jobsKey, jobsVal, nameKey, nameVal *yaml.Node
	var jobsEnd = total // exclusive, 0-based line index
	for i := 0; i+1 < len(top.Content); i += 2 {
		k, v := top.Content[i], top.Content[i+1]
		if k.Value == "jobs" {
			jobsKey, jobsVal = k, v
			continue
		}
		if k.Value == "name" && k.Column == 1 {
			nameKey, nameVal = k, v
		}
		if jobsKey != nil && k.Line-1 < jobsEnd {
			jobsEnd = k.Line - 1
		}
	}
	original := fileName
	if nameVal != nil && nameVal.Kind == yaml.ScalarNode {
		original = nameVal.Value
	}
	nameLine := "name: " + ProjectionName(attempt, original) + "\n"
	if jobsKey == nil || jobsVal.Kind != yaml.MappingNode {
		return nil, errors.New("workflow has no jobs mapping")
	}
	// each job's block: from its key line to the line before the next
	// job's key (or the end of the jobs block)
	type block struct {
		id         string
		start, end int // 0-based, end exclusive
		value      *yaml.Node
	}
	var blocks []block
	for i := 0; i+1 < len(jobsVal.Content); i += 2 {
		blocks = append(blocks, block{id: jobsVal.Content[i].Value, start: jobsVal.Content[i].Line - 1, value: jobsVal.Content[i+1]})
	}
	for i := range blocks {
		if i+1 < len(blocks) {
			blocks[i].end = blocks[i+1].start
		} else {
			blocks[i].end = jobsEnd
		}
	}
	var chosen *block
	for i := range blocks {
		if blocks[i].id == jobID {
			chosen = &blocks[i]
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("job %q is not in the workflow", jobID)
	}
	// lines to drop inside the chosen job: needs and if, each from its key
	// line to the line before the next key of the job mapping
	drop := map[int]bool{}
	if chosen.value.Kind == yaml.MappingNode {
		type keyLine struct {
			name       string
			start, end int
		}
		var keys []keyLine
		for i := 0; i+1 < len(chosen.value.Content); i += 2 {
			keys = append(keys, keyLine{name: chosen.value.Content[i].Value, start: chosen.value.Content[i].Line - 1})
		}
		sort.Slice(keys, func(a, b int) bool { return keys[a].start < keys[b].start })
		for i := range keys {
			if i+1 < len(keys) {
				keys[i].end = keys[i+1].start
			} else {
				keys[i].end = chosen.end
			}
			if keys[i].name == "needs" || keys[i].name == "if" {
				for l := keys[i].start; l < keys[i].end; l++ {
					drop[l] = true
				}
			}
		}
	}
	var out strings.Builder
	if nameKey == nil {
		out.WriteString(nameLine)
	}
	for l := 0; l < total; l++ {
		if nameKey != nil && l == nameKey.Line-1 {
			out.WriteString(nameLine)
			continue
		}
		if l >= jobsKey.Line && l < jobsEnd {
			// inside the jobs block: keep only the chosen job's lines
			if l < chosen.start || l >= chosen.end || drop[l] {
				continue
			}
		}
		out.WriteString(lines[l])
	}
	return []byte(out.String()), nil
}

// JobText returns the chosen job's block as it appears in the original
// text (for the projection's unit test), optionally without its needs
// and if lines.
func JobText(data []byte, jobID string, stripNeedsIf bool) (string, error) {
	if stripNeedsIf {
		projected, err := Project(data, jobID, "0v0", "x.yml")
		if err != nil {
			return "", err
		}
		return jobBlock(projected, jobID)
	}
	return jobBlock(data, jobID)
}

func jobBlock(data []byte, jobID string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	top := topMapping(&doc)
	lines := strings.SplitAfter(string(data), "\n")
	end := len(lines)
	var jobsKey, jobsVal *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		k, v := top.Content[i], top.Content[i+1]
		if k.Value == "jobs" {
			jobsKey, jobsVal = k, v
			continue
		}
		if jobsKey != nil && k.Line-1 < end {
			end = k.Line - 1
		}
	}
	if jobsKey == nil {
		return "", errors.New("no jobs")
	}
	for i := 0; i+1 < len(jobsVal.Content); i += 2 {
		if jobsVal.Content[i].Value != jobID {
			continue
		}
		start := jobsVal.Content[i].Line - 1
		stop := end
		if i+2 < len(jobsVal.Content) {
			stop = jobsVal.Content[i+2].Line - 1
		}
		return strings.Join(lines[start:stop], ""), nil
	}
	return "", fmt.Errorf("job %q not found", jobID)
}

func topMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
