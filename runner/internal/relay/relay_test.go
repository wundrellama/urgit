package relay

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// the P0-recorded act 0.2.89 streams: every line is sent as-is, the
// jobResult is what the stream said, and act's docker-host banner (the
// stderr line) is dropped, never sent
func TestRelayRecordedStreams(t *testing.T) {
	cases := []struct {
		file, jobResult string
		lines           int
	}{
		{"fixture-pass.jsonl", "success", 15},
		{"fixture-fail.jsonl", "failure", 26},
	}
	for _, c := range cases {
		data, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		banner, _ := os.ReadFile("testdata/banner.jsonl")
		stream := string(banner) + "not json at all\n" + string(data)
		var sent [][]byte
		summary, err := Relay(context.Background(), strings.NewReader(stream), func(_ context.Context, line []byte) (int, []byte, error) {
			sent = append(sent, line)
			return 202, nil, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Lines != c.lines || summary.Accepted != c.lines || summary.Dropped != 2 || summary.JobResult != c.jobResult {
			t.Errorf("%s: %+v", c.file, summary)
		}
		for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if string(sent[i]) != line {
				t.Errorf("%s line %d was not relayed verbatim", c.file, i)
			}
			var obj map[string]any
			if json.Unmarshal(sent[i], &obj) != nil || obj["job"] == nil || obj["jobID"] == nil {
				t.Errorf("%s line %d is not an event line", c.file, i)
			}
		}
	}
}

// a refusal (the projection tripwire's 409, the event cap's 429) does not
// stop the relay and a refused jobResult is never claimed
func TestRelayRefusals(t *testing.T) {
	stream := `{"job":"w/a","jobID":"a","jobResult":"success","time":"t","msg":"a"}` + "\n" +
		`{"job":"w/b","jobID":"b","time":"t","msg":"b"}` + "\n" +
		`{"job":"w/b","jobID":"b","jobResult":"success","time":"t","msg":"b done"}` + "\n"
	var logged []string
	summary, err := Relay(context.Background(), strings.NewReader(stream), func(_ context.Context, line []byte) (int, []byte, error) {
		if strings.Contains(string(line), `"jobID":"a"`) {
			return 409, []byte(`{"error":"event job does not match the assignment"}`), nil
		}
		return 202, nil, nil
	}, func(s string) { logged = append(logged, s) })
	if err != nil {
		t.Fatal(err)
	}
	if summary.Refused != 1 || summary.Accepted != 2 || summary.JobResult != "success" || !strings.Contains(summary.LastError, "does not match") {
		t.Fatalf("%+v", summary)
	}
	if len(logged) != 1 {
		t.Fatalf("expected one refusal log line, got %v", logged)
	}
	// a stream whose only jobResult is refused claims nothing
	only := `{"job":"w/a","jobID":"a","jobResult":"success","time":"t","msg":"a"}` + "\n"
	summary, _ = Relay(context.Background(), strings.NewReader(only), func(_ context.Context, line []byte) (int, []byte, error) {
		return 409, nil, nil
	}, nil)
	if summary.JobResult != "" {
		t.Fatalf("a refused jobResult must not be claimed: %+v", summary)
	}
}

// the scrub replaces every released value on every line before the tee,
// leaves other lines byte-identical, and is a no-op with no values
func TestScrub(t *testing.T) {
	in := "{\"msg\":\"token is s3cr3t and s3cr3t\"}\n{\"msg\":\"other\",\"arg\":\"hunter2\"}\n{\"msg\":\"clean\"}\n"
	out, err := io.ReadAll(Scrub(strings.NewReader(in), []string{"s3cr3t", "hunter2", ""}))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"msg\":\"token is *** and ***\"}\n{\"msg\":\"other\",\"arg\":\"***\"}\n{\"msg\":\"clean\"}\n"
	if string(out) != want {
		t.Fatalf("got %q", out)
	}
	same, _ := io.ReadAll(Scrub(strings.NewReader(in), nil))
	if string(same) != in {
		t.Fatalf("no values must mean no change: %q", same)
	}
}
