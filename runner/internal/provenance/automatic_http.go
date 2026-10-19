package provenance

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"urgit/runner/internal/inputpolicy"
)

// These are test seams for owned, in-memory TLS peers. Production always uses
// the system resolver and net.Dialer, and never a default/ambient HTTP client.
type automaticConnection struct {
	lookup func(context.Context, string, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
	tls    *tls.Config
}

// Reject special-use destinations, including globally-unicast special ranges.
// IPv4 mapped IPv6, transition/tunnel and NAT64 forms are deliberately refused.
var nonpublic = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicAddress(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Is4In6() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range nonpublic {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func (g *GitFetcher) automaticAllowed(raw string) error {
	if g.Purpose != AutomaticTag {
		return fmt.Errorf("missing authenticated automatic resolver purpose")
	}
	if _, err := g.AutomaticPolicy.Scope(); err != nil {
		return err
	}
	if len(g.AutomaticPolicy.Sources) == 0 {
		return fmt.Errorf("automatic tag resolution has no external-input policy grant for %s", raw)
	}
	if err := inputpolicy.Validate(g.AutomaticCeiling); err != nil {
		return fmt.Errorf("runner automatic input ceiling: %w", err)
	}
	if _, err := inputpolicy.URL(raw); err != nil {
		return err
	}
	if !inputpolicy.Allows(g.AutomaticPolicy.Sources, raw) {
		return fmt.Errorf("automatic tag resolution has no external-input policy grant for %s", raw)
	}
	if !inputpolicy.Allows(g.AutomaticCeiling, raw) {
		return fmt.Errorf("runner owner automatic input ceiling refuses %s", raw)
	}
	return nil
}

func (g *GitFetcher) automaticClient(raw string) (*http.Client, error) {
	if err := g.automaticAllowed(raw); err != nil {
		return nil, err
	}
	boundary := g.automaticConnection
	lookup := net.DefaultResolver.LookupNetIP
	dial := (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	var tlsConfig *tls.Config
	if boundary != nil {
		if boundary.lookup != nil {
			lookup = boundary.lookup
		}
		if boundary.dial != nil {
			dial = boundary.dial
		}
		tlsConfig = boundary.tls
	}
	// net/http deliberately detaches its dial context from request deadlines
	// and cancellation. Capture this download's original bound explicitly;
	// redirects and address fallback must not renew it.
	parent := g.Context
	if parent == nil {
		parent = context.Background()
	}
	downloadDeadline := time.Now().Add(g.timeout())
	if deadline, ok := parent.Deadline(); ok && deadline.Before(downloadDeadline) {
		downloadDeadline = deadline
	}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 30 * time.Second, ResponseHeaderTimeout: 30 * time.Second, TLSClientConfig: tlsConfig}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		requestCtx, cancelRequest := context.WithDeadline(ctx, downloadDeadline)
		defer cancelRequest()
		stop := context.AfterFunc(parent, cancelRequest)
		defer stop()
		if err := parent.Err(); err != nil {
			return nil, err
		}
		if err := requestCtx.Err(); err != nil {
			return nil, err
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, fmt.Errorf("automatic input connection requires HTTPS port 443")
		}
		ips, err := lookup(requestCtx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("automatic input DNS returned no addresses for %s", host)
		}
		// Validate the complete answer before the first dial. A mixed answer fails.
		for _, ip := range ips {
			if !publicAddress(ip) {
				return nil, fmt.Errorf("automatic input DNS for %s contains nonpublic address %s", host, ip)
			}
		}
		// Keep one connection budget, bounded by the original request, and
		// share its remaining time so a dead address cannot consume every
		// fallback's opportunity. Only already-validated literals reach dial.
		connectCtx, cancel := context.WithTimeout(requestCtx, 30*time.Second)
		defer cancel()
		deadline, _ := connectCtx.Deadline()
		var lastErr error
		for i, ip := range ips {
			if err := connectCtx.Err(); err != nil {
				return nil, err
			}
			budget := time.Until(deadline) / time.Duration(len(ips)-i)
			attemptCtx, cancelAttempt := context.WithTimeout(connectCtx, budget)
			conn, err := dial(attemptCtx, "tcp", net.JoinHostPort(ip.String(), port))
			cancelAttempt()
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if err := connectCtx.Err(); err != nil {
			return nil, err
		}
		return nil, lastErr
	}
	return &http.Client{Transport: tr, Timeout: g.timeout(), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("automatic input redirect limit exceeded")
		}
		if err := g.automaticAllowed(req.URL.String()); err != nil {
			return fmt.Errorf("automatic input redirect refused: %w", err)
		}
		req.Header = make(http.Header) // no ambient/cross-origin credentials or cookies
		return nil
	}}, nil
}

// Acquisition is measured only after a complete successful download. Redirect
// targets are checked individually; the final target and exact bytes are retained.
type Acquisition struct {
	URL         string `json:"url"`
	ResolvedURL string `json:"resolvedUrl"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}
type InputEvidence struct {
	Policy       string        `json:"policy"`
	PolicyID     string        `json:"policyId"`
	Ceiling      string        `json:"ceiling"`
	CeilingID    string        `json:"ceilingId"`
	Acquisitions []Acquisition `json:"acquisitions"`
}

func (g *GitFetcher) InputEvidence() (InputEvidence, error) {
	policy, err := g.AutomaticPolicy.Scope()
	if err != nil {
		return InputEvidence{}, err
	}
	ceiling, err := inputpolicy.Scope(g.AutomaticCeiling)
	if err != nil {
		return InputEvidence{}, err
	}
	a := append([]Acquisition{}, g.acquisitions...)
	return InputEvidence{policy, inputpolicy.Identity(policy), ceiling, inputpolicy.Identity(ceiling), a}, nil
}
