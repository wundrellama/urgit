package ship

// The recovery channel's client (legacy-recovery UI ruling 01; runner/
// launcher/INTEGRATION.md §11.12): the poll answers an assignment, a
// recovery command, or neither, and never both; the ship hands a command
// over only on a poll that says the daemon carries them out; the report
// and the answers carry the daemon's bearer; a ship without the channel is
// told apart. The ship is an in-process handler: no listener.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type inprocess struct{ h http.Handler }

func (p inprocess) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body == nil {
		r.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	p.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func clientOf(h http.HandlerFunc) *Client {
	c := New("http://ship.test", "0vbearer")
	c.HTTP = &http.Client{Transport: inprocess{h}}
	return c
}

func TestPollWorkAnswersOneKindOfWork(t *testing.T) {
	var sawRecovery []string
	answer := ""
	c := clientOf(func(w http.ResponseWriter, r *http.Request) {
		sawRecovery = append(sawRecovery, r.Header.Get("x-ci-recovery"))
		if answer == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(answer))
	})
	if a, cmd, err := c.PollWork(context.Background(), "0vd"); a != nil || cmd != nil || err != nil {
		t.Fatalf("an empty window is neither: %v %v %v", a, cmd, err)
	}
	c.Recovery = true
	answer = `{"recovery":{"version":1,"id":"0v5.cmd","recipient":"0vd","operation":"release-legacy","selection":"s/1","revision":1,"evidence":"e","expiry":9,"nonce":"0v6","sig":"aa"}}`
	a, cmd, err := c.PollWork(context.Background(), "0vd")
	if err != nil || a != nil || cmd == nil || cmd.ID != "0v5.cmd" || cmd.Revision != 1 || cmd.Operation != "release-legacy" {
		t.Fatalf("A RECOVERY COMMAND WAS NOT TAKEN FROM THE POLL: %v %+v %v", a, cmd, err)
	}
	if sawRecovery[0] != "" || sawRecovery[1] != "1" {
		t.Fatalf("THE POLL DID NOT SAY WHETHER THIS DAEMON CARRIES RECOVERY COMMANDS OUT: %q", sawRecovery)
	}
	answer = `{"assignment":{"id":"0v1","attempt":"0v2"}}`
	if a, cmd, err := c.PollWork(context.Background(), "0vd"); err != nil || cmd != nil || a == nil || a.Attempt != "0v2" {
		t.Fatalf("an assignment was not taken: %v %v %v", a, cmd, err)
	}
	for _, both := range []string{
		`{"assignment":{"id":"0v1","attempt":"0v2"},"recovery":{"id":"0v5"}}`,
		`{"recovery":{"version":1}}`,
		`{}`,
	} {
		answer = both
		if a, cmd, err := c.PollWork(context.Background(), "0vd"); err == nil || a != nil || cmd != nil {
			t.Fatalf("A MALFORMED POLL ANSWER WAS TAKEN FOR WORK: %s -> %v %v", both, a, cmd)
		}
	}
}

func TestReportAndAnswerCarryTheBearer(t *testing.T) {
	var got []string
	status := http.StatusNoContent
	body := ""
	c := clientOf(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+r.Header.Get("x-ci-bearer")+" "+string(b))
		w.WriteHeader(status)
		w.Write([]byte(body))
	})
	report := RetentionReport{Version: 1, Retentions: []RetentionEntry{{Selection: "s/1", Revision: 1, Kind: "legacy", Eligible: true, Evidence: "e"}}}
	if err := c.PostRetentions(context.Background(), "0vd", report); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got[0], "POST /apps/urgit/api/ci/daemon/0vd/retentions 0vbearer ") {
		t.Fatalf("THE REPORT DID NOT CARRY THE DAEMON'S BEARER TO ITS ROUTE: %s", got[0])
	}
	var sent RetentionReport
	if err := json.Unmarshal([]byte(strings.SplitN(got[0], " ", 4)[3]), &sent); err != nil || len(sent.Retentions) != 1 || sent.Retentions[0].Evidence != "e" {
		t.Fatalf("the report's body: %v %+v", err, sent)
	}
	status = http.StatusOK
	found, err := c.PostRecoveryAnswer(context.Background(), "0vd", RecoveryAnswer{Command: "0v5", Status: AnswerCompleted})
	if err != nil || !found || !strings.HasPrefix(got[1], "POST /apps/urgit/api/ci/daemon/0vd/recovery 0vbearer ") {
		t.Fatalf("THE ANSWER DID NOT CARRY THE DAEMON'S BEARER TO ITS ROUTE: %v %v %s", found, err, got[1])
	}
	// the ship holds no such command: the answer has nowhere to go
	status, body = http.StatusNotFound, `{"error":"no such recovery command"}`
	if found, err := c.PostRecoveryAnswer(context.Background(), "0vd", RecoveryAnswer{Command: "0v9"}); err != nil || found {
		t.Fatalf("an unknown command's answer: %v %v", found, err)
	}
	// a ship without the channel, and one that no longer knows the bearer
	status, body = http.StatusNotFound, `{"error":"ci route not found"}`
	if err := c.PostRetentions(context.Background(), "0vd", report); !errors.Is(err, ErrNoRecovery) {
		t.Fatalf("a ship without the channel: %v", err)
	}
	status, body = http.StatusUnauthorized, `{"error":"daemon authentication required"}`
	if err := c.PostRetentions(context.Background(), "0vd", report); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an unknown bearer: %v", err)
	}
	if _, err := c.PostRecoveryAnswer(context.Background(), "0vd", RecoveryAnswer{Command: "0v5"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an unknown bearer's answer: %v", err)
	}
}
