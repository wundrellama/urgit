package daemon

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"urgit/runner/internal/ship"
)

// Environment names are grant policy metadata, separate from the plan's
// job admission and projection. Expressions cannot select a credential.
func grantEnvironment(data []byte, job string) string {
	var doc struct {
		Jobs map[string]struct {
			Environment yaml.Node `yaml:"environment"`
		} `yaml:"jobs"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return ""
	}
	n := doc.Jobs[job].Environment
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "name" {
				n = *n.Content[i+1]
				break
			}
		}
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || strings.Contains(n.Value, "${{") {
		return ""
	}
	return n.Value
}

type secretScrubber struct{ values []string }

func newSecretScrubber(grants []ship.Grant) secretScrubber {
	var values []string
	for _, g := range grants {
		if g.Value != "" {
			values = append(values, g.Value)
		}
	}
	// Mask a longer value first if two credentials share a prefix.
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return secretScrubber{values: values}
}

func (s secretScrubber) text(value string) string {
	for _, secret := range s.values {
		value = strings.ReplaceAll(value, secret, "***")
	}
	return value
}

func (s secretScrubber) json(value any) any {
	switch v := value.(type) {
	case string:
		return s.text(v)
	case []any:
		for i := range v {
			v[i] = s.json(v[i])
		}
	case map[string]any:
		clean := make(map[string]any, len(v))
		for k, item := range v {
			clean[s.text(k)] = s.json(item)
		}
		return clean
	}
	return value
}

func (s secretScrubber) line(line []byte) []byte {
	var value any
	if json.Unmarshal(line, &value) == nil {
		if clean, err := json.Marshal(s.json(value)); err == nil {
			return clean
		}
	}
	// Non-JSON diagnostics must also be scrubbed before local logging.
	clean := s.text(string(line))
	for _, secret := range s.values {
		quoted, _ := json.Marshal(secret)
		clean = strings.ReplaceAll(clean, string(quoted[1:len(quoted)-1]), "***")
	}
	return []byte(clean)
}

type scrubbedReader struct {
	scanner *bufio.Scanner
	scrub   secretScrubber
	pending []byte
}

func (s secretScrubber) reader(source io.Reader) io.Reader {
	if len(s.values) == 0 {
		return source
	}
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	return &scrubbedReader{scanner: scanner, scrub: s}
}

func (r *scrubbedReader) Read(out []byte) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		r.pending = append(r.scrub.line(r.scanner.Bytes()), '\n')
	}
	n := copy(out, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
