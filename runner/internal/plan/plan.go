// Package plan is the daemon's YAML walk: the job-level `needs`, `if` and
// `outputs` act -l does not print, compiled to the versioned structure the
// ship evaluates (CI-EXPR-2), the matrix flag, and the single-job
// projection act runs on (CI-PROJECT-1), which carries the job's signed
// needs context. The walk reads YAML; it executes nothing.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// JobInfo is what the walk learns about one job that act -l omits.
type JobInfo struct {
	Needs []string
	// the job-level if, compiled (CI-EXPR-2); nil when the job has none
	Cond *Condition
	// the job's declared outputs (jobs.<id>.outputs), compiled, in
	// declaration order; the ship resolves them from the accepted attempt
	Outputs     []Output
	Matrix      bool
	Environment string // the job's `environment:` name, "" when none
	// the job's `runs-on` (a string or a list; the ship treats a string
	// as a one-element set) and `timeout-minutes` (0 when none)
	RunsOn         []string
	TimeoutMinutes int
	// whether the job declares a job-level `concurrency:`, which this
	// release refuses with the reason (CI-CONCURRENCY-1): only the
	// workflow-level group is evaluated
	Concurrency bool
}

// Walk reads one workflow file's jobs: needs (string or list), the
// compiled if and outputs, and whether strategy.matrix is present.
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
			// an empty if is no condition, as act reads it; one that is not a
			// single value is refused, never read as true
			if cond := mappingValue(job, "if"); cond != nil && !isNull(cond) {
				c := Condition{V: ExprVersion, Line: cond.Line, Col: cond.Column, Refusal: "the if is not a single expression"}
				if cond.Kind == yaml.ScalarNode {
					c = CompileCondition(cond.Value, cond.Line, cond.Column)
				}
				info.Cond = &c
			}
			if outputs := mappingValue(job, "outputs"); outputs != nil && !isNull(outputs) {
				if outputs.Kind != yaml.MappingNode {
					info.Outputs = []Output{{V: ExprVersion, Line: outputs.Line, Col: outputs.Column, Refusal: "outputs is not a mapping of output names"}}
				}
				for i := 0; outputs.Kind == yaml.MappingNode && i+1 < len(outputs.Content); i += 2 {
					name, value := outputs.Content[i].Value, outputs.Content[i+1]
					if value.Kind != yaml.ScalarNode {
						info.Outputs = append(info.Outputs, Output{Name: name, V: ExprVersion, Line: value.Line, Col: value.Column, Refusal: "output " + name + " is not a single value"})
						continue
					}
					info.Outputs = append(info.Outputs, CompileOutput(name, value.Value, value.Line, value.Column))
				}
			}
			if strategy := mappingValue(job, "strategy"); strategy != nil && strategy.Kind == yaml.MappingNode {
				if mappingValue(strategy, "matrix") != nil {
					info.Matrix = true
				}
			}
			// a job-level `concurrency:` in any form, even an empty one, is
			// declared: refused, never ignored (CI-CONCURRENCY-1)
			for k := 0; k+1 < len(job.Content); k += 2 {
				if job.Content[k].Value == "concurrency" {
					info.Concurrency = true
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

// SafeFilename is act's own directory naming for a `uses:` string in its
// action cache (pkg/runner/step_action_remote.go safeFilename, 0.2.89):
// the bundle materializes each locked action under exactly this name.
func SafeFilename(s string) string {
	return strings.NewReplacer(
		`<`, "-", `>`, "-", `:`, "-", `"`, "-", `/`, "-", `\`, "-", `|`, "-", `?`, "-", `*`, "-",
	).Replace(s)
}

// ProjectionName is the run name act sees for an attempt's job: the
// attempt id, a slash, the job's own name or else its id (CI-PROJECT-1.1).
// act names a job's container, volumes and services network from
// sha256(<workflow name>/<run name>) and force-removes an existing
// container of that name, so two attempts on the same job on one Docker
// daemon must not share it. The attempt goes in the run name, never in
// the workflow's name, which is github.workflow; github.job is the id.
func ProjectionName(attempt, original string) string {
	return attempt + "/" + original
}

// Project writes the single-job projection of a workflow: the original
// bytes with every job but jobID removed, jobID's own `needs` and
// job-level `if` keys removed, and jobID's `name:` replaced by one line
// holding ProjectionName(attempt, its name) — or its id when it has no
// name — right after the job's key. Nothing else is re-encoded: the
// projection is cut from the original text by the line numbers yaml.v3
// reports, so every other kept line, the workflow's own `name:` among
// them, is byte-identical to the source. act then runs only what the ship
// admitted, under a run name no other attempt shares. A job written as a
// flow mapping has no line of its own for the name and is refused.
func Project(data []byte, jobID, attempt, fileName string) ([]byte, error) {
	return ProjectImages(data, jobID, attempt, fileName, nil)
}

// ProjectImages is Project with the locked container images renamed: on
// the chosen job's `container:`, `image:` and `uses: docker://` lines,
// each reference the lock pinned (as the workflow wrote it) becomes the
// local name its archive was loaded under, so act runs exactly the
// locked bytes and never asks a registry (P03). Nothing else on those
// lines changes; a reference the lock does not name is left as written.
func ProjectImages(data []byte, jobID, attempt, fileName string, images map[string]string) ([]byte, error) {
	p, err := ProjectJob(data, jobID, attempt, fileName, images)
	return p.Data, err
}

// Projection is a job's single-job projection, and how many of its
// expressions' `needs` roots read the signed needs context from the
// carrier act is given as the variable NeedsCarrier (0: the job reads no
// needs, and act needs no carrier).
type Projection struct {
	Data  []byte
	Needs int
}

// ProjectJob is ProjectImages, carrying the job's needs context. act
// gives a projected job no `needs` context of its own — it builds one only
// from upstream jobs of the same run, and the projection never holds one —
// so every `needs` root in the job's expressions is rewritten to read the
// ship's signed needs context from the NeedsCarrier variable, as data
// (RewriteNeedsExpression). Such a job is re-encoded from its YAML tree
// with only those expressions changed, and the result is decoded again
// and compared with the intended job before it is used; a job that reads
// no needs keeps the byte-for-byte cut.
func ProjectJob(data []byte, jobID, attempt, fileName string, images map[string]string) (Projection, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Projection{}, err
	}
	top := topMapping(&doc)
	if top == nil {
		return Projection{}, errors.New("workflow is not a mapping")
	}
	lines := strings.SplitAfter(string(data), "\n")
	total := len(lines)
	// top-level keys in document order, to bound the jobs block
	var jobsKey, jobsVal *yaml.Node
	var jobsEnd = total // exclusive, 0-based line index
	for i := 0; i+1 < len(top.Content); i += 2 {
		k, v := top.Content[i], top.Content[i+1]
		if k.Value == "jobs" {
			jobsKey, jobsVal = k, v
			continue
		}
		if jobsKey != nil && k.Line-1 < jobsEnd {
			jobsEnd = k.Line - 1
		}
	}
	if jobsKey == nil || jobsVal.Kind != yaml.MappingNode {
		return Projection{}, errors.New("workflow has no jobs mapping")
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
		return Projection{}, fmt.Errorf("job %q is not in the workflow", jobID)
	}
	if chosen.value.Kind != yaml.MappingNode || chosen.value.Style&yaml.FlowStyle != 0 || len(chosen.value.Content) == 0 {
		return Projection{}, fmt.Errorf("job %q is not a block mapping: the projection names the job's run on a line of its own", jobID)
	}
	// the job's run name: the attempt, then the job's own name (act
	// interpolates it, so an expression is kept as written) or its id
	runName := jobID
	if name := mappingValue(chosen.value, "name"); name != nil {
		if name.Kind != yaml.ScalarNode {
			return Projection{}, fmt.Errorf("job %q: name is not a string", jobID)
		}
		runName = name.Value
	}
	// the job as act will run it: no needs, no job-level if, its own name
	// replaced, and every needs root of its expressions read from the
	// carrier
	job := copyNode(chosen.value)
	dropKeys(job, "needs", "if", "name")
	carried, err := rewriteJob(job)
	if err != nil {
		return Projection{}, fmt.Errorf("job %q: %w", jobID, err)
	}
	runName, named, err := RewriteNeedsTemplate(runName)
	if err != nil {
		return Projection{}, fmt.Errorf("job %q: name: %w", jobID, err)
	}
	carried += named
	indent := strings.Repeat(" ", chosen.value.Content[0].Column-1)
	nameLine := indent + "name: " + yamlQuoted(ProjectionName(attempt, runName)) + "\n"
	var body []string
	if carried > 0 {
		renameImageNodes(job, images)
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(job); err != nil {
			return Projection{}, fmt.Errorf("job %q: %w", jobID, err)
		}
		if err := enc.Close(); err != nil {
			return Projection{}, fmt.Errorf("job %q: %w", jobID, err)
		}
		for _, line := range strings.SplitAfter(buf.String(), "\n") {
			if line != "" {
				body = append(body, indent+line)
			}
		}
	}
	// lines to drop inside the chosen job: needs, if and its own name,
	// each from its key line to the line before the next key of the job
	// mapping
	drop := map[int]bool{}
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
		if keys[i].name == "needs" || keys[i].name == "if" || keys[i].name == "name" {
			for l := keys[i].start; l < keys[i].end; l++ {
				drop[l] = true
			}
		}
	}
	var out strings.Builder
	for l := 0; l < total; l++ {
		if l >= jobsKey.Line && l < jobsEnd {
			// inside the jobs block: keep only the chosen job's lines, its
			// run name right after its key
			if l < chosen.start || l >= chosen.end {
				continue
			}
			if l == chosen.start {
				out.WriteString(lines[l])
				out.WriteString(nameLine)
				for _, line := range body {
					out.WriteString(line)
				}
				continue
			}
			if carried > 0 || drop[l] {
				continue
			}
			line := lines[l]
			if len(images) > 0 {
				line = renameImage(line, images)
			}
			out.WriteString(line)
			continue
		}
		out.WriteString(lines[l])
	}
	projected := []byte(out.String())
	if carried > 0 {
		if err := sameJob(projected, jobID, ProjectionName(attempt, runName), job); err != nil {
			return Projection{}, fmt.Errorf("job %q: %w", jobID, err)
		}
	}
	return Projection{Data: projected, Needs: carried}, nil
}

// sameJob decodes the projection and checks that it holds exactly the one
// job, named for the attempt, equal as data to the intended tree
func sameJob(projected []byte, jobID, runName string, want *yaml.Node) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(projected, &doc); err != nil {
		return fmt.Errorf("the projection is not YAML: %w", err)
	}
	jobs := mappingValue(topMapping(&doc), "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) != 2 || jobs.Content[0].Value != jobID {
		return errors.New("the projection does not hold exactly its one job")
	}
	var got, expected map[string]any
	if err := jobs.Content[1].Decode(&got); err != nil {
		return err
	}
	if err := want.Decode(&expected); err != nil {
		return err
	}
	if got["name"] != runName {
		return errors.New("the projection does not name the attempt's run")
	}
	delete(got, "name")
	if !reflect.DeepEqual(got, expected) {
		return errors.New("the re-encoded job is not the job with its needs carried")
	}
	return nil
}

func copyNode(n *yaml.Node) *yaml.Node {
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = copyNode(child)
	}
	return &c
}

func dropKeys(m *yaml.Node, names ...string) {
	var kept []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		dropped := false
		for _, name := range names {
			if m.Content[i].Value == name {
				dropped = true
			}
		}
		if !dropped {
			kept = append(kept, m.Content[i], m.Content[i+1])
		}
	}
	m.Content = kept
}

// rewriteJob carries the needs context in every expression of a job's
// tree: a step's `if` is one expression when it has no ${{ }}, every
// other value (and key) a template
func rewriteJob(job *yaml.Node) (int, error) {
	total := 0
	for i := 0; i+1 < len(job.Content); i += 2 {
		k, v := job.Content[i], job.Content[i+1]
		n, err := rewriteValue(k, false)
		if err != nil {
			return 0, err
		}
		total += n
		if k.Value == "steps" && v.Kind == yaml.SequenceNode {
			for _, step := range v.Content {
				if step.Kind != yaml.MappingNode {
					n, err = rewriteValue(step, false)
				} else {
					n, err = rewriteStep(step)
				}
				if err != nil {
					return 0, err
				}
				total += n
			}
			continue
		}
		if n, err = rewriteValue(v, false); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func rewriteStep(step *yaml.Node) (int, error) {
	total := 0
	for i := 0; i+1 < len(step.Content); i += 2 {
		k, v := step.Content[i], step.Content[i+1]
		n, err := rewriteValue(k, false)
		if err != nil {
			return 0, err
		}
		total += n
		if n, err = rewriteValue(v, k.Value == "if"); err != nil {
			return 0, fmt.Errorf("step %s: %w", k.Value, err)
		}
		total += n
	}
	return total, nil
}

func rewriteValue(n *yaml.Node, condition bool) (int, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		var text string
		var count int
		var err error
		if condition && !strings.Contains(n.Value, "${{") {
			text, count, err = RewriteNeedsExpression(n.Value)
		} else {
			text, count, err = RewriteNeedsTemplate(n.Value)
		}
		if err != nil {
			return 0, err
		}
		if count > 0 {
			n.Value, n.Tag = text, "!!str"
		}
		return count, nil
	case yaml.MappingNode, yaml.SequenceNode:
		total := 0
		for _, child := range n.Content {
			count, err := rewriteValue(child, false)
			if err != nil {
				return 0, err
			}
			total += count
		}
		return total, nil
	}
	return 0, nil
}

// renameImageNodes is renameImage on a tree: a locked `container:`,
// `image:` or `uses: docker://` reference becomes its loaded name
func renameImageNodes(n *yaml.Node, images map[string]string) {
	if len(images) == 0 {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if v.Kind == yaml.ScalarNode {
				switch k.Value {
				case "container", "image":
					if tag, ok := images[v.Value]; ok {
						v.Value = tag
					}
				case "uses":
					if ref := strings.TrimPrefix(v.Value, "docker://"); ref != v.Value {
						if tag, ok := images[ref]; ok {
							v.Value = "docker://" + tag
						}
					}
				}
			}
			renameImageNodes(v, images)
		}
	case yaml.SequenceNode:
		for _, child := range n.Content {
			renameImageNodes(child, images)
		}
	}
}

// yamlQuoted is s as a YAML double-quoted scalar: a JSON string is one,
// its escapes YAML's own
func yamlQuoted(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// renameImage rewrites one job line that names an image (`container:`,
// `image:` or `uses: docker://`) when its reference is a locked one.
func renameImage(line string, images map[string]string) string {
	trimmed := strings.TrimLeft(line, " \t-")
	key := ""
	for _, k := range []string{"container:", "image:", "uses:"} {
		if strings.HasPrefix(trimmed, k) {
			key = k
		}
	}
	if key == "" {
		return line
	}
	value := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
	value = strings.Trim(value, `"'`)
	written := strings.TrimPrefix(value, "docker://")
	if key == "uses:" && written == value {
		return line // an action, not an image
	}
	tag, ok := images[written]
	if !ok {
		return line
	}
	return strings.Replace(line, written, tag, 1)
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
