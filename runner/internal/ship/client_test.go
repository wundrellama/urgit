package ship

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// the 401 split (P3 D6 g): the ship's foreign-attempt answer is not
// enrollment loss; a bearer the ship no longer knows is; a revoked
// daemon's poll is its own error
func TestUnauthorizedSplit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		switch r.URL.Path {
		case "/apps/urgit/api/ci/attempt/0v1":
			w.Write([]byte(`{"error":"attempt authentication required"}`))
		case "/apps/urgit/api/ci/attempt/0v2":
			w.Write([]byte(`{"error":"daemon authentication required"}`))
		case "/apps/urgit/api/ci/daemon/0vd/assignment":
			w.Write([]byte(`{"error":"revoked by the ship"}`))
		default:
			w.Write([]byte(`{"error":"daemon authentication required"}`))
		}
	}))
	defer server.Close()
	c := New(server.URL, "0vbearer")
	if _, _, err := c.AttemptStatus(context.Background(), "0v1"); !errors.Is(err, ErrNotOurs) {
		t.Errorf("foreign attempt: got %v, want ErrNotOurs", err)
	}
	if _, _, err := c.AttemptStatus(context.Background(), "0v2"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("unknown bearer: got %v, want ErrUnauthorized", err)
	}
	if _, err := c.Poll(context.Background(), "0vd"); !errors.Is(err, ErrRevoked) {
		t.Errorf("revoked poll: got %v, want ErrRevoked", err)
	}
	if _, err := c.Poll(context.Background(), "0vx"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("lost poll: got %v, want ErrUnauthorized", err)
	}
}
