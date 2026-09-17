package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/ship"
)

func TestGrantedSecretIsScrubbedBeforeFileRelayAndDiagnostics(t *testing.T) {
	secret := "test-quote\"backslash\\credential"
	encoded, _ := json.Marshal(secret)
	line, _ := json.Marshal(map[string]any{
		"job": "fixture-chain/b", "jobID": "b", "time": "t",
		"command": "set-output", "name": "token", "arg": secret,
		"msg": "***", "extra": []string{secret},
	})
	box := &fakeBox{stream: "diagnostic " + secret + "\nescaped diagnostic " + string(encoded) + "\n" + string(line) + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	sh := &fakeShip{}
	srv := httptest.NewServer(sh.handler(t))
	defer srv.Close()
	d := newTestDaemon(t, box, srv, 1)
	var diagnostics bytes.Buffer
	d.log = log.New(&diagnostics, "", 0)
	a := *jobAssignment
	a.Grants = []ship.Grant{{Name: "TOKEN", Value: secret, Expiry: time.Now().Add(time.Hour).Unix()}}
	d.handle(context.Background(), &a)
	local, err := os.ReadFile(filepath.Join(d.cfg.WorkDir, a.Attempt+".act.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"local jsonl": string(local), "uploaded jsonl": string(sh.uploaded),
		"relay": strings.Join(sh.events, "\n"), "diagnostics": diagnostics.String(),
	} {
		if strings.Contains(body, secret) || strings.Contains(body, string(encoded[1:len(encoded)-1])) {
			t.Fatalf("credential leaked to %s", name)
		}
		if !strings.Contains(body, "***") {
			t.Fatalf("%s contains no masked value", name)
		}
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(sh.events[0]), &event); err != nil || event["arg"] != "***" {
		t.Fatalf("set-output was not scrubbed: %v", err)
	}
	if !strings.Contains(strings.Join(box.ops, "\n"), "--secret TOKEN="+secret) {
		t.Fatal("authorized credential was not passed to act")
	}
	if len(sh.results) != 1 || len(sh.abandons) != 0 {
		t.Fatal("scrubbing changed the job outcome")
	}
}

func TestUntrustedAndExpiredGrantsNeverReachAct(t *testing.T) {
	for _, test := range []struct {
		name, trust string
		expiry      int64
	}{
		{"untrusted", "untrusted", time.Now().Add(time.Hour).Unix()},
		{"expired", "trusted", time.Now().Add(-time.Minute).Unix()},
	} {
		t.Run(test.name, func(t *testing.T) {
			sh := &fakeShip{}
			srv := httptest.NewServer(sh.handler(t))
			defer srv.Close()
			box := &fakeBox{}
			d := newTestDaemon(t, box, srv, 1)
			a := *jobAssignment
			a.Trust = test.trust
			a.Grants = []ship.Grant{{Name: "TOKEN", Value: "test-credential", Expiry: test.expiry}}
			d.handle(context.Background(), &a)
			for _, op := range box.ops {
				if strings.HasPrefix(op, "run ") {
					t.Fatal("refused grant reached act")
				}
			}
		})
	}
}

func TestGrantEnvironmentMetadata(t *testing.T) {
	for _, test := range []struct{ yaml, want string }{
		{"environment: production", "production"},
		{"environment: {name: staging, url: https://example.test}", "staging"},
		{"environment: '${{ inputs.target }}'", ""},
		{"environment: 42", ""},
		{"runs-on: ubuntu-latest", ""},
	} {
		data := []byte("jobs:\n  deploy:\n    " + test.yaml + "\n")
		if got := grantEnvironment(data, "deploy"); got != test.want {
			t.Fatalf("%s: got %q, want %q", test.yaml, got, test.want)
		}
		if grantEnvironment(data, "other") != "" {
			t.Fatal("environment metadata crossed job boundaries")
		}
	}
}
