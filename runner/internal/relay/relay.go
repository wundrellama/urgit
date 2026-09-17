// Package relay forwards act's --json stream to the ship one line at a
// time, exactly as the P0 harness did by hand: a line that is not a JSON
// object with `job` and `jobID` (the docker-host banner, act's own
// warnings) is dropped, never sent; the jobResult the stream carries is
// remembered so the daemon can claim it, and only it.
package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// Sender posts one line and answers with the ship's HTTP status.
type Sender func(ctx context.Context, line []byte) (status int, body []byte, err error)

// Summary is what the relay learned from the stream.
type Summary struct {
	Lines     int    // JSON event lines sent
	Dropped   int    // lines that were not event lines
	Accepted  int    // 202s
	Refused   int    // any other status
	LastError string // the last refusal's body
	JobResult string // the jobResult the stream carried, if any
}

type eventLine struct {
	Job       string `json:"job"`
	JobID     string `json:"jobID"`
	JobResult string `json:"jobResult"`
}

// Relay reads lines until EOF. A send error (the ship unreachable) ends
// the relay early with the error; refusals do not.
func Relay(ctx context.Context, stream io.Reader, send Sender, log func(string)) (Summary, error) {
	var s Summary
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var ev eventLine
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &ev) != nil || ev.Job == "" || ev.JobID == "" {
			s.Dropped++
			if log != nil {
				log("dropped non-event line: " + truncate(string(line), 160))
			}
			continue
		}
		copied := append([]byte(nil), line...)
		status, body, err := send(ctx, copied)
		if err != nil {
			return s, err
		}
		s.Lines++
		if status == 202 {
			s.Accepted++
			if ev.JobResult != "" {
				s.JobResult = ev.JobResult
			}
		} else {
			s.Refused++
			s.LastError = truncate(string(body), 300)
			if log != nil {
				log("ship refused event: " + s.LastError)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return s, err
	}
	return s, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Mask replaces a released credential value in act's output (D4).
const Mask = "***"

// Scrub returns a reader over the stream with every occurrence of every
// value replaced by Mask, line by line, so neither the relay nor the
// saved stream ever carries a released credential value. act masks its
// own output; this is the daemon's fence, applied before the tee.
func Scrub(stream io.Reader, values []string) io.Reader {
	var live []string
	for _, v := range values {
		if v != "" {
			live = append(live, v)
		}
	}
	if len(live) == 0 {
		return stream
	}
	pr, pw := io.Pipe()
	go func() {
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			for _, v := range live {
				line = strings.ReplaceAll(line, v, Mask)
			}
			if _, err := io.WriteString(pw, line+"\n"); err != nil {
				return
			}
		}
		pw.CloseWithError(scanner.Err())
	}()
	return pr
}
