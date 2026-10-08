package launchertest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

// Proxy stands between a runner and a launcher's socket, on a fresh private
// unix socket of its own (relative to the test's working directory): each
// connection it accepts gets a connection of its own to the launcher, and
// every request line and its reply pass through — except the next request
// of an op a rule names. Its answer can be lost on the way back (DropReply:
// the launcher acts; the runner's connection closes unanswered). Its
// delivery can be held (Hold: not delivered yet; the runner's connection
// closes; Deliver sends it later on the launcher connection it was bound
// for — a request delayed in flight). Or it can be lost (Lose: never
// delivered). A connect's descriptor is not carried: a test through the
// proxy never connects (INTEGRATION.md §11.10's lost and delayed requests).
type Proxy struct {
	Socket string
	target string
	l      net.Listener
	mu     sync.Mutex
	rules  map[string][]string // op -> what befalls its next requests, in order
	held   []heldRequest
	seen   []launcher.Request // every request that reached the proxy, in order
}

type heldRequest struct {
	line []byte
	up   net.Conn
	upr  *bufio.Reader
}

// NewProxy serves a proxy to target, a launcher's socket.
func NewProxy(t testing.TB, target string) *Proxy {
	t.Helper()
	p := &Proxy{Socket: fmt.Sprintf("p%d.sock", time.Now().UnixNano()%1000000), target: target, rules: map[string][]string{}}
	l, err := net.Listen("unix", p.Socket)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	p.l = l
	go p.serve()
	t.Cleanup(func() {
		l.Close()
		p.mu.Lock()
		for _, h := range p.held {
			h.up.Close()
		}
		p.held = nil
		p.mu.Unlock()
	})
	return p
}

// DropReply loses the answer to the next request of op, after the
// launcher has acted on it.
func (p *Proxy) DropReply(op string) { p.rule(op, "drop-reply") }

// Hold keeps the next request of op from the launcher until Deliver.
func (p *Proxy) Hold(op string) { p.rule(op, "hold") }

// Lose never delivers the next request of op.
func (p *Proxy) Lose(op string) { p.rule(op, "lose") }

func (p *Proxy) rule(op, action string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules[op] = append(p.rules[op], action)
}

func (p *Proxy) next(op string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.rules[op]
	if len(a) == 0 {
		return ""
	}
	p.rules[op] = a[1:]
	return a[0]
}

// Deliver sends every held request to the launcher, each on the launcher
// connection it was bound for, and returns the launcher's answers — which
// reach no runner.
func (p *Proxy) Deliver() ([]launcher.Reply, error) {
	p.mu.Lock()
	held := p.held
	p.held = nil
	p.mu.Unlock()
	var out []launcher.Reply
	for _, h := range held {
		if _, err := h.up.Write(h.line); err != nil {
			h.up.Close()
			return out, err
		}
		line, err := h.upr.ReadBytes('\n')
		h.up.Close()
		if err != nil {
			return out, err
		}
		var r launcher.Reply
		if err := json.Unmarshal(line, &r); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Seen is the op of every request that reached the proxy, in order.
func (p *Proxy) Seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, r := range p.seen {
		out = append(out, r.Op)
	}
	return out
}

// Requests is every request of op that reached the proxy, in order, as it
// was sent: what a replay of one would send again.
func (p *Proxy) Requests(op string) []launcher.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []launcher.Request
	for _, r := range p.seen {
		if r.Op == op {
			out = append(out, r)
		}
	}
	return out
}

func (p *Proxy) serve() {
	for {
		c, err := p.l.Accept()
		if err != nil {
			return
		}
		go p.pass(c)
	}
}

func (p *Proxy) pass(c net.Conn) {
	up, err := net.Dial("unix", p.target)
	if err != nil {
		c.Close()
		return
	}
	cr, ur := bufio.NewReader(c), bufio.NewReader(up)
	for {
		line, err := cr.ReadBytes('\n')
		if err != nil {
			c.Close()
			up.Close()
			return
		}
		var req launcher.Request
		_ = json.Unmarshal(line, &req)
		p.mu.Lock()
		p.seen = append(p.seen, req)
		p.mu.Unlock()
		switch p.next(req.Op) {
		case "lose":
			c.Close()
			up.Close()
			return
		case "hold":
			p.mu.Lock()
			p.held = append(p.held, heldRequest{line: line, up: up, upr: ur})
			p.mu.Unlock()
			c.Close()
			return
		case "drop-reply":
			// the launcher acts; its answer goes nowhere
			if _, err := up.Write(line); err == nil {
				_, _ = ur.ReadBytes('\n')
			}
			c.Close()
			up.Close()
			return
		}
		if _, err := up.Write(line); err != nil {
			c.Close()
			up.Close()
			return
		}
		reply, err := ur.ReadBytes('\n')
		if err != nil {
			c.Close()
			up.Close()
			return
		}
		if _, err := c.Write(reply); err != nil {
			c.Close()
			up.Close()
			return
		}
	}
}
