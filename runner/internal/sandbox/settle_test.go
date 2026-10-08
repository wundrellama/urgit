package sandbox

// A reserve the launcher refuses is settled before it is taken for nothing
// reserved (runner/launcher/INTEGRATION.md §11.10; settled-admission ruling
// 01): the refusal reserved nothing on its delivery, but it is kept nowhere
// — a replayed or delayed copy of the request could still be admitted — so
// the Microvm settles the request before its caller may return the slot:
// closed, the refusal stands; a copy admitted meanwhile, rolled back by its
// identity; no conclusive answer, the attempt is retained by its request.
// Against the scripted launcher on a private unix socket.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"urgit/runner/internal/launcher"
)

func TestARefusedReserveIsSettledBeforeNothingIsReserved(t *testing.T) {
	const refusal = "launcher: over budget: guests 4+1/4"
	for _, c := range []struct {
		name   string
		settle func(req launcher.Request) (launcher.Reply, bool)
	}{
		{"its request closed", func(launcher.Request) (launcher.Reply, bool) {
			return launcher.Reply{OK: true, Settled: launcher.SettledClosed, SettledWhy: "never admitted; closed by this settlement"}, false
		}},
		{"a copy of it admitted meanwhile", func(req launcher.Request) (launcher.Reply, bool) {
			return launcher.Reply{OK: true, Settled: launcher.SettledAdmitted, Record: &launcher.Record{ID: "t-y", Incarnation: scriptedToken, Attempt: "0v1.att", CID: 8, Created: 5, State: launcher.StatePreparing, Request: req.Request}}, false
		}},
		{"its settlement unanswered", func(launcher.Request) (launcher.Reply, bool) { return launcher.Reply{}, true }},
		{"its settlement refused", func(launcher.Request) (launcher.Reply, bool) {
			return launcher.Reply{Error: "launcher: unsafe state: the inventory is not authoritative"}, false
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, s := scriptedMicrovm(t, func(req launcher.Request) (launcher.Reply, bool) {
				switch req.Op {
				case "reserve":
					return launcher.Reply{Error: refusal}, false
				case "settle":
					return c.settle(req)
				case "destroy":
					return launcher.Reply{OK: true}, false
				}
				return launcher.Reply{Error: "unexpected " + req.Op}, false
			})
			_, err := m.Prepare(context.Background(), vmSpec)
			var kept *RetainedError
			settles, destroys := s.requests("settle"), s.requests("destroy")
			switch c.name {
			case "its request closed":
				if len(settles) != 1 {
					t.Fatalf("A REFUSED REQUEST WAS NOT SETTLED: %d settle(s); %v", len(settles), err)
				}
				if err == nil || errors.As(err, &kept) || len(destroys) != 0 || !strings.Contains(err.Error(), refusal) || !strings.Contains(err.Error(), "never admitted") {
					t.Fatalf("a refused request, closed: the refusal stands: %v (destroys %+v)", err, destroys)
				}
			case "a copy of it admitted meanwhile":
				if len(destroys) != 1 || destroys[0].ID != "t-y" || destroys[0].Incarnation != scriptedToken || err == nil || errors.As(err, &kept) {
					t.Fatalf("A REFUSED REQUEST'S ADMITTED COPY WAS NOT ROLLED BACK: destroys %+v, %v", destroys, err)
				}
			default:
				if !errors.As(err, &kept) || kept.Handle.VM != "" || kept.Handle.Attempt != "0v1.att" || kept.Handle.ID != "ci-0v1.att" || kept.Handle.Request == "" || len(destroys) != 0 {
					t.Fatalf("A REFUSED REQUEST WAS TAKEN FOR NOTHING RESERVED BEFORE IT WAS SETTLED: %v (destroys %+v)", err, destroys)
				}
			}
			if len(settles) != 1 || settles[0].Attempt != "0v1.att" || settles[0].Request == "" || settles[0].Request != s.requests("reserve")[0].Request {
				t.Fatalf("THE SETTLEMENT DID NOT NAME THE REFUSED REQUEST: settles %+v", settles)
			}
			if n := len(s.requests("list")); n != 0 {
				t.Fatalf("A LIST WAS TAKEN FOR A SETTLEMENT: %d list request(s)", n)
			}
		})
	}
}
