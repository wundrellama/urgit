package ship

// Legacy-replay-upgrade ruling 01 (runner/launcher/INTEGRATION.md §11.15):
// a daemon that waits for its history transition advertises capacity 0 on
// its polls, explicitly, and its ship honours it; any other daemon
// advertises its capacity when it has one, and nothing otherwise, as
// before.

import (
	"context"
	"net/http"
	"testing"
)

func TestAPausedDaemonAdvertisesAnExplicitZero(t *testing.T) {
	for _, c := range []struct {
		capacity int
		paused   bool
		want     string
		sent     bool
	}{
		{2, false, "2", true},
		{0, false, "", false},
		{0, true, "0", true},
	} {
		var got string
		var sent bool
		cl := clientOf(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("x-ci-capacity")
			_, sent = r.Header[http.CanonicalHeaderKey("x-ci-capacity")]
			w.WriteHeader(http.StatusNoContent)
		})
		cl.Capacity, cl.Paused = c.capacity, c.paused
		if _, _, err := cl.PollWork(context.Background(), "0v1.daemon"); err != nil {
			t.Fatal(err)
		}
		if got != c.want || sent != c.sent {
			t.Fatalf("A POLL ADVERTISED THE WRONG CAPACITY (capacity %d, paused %v): %q, sent %v", c.capacity, c.paused, got, sent)
		}
	}
}
