package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

// inproc is an http.RoundTripper that serves each request in process, by
// the handler registered for its URL host: no listener, no socket. The
// ship and the object store of these tests are such handlers.
type inproc map[string]http.Handler

func (m inproc) RoundTrip(r *http.Request) (*http.Response, error) {
	h, ok := m[r.URL.Host]
	if !ok {
		return nil, fmt.Errorf("no in-process handler for host %q", r.URL.Host)
	}
	// a server hands its handlers a non-nil body; so does this one
	if r.Body == nil {
		r.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// shipURL and storeURL are the in-process ship's and store's origins.
const (
	shipURL  = "http://ship.test"
	storeURL = "http://store.test"
)
