package act

import (
	"os"
	"strings"
	"testing"
)

// the planted ERPit output (.scratch/act-list-erpit.txt): two tables,
// eight jobs, the columns the 0.2.89 parse is pinned to
func TestParseListERPit(t *testing.T) {
	data, err := os.ReadFile("testdata/act-list-erpit.txt")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := ParseList(string(data))
	if err != nil {
		t.Fatal(err)
	}
	want := []ListedJob{
		{0, "plan", "what to run", "suite", "suite.yml", []string{"push", "workflow_dispatch", "pull_request"}},
		{0, "structural", "structural", "suite", "suite.yml", []string{"pull_request", "push", "workflow_dispatch"}},
		{1, "suite", "suite (fake ~zod)", "suite", "suite.yml", []string{"pull_request", "push", "workflow_dispatch"}},
		{0, "pins", "pins agree with suite.yml", "fixtures", "fixtures.yml", []string{"pull_request", "workflow_dispatch", "push"}},
		{0, "plan", "what to run", "fixtures", "fixtures.yml", []string{"workflow_dispatch", "push", "pull_request"}},
		{1, "replay", "replay fixture (fake ~zod)", "fixtures", "fixtures.yml", []string{"workflow_dispatch", "push", "pull_request"}},
		{1, "erasure", "erasure fixture (fake ~wes)", "fixtures", "fixtures.yml", []string{"push", "pull_request", "workflow_dispatch"}},
		{1, "duo", "duo fixture (fake ~zod and ~nec)", "fixtures", "fixtures.yml", []string{"push", "pull_request", "workflow_dispatch"}},
	}
	if len(jobs) != len(want) {
		t.Fatalf("got %d jobs, want %d: %+v", len(jobs), len(want), jobs)
	}
	for i := range want {
		if jobs[i].Stage != want[i].Stage || jobs[i].JobID != want[i].JobID || jobs[i].JobName != want[i].JobName ||
			jobs[i].WorkflowName != want[i].WorkflowName || jobs[i].WorkflowFile != want[i].WorkflowFile ||
			strings.Join(jobs[i].Events, ",") != strings.Join(want[i].Events, ",") {
			t.Errorf("row %d: got %+v, want %+v", i, jobs[i], want[i])
		}
	}
}

// the docker-host banner act prints on the same stream when stderr is
// merged is not a row; a header with other columns is a pinned-parse error
func TestParseListSkipsBannerAndRefusesOtherHeaders(t *testing.T) {
	out := "time=\"2026-09-13T12:38:46-05:00\" level=info msg=\"Using docker host 'unix:///var/run/docker.sock'\"\n" +
		"Stage  Job ID  Job name  Workflow name  Workflow file  Events\n" +
		"0      a       a         fixture-chain  fixture-chain.yml  push  \n"
	jobs, err := ParseList(out)
	if err != nil || len(jobs) != 1 || jobs[0].JobID != "a" {
		t.Fatalf("got %+v, %v", jobs, err)
	}
	if _, err := ParseList("Stage  Job ID  Job name  Workflow file  Events\n0  a  a  f.yml  push\n"); err == nil {
		t.Fatal("a header with five columns must be refused")
	}
	if _, err := ParseList("Stage  Job ID  Job name  Workflow name  Workflow file  Events\n-1  a  a  w  f.yml  push\n"); err == nil {
		t.Fatal("a negative stage must be refused")
	}
	if _, err := ParseList("nothing here\n"); err == nil {
		t.Fatal("no table must be an error")
	}
}
