// Package ship is the daemon's only channel to %urgit-ci: enroll, the
// long-poll, the event relay, plan, result, abandon, and the attempt read
// used to reconcile orphans. The bearer travels as `x-ci-bearer`; the
// ship refuses `Authorization: Bearer` at Eyre before any agent sees it.
package ship

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrUnauthorized is the ship answering 401 to a bearer it no longer
// knows: the daemon's enrollment is gone.
var ErrUnauthorized = errors.New("ship answered 401: enrollment lost")

// ErrNotOurs is the ship's 401 for an attempt that belongs to another
// daemon (`attempt authentication required`): this daemon's bearer is
// fine, the attempt is simply not its own. Reconcile leaves such a
// sandbox alone (P3 D6 g); nothing reads it as enrollment lost.
var ErrNotOurs = errors.New("ship answered 401: not this daemon's attempt")

// ErrRevoked is the ship answering 401 because the operator revoked this
// daemon (P3 D2): the daemon logs it and exits; only a fresh enrollment
// with a new token brings it back.
var ErrRevoked = errors.New("ship answered 401: revoked by the ship")

type Client struct {
	Base   string
	Bearer string
	HTTP   *http.Client
	// Capacity is reported on every poll (`x-ci-capacity`) so a restart
	// with a new config reaches the ship without re-enrolling.
	Capacity int
	// Labels ride every request the same way (`x-ci-labels`, comma
	// separated): the daemon's declared labels (CI-P3-SCHED-A).
	Labels []string
}

func New(base, bearer string) *Client {
	return &Client{
		Base:   strings.TrimRight(base, "/"),
		Bearer: bearer,
		// the long-poll window is 25 s on the ship; leave room
		HTTP: &http.Client{Timeout: 90 * time.Second},
	}
}

// Grant is a credential released to one attempt (D4): the value the
// daemon hands act as a secret, the expiry after which it must not, and
// the ship's signature over the grant (D5).
type Grant struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Expiry int64  `json:"expiry"` // unix seconds
	Nonce  string `json:"nonce"`
	Sig    string `json:"sig"`
}

// Assignment is the ship's assignment object as delivered on the channel.
type Assignment struct {
	ID              string                       `json:"id"`
	Attempt         string                       `json:"attempt"`
	Candidate       string                       `json:"candidate"`
	Repo            string                       `json:"repo"`
	Ref             string                       `json:"ref"`
	OID             string                       `json:"oid"`
	Head            string                       `json:"head"`
	Base            string                       `json:"base"`
	Trust           string                       `json:"trust"`
	ScratchRef      string                       `json:"scratch-ref"`
	Kind            string                       `json:"kind"`
	Workflow        string                       `json:"workflow"`
	Job             string                       `json:"job"`
	PrereqOutputs   map[string]map[string]string `json:"prereq-outputs"`
	DeadlineSeconds int                          `json:"deadline-seconds"`
	Assigned        string                       `json:"assigned"`
	Grants          []Grant                      `json:"grants"`
	Sig             *Signature                   `json:"sig"`
}

// Signature is the ship's authorization over an assignment (D5): the
// fields it signed and the signature, hex of the little-endian bytes.
type Signature struct {
	Recipient string `json:"recipient"`
	Attempt   string `json:"attempt"`
	Operation string `json:"operation"`
	Expiry    int64  `json:"expiry"`
	Nonce     string `json:"nonce"`
	Sig       string `json:"sig"`
}

type Response struct {
	Status int
	Body   []byte
}

func (r Response) Error() string {
	msg := struct {
		Error string `json:"error"`
	}{}
	_ = json.Unmarshal(r.Body, &msg)
	if msg.Error != "" {
		return fmt.Sprintf("%d %s", r.Status, msg.Error)
	}
	return fmt.Sprintf("%d %s", r.Status, strings.TrimSpace(string(r.Body)))
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/apps/urgit/api/ci"+path, reader)
	if err != nil {
		return Response{}, err
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if c.Bearer != "" {
		req.Header.Set("x-ci-bearer", c.Bearer)
	}
	if c.Capacity > 0 {
		req.Header.Set("x-ci-capacity", strconv.Itoa(c.Capacity))
	}
	if len(c.Labels) > 0 {
		req.Header.Set("x-ci-labels", strings.Join(c.Labels, ","))
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, err
	}
	return Response{Status: resp.StatusCode, Body: data}, nil
}

// Enrollment is what the ship hands a daemon once: its id, the bearer,
// and the CI public key the daemon pins (D5).
type Enrollment struct {
	DaemonID    string `json:"daemon-id"`
	Bearer      string `json:"bearer"`
	CIPublicKey string `json:"ci-public-key"`
}

// Enroll consumes the token. The caller forgets the token afterwards.
// The labels are declared here and again on every poll.
func (c *Client) Enroll(ctx context.Context, token string, capacity int, sandbox string, labels []string) (*Enrollment, error) {
	if labels == nil {
		labels = []string{}
	}
	body, _ := json.Marshal(map[string]any{"token": token, "capacity": capacity, "sandbox": sandbox, "labels": labels})
	resp, err := c.do(ctx, http.MethodPost, "/daemon/enroll", body)
	if err != nil {
		return nil, err
	}
	if resp.Status != http.StatusOK {
		return nil, fmt.Errorf("enroll: %s", resp.Error())
	}
	var answer Enrollment
	if err := json.Unmarshal(resp.Body, &answer); err != nil {
		return nil, fmt.Errorf("enroll: %w", err)
	}
	if answer.DaemonID == "" || answer.Bearer == "" {
		return nil, errors.New("enroll: ship answered without daemon-id and bearer")
	}
	return &answer, nil
}

// Poll holds the channel open; nil, nil means the window closed with
// nothing assigned (204).
func (c *Client) Poll(ctx context.Context, daemonID string) (*Assignment, error) {
	resp, err := c.do(ctx, http.MethodGet, "/daemon/"+daemonID+"/assignment", nil)
	if err != nil {
		return nil, err
	}
	switch resp.Status {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusUnauthorized:
		if strings.Contains(resp.Error(), "revoked") {
			return nil, ErrRevoked
		}
		return nil, ErrUnauthorized
	case http.StatusOK:
		var answer struct {
			Assignment Assignment `json:"assignment"`
		}
		if err := json.Unmarshal(resp.Body, &answer); err != nil {
			return nil, fmt.Errorf("poll: %w", err)
		}
		if answer.Assignment.Attempt == "" {
			return nil, errors.New("poll: assignment without an attempt")
		}
		return &answer.Assignment, nil
	default:
		return nil, fmt.Errorf("poll: %s", resp.Error())
	}
}

// Event relays one act --json line as-is. The status is the ship's
// answer; 202 accepted, anything else refused (409 on the projection
// tripwire, 429 at the event cap).
func (c *Client) Event(ctx context.Context, attempt string, line []byte) (Response, error) {
	return c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/event", line)
}

// ObjectRef names an uploaded object by size and sha-256; the ship
// derives the key from the attempt, never from the daemon.
type ObjectRef struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Result claims the jobResult the stream carried, naming the uploaded
// log when there is one (D1).
func (c *Client) Result(ctx context.Context, attempt, jobResult string, logRef *ObjectRef) (Response, error) {
	body, _ := json.Marshal(struct {
		JobResult string     `json:"job-result"`
		Log       *ObjectRef `json:"log,omitempty"`
	}{jobResult, logRef})
	return c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/result", body)
}

// Upload is the signed PUT the ship hands back for one of an attempt's
// objects (D2): the URL and the headers that authorize it.
type Upload struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Key     string            `json:"key"`
	Headers map[string]string `json:"headers"`
}

// RequestUpload asks the ship for a signed PUT of one object under the
// attempt's own trust class. The ship refuses names outside its fence.
func (c *Client) RequestUpload(ctx context.Context, attempt, name, contentType, sha256hex string, size int64) (*Upload, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "contentType": contentType, "sha256": sha256hex, "size": size})
	resp, err := c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/upload", body)
	if err != nil {
		return nil, err
	}
	if resp.Status != http.StatusOK {
		return nil, fmt.Errorf("upload: %s", resp.Error())
	}
	var up Upload
	if err := json.Unmarshal(resp.Body, &up); err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	if up.URL == "" {
		return nil, errors.New("upload: ship answered without a url")
	}
	return &up, nil
}

// Put sends the file to the store with the ship's headers, exactly as
// signed; the store's answer is the daemon's only evidence of success.
func (c *Client) Put(ctx context.Context, up *Upload, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, up.URL, f)
	if err != nil {
		return err
	}
	req.ContentLength = size
	for k, v := range up.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("store answered %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

// Abandon tells the ship no result is coming and why (D8).
func (c *Client) Abandon(ctx context.Context, attempt, reason string) (Response, error) {
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/abandon", body)
}

// Plan posts the plan (or the reason none could be produced) for a plan
// attempt. 200 stored; 422 the ship refused it with the reason.
func (c *Client) Plan(ctx context.Context, attempt string, plan any) (Response, error) {
	body, err := json.Marshal(plan)
	if err != nil {
		return Response{}, err
	}
	return c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/plan", body)
}

// AttemptStatus reads an attempt's status for orphan reconciliation:
// "" with found=false when the ship does not know it.
func (c *Client) AttemptStatus(ctx context.Context, attempt string) (status string, found bool, err error) {
	resp, err := c.do(ctx, http.MethodGet, "/attempt/"+attempt, nil)
	if err != nil {
		return "", false, err
	}
	switch resp.Status {
	case http.StatusNotFound:
		return "", false, nil
	case http.StatusUnauthorized:
		// the ship's foreign-attempt answer is not enrollment loss: the
		// bearer authenticated, the attempt is another daemon's
		if strings.Contains(resp.Error(), "attempt authentication required") {
			return "", false, ErrNotOurs
		}
		return "", false, ErrUnauthorized
	case http.StatusOK:
		var answer struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(resp.Body, &answer); err != nil {
			return "", false, err
		}
		return answer.Status, true, nil
	default:
		return "", false, fmt.Errorf("attempt %s: %s", attempt, resp.Error())
	}
}
