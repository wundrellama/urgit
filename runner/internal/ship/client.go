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
	"strconv"
	"strings"
	"time"
)

// ErrUnauthorized is the ship answering 401 to a bearer it no longer
// knows: the daemon's enrollment is gone.
var ErrUnauthorized = errors.New("ship answered 401: enrollment lost")

type Client struct {
	Base   string
	Bearer string
	HTTP   *http.Client
	// Capacity is reported on every poll (`x-ci-capacity`) so a restart
	// with a new config reaches the ship without re-enrolling.
	Capacity int
}

func New(base, bearer string) *Client {
	return &Client{
		Base:   strings.TrimRight(base, "/"),
		Bearer: bearer,
		// the long-poll window is 25 s on the ship; leave room
		HTTP: &http.Client{Timeout: 90 * time.Second},
	}
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
	Kind            string                       `json:"kind"`
	Workflow        string                       `json:"workflow"`
	Job             string                       `json:"job"`
	PrereqOutputs   map[string]map[string]string `json:"prereq-outputs"`
	DeadlineSeconds int                          `json:"deadline-seconds"`
	Assigned        string                       `json:"assigned"`
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

// Enroll consumes the token and returns the daemon id and bearer. The
// caller forgets the token afterwards.
func (c *Client) Enroll(ctx context.Context, token string, capacity int, sandbox string) (daemonID, bearer string, err error) {
	body, _ := json.Marshal(map[string]any{"token": token, "capacity": capacity, "sandbox": sandbox})
	resp, err := c.do(ctx, http.MethodPost, "/daemon/enroll", body)
	if err != nil {
		return "", "", err
	}
	if resp.Status != http.StatusOK {
		return "", "", fmt.Errorf("enroll: %s", resp.Error())
	}
	var answer struct {
		DaemonID string `json:"daemon-id"`
		Bearer   string `json:"bearer"`
	}
	if err := json.Unmarshal(resp.Body, &answer); err != nil {
		return "", "", fmt.Errorf("enroll: %w", err)
	}
	if answer.DaemonID == "" || answer.Bearer == "" {
		return "", "", errors.New("enroll: ship answered without daemon-id and bearer")
	}
	return answer.DaemonID, answer.Bearer, nil
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

// Result claims the jobResult the stream carried.
func (c *Client) Result(ctx context.Context, attempt, jobResult string) (Response, error) {
	body, _ := json.Marshal(map[string]string{"job-result": jobResult})
	return c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/result", body)
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
