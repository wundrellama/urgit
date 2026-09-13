// Package act parses what the pinned act (0.2.89) prints and how it is
// invoked. The `act -l` table is the plan source (D1, R2.3′-A).
package act

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is the act release this daemon's parsers are pinned to.
const Version = "0.2.89"

// ListedJob is one row of the `act -l` table.
type ListedJob struct {
	Stage        int
	JobID        string
	JobName      string
	WorkflowName string
	WorkflowFile string
	Events       []string
}

var columns = regexp.MustCompile(`\s{2,}`)

// ParseList reads the table act -l prints: a header line
// `Stage  Job ID  Job name  Workflow name  Workflow file  Events`, one
// row per job, columns padded with two or more spaces, tables for
// several workflows separated by blank lines. Lines that are not part of
// a table (act's docker-host banner on the same stream, for one) are
// skipped; a header with other columns is an error, because the parse is
// pinned to the release.
func ParseList(output string) ([]ListedJob, error) {
	var jobs []ListedJob
	inTable := false
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" {
			inTable = false
			continue
		}
		fields := columns.Split(strings.TrimSpace(line), -1)
		if fields[0] == "Stage" {
			if len(fields) != 6 || fields[1] != "Job ID" || fields[2] != "Job name" ||
				fields[3] != "Workflow name" || fields[4] != "Workflow file" || fields[5] != "Events" {
				return nil, fmt.Errorf("act -l header is not the %s shape: %q", Version, line)
			}
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if len(fields) != 6 {
			return nil, fmt.Errorf("act -l row has %d columns, not 6: %q", len(fields), line)
		}
		stage, err := strconv.Atoi(fields[0])
		if err != nil || stage < 0 {
			return nil, fmt.Errorf("act -l row stage %q is not a natural number", fields[0])
		}
		var events []string
		for _, e := range strings.Split(fields[5], ",") {
			if e = strings.TrimSpace(e); e != "" {
				events = append(events, e)
			}
		}
		jobs = append(jobs, ListedJob{
			Stage: stage, JobID: fields[1], JobName: fields[2],
			WorkflowName: fields[3], WorkflowFile: fields[4], Events: events,
		})
	}
	if !inTable && len(jobs) == 0 {
		return nil, errors.New("act -l printed no table")
	}
	return jobs, nil
}
