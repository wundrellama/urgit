package provenance

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

// Independent probes use the submitted owned in-memory TLS peer. Only
// DNS answers, final dial and trusted fixture CA are injected; HTTP/TLS,
// allowlist, address checks, hashing, cache and context handling are real.
func TestIndependentTLSHostname(t *testing.T) {
	for _, host := range []string{"inputs.example", "not-in-certificate.example"} {
		t.Run(host, func(t *testing.T) {
			p := inputTLS(t)
			f := automaticFetcher(t, p, "https://"+host+"/tool")
			got, err := f.Download("https://" + host + "/tool")
			if host == "inputs.example" {
				if err != nil || got.Size != 15 {
					t.Fatalf("positive: %+v %v", got, err)
				}
			} else {
				if err == nil || len(p.requests) != 0 || len(f.acquisitions) != 0 {
					t.Fatalf("hostname bypass: %v requests=%v", err, p.requests)
				}
				t.Logf("wrong DNS hostname refused by real TLS: %v", err)
			}
		})
	}
}

func TestIndependentCancelledDownload(t *testing.T) {
	p := inputTLS(t)
	f := automaticFetcher(t, p, "https://inputs.example/tool")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Context = ctx
	_, err := f.Download("https://inputs.example/tool")
	if !errors.Is(err, context.Canceled) || len(f.acquisitions) != 0 || len(p.requests) != 0 {
		t.Fatalf("cancelled fetch: %v receipts=%v requests=%v", err, f.acquisitions, p.requests)
	}
	t.Logf("cancelled before HTTP, no acquisition receipt: %v", err)
}

func TestIndependentPublicAddressFallback(t *testing.T) {
	// Both addresses pass the actual public-address validator. The first
	// address is unreachable; the second reaches the same owned TLS peer.
	for _, badFirst := range []bool{false, true} {
		name := "reachable-first"
		if badFirst {
			name = "unreachable-first"
		}
		t.Run(name, func(t *testing.T) {
			p := inputTLS(t)
			good := netip.MustParseAddr("8.8.8.8")
			bad := netip.MustParseAddr("8.8.4.4")
			p.addresses = func(string) []netip.Addr {
				if badFirst {
					return []netip.Addr{bad, good}
				}
				return []netip.Addr{good, bad}
			}
			actualDial := p.boundary.dial
			var tried []string
			p.boundary.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				tried = append(tried, address)
				if strings.HasPrefix(address, "8.8.4.4:") {
					return nil, errors.New("owned fixture: first public address unreachable")
				}
				return actualDial(ctx, network, address)
			}
			f := automaticFetcher(t, p, "https://inputs.example/tool")
			got, err := f.Download("https://inputs.example/tool")
			t.Logf("tried=%v receipt_count=%d result=%+v error=%v", tried, len(f.acquisitions), got, err)
			if err != nil {
				t.Fatalf("supported HTTPS input with a reachable validated address failed: %v", err)
			}
		})
	}
}
