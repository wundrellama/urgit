package provenance

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

// Connection policy and deadlines are real. DNS/dial are deterministic test
// seams: these tests never contact the public addresses used as identities.
func TestAutomaticFallbackSharesOriginalConnectBudget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		parentLimit time.Duration
		totalLimit  time.Duration
	}{
		{"default-cap", 0, 30 * time.Second},
		{"shorter-parent", 400 * time.Millisecond, 400 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := inputTLS(t)
			p.addresses = func(string) []netip.Addr {
				return []netip.Addr{netip.MustParseAddr("8.8.4.4"), netip.MustParseAddr("8.8.8.8")}
			}
			var attempts []string
			var budgets []time.Duration
			var deadlines []time.Time
			failure := errors.New("fixture connection refused")
			p.boundary.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				if network != "tcp" {
					t.Fatalf("unexpected network %q", network)
				}
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("dial has no shared connection deadline")
				}
				attempts = append(attempts, address)
				budgets = append(budgets, time.Until(deadline))
				deadlines = append(deadlines, deadline)
				return nil, failure
			}
			f := automaticFetcher(t, p, "https://inputs.example/tool")
			f.Timeout = time.Minute // exercise the connection cap, not a shorter download cap
			client, err := f.automaticClient("https://inputs.example/tool")
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.parentLimit != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.parentLimit)
				defer cancel()
			}
			start := time.Now()
			_, err = client.Transport.(*http.Transport).DialContext(ctx, "tcp", "inputs.example:443")
			if !errors.Is(err, failure) {
				t.Fatalf("lost final connection error: %v", err)
			}
			if len(attempts) != 2 || attempts[0] != "8.8.4.4:443" || attempts[1] != "8.8.8.8:443" {
				t.Fatalf("literal fallback attempts %v", attempts)
			}
			if budgets[0] <= 0 || budgets[0] > tc.totalLimit/2+20*time.Millisecond {
				t.Fatalf("first address consumed fallback budget: %v", budgets)
			}
			for _, deadline := range deadlines {
				if deadline.After(start.Add(tc.totalLimit + 20*time.Millisecond)) {
					t.Fatalf("connection deadline renewed: %v", deadlines)
				}
			}
		})
	}
}

func TestAutomaticFallbackRecoversAfterFirstAddressTimeout(t *testing.T) {
	p := inputTLS(t)
	p.addresses = func(string) []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("8.8.4.4"), netip.MustParseAddr("8.8.8.8")}
	}
	reachable := p.boundary.dial
	calls := 0
	p.boundary.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		calls++
		if address == "8.8.4.4:443" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return reachable(ctx, network, address)
	}
	f := automaticFetcher(t, p, "https://inputs.example/tool")
	f.Timeout = 2 * time.Second
	got, err := f.Download("https://inputs.example/tool")
	if err != nil || calls != 2 || got.Size != 15 || len(f.acquisitions) != 1 {
		t.Fatalf("first timeout consumed fallback opportunity: calls=%d result=%+v err=%v", calls, got, err)
	}
}

func TestAutomaticDownloadCancellationReachesDetachedDial(t *testing.T) {
	p := inputTLS(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	p.boundary.dial = func(dialCtx context.Context, _, _ string) (net.Conn, error) {
		cancel()
		select {
		case <-dialCtx.Done():
			finished <- dialCtx.Err()
			return nil, dialCtx.Err()
		case <-time.After(time.Second):
			err := errors.New("original download cancellation did not reach detached dial")
			finished <- err
			return nil, err
		}
	}
	f := automaticFetcher(t, p, "https://inputs.example/tool")
	f.Context = ctx
	_, err := f.Download("https://inputs.example/tool")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("download cancellation: %v", err)
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(p.requests) != 0 || len(f.acquisitions) != 0 {
		t.Fatal("cancelled acquisition produced effects")
	}
}

func TestAutomaticFallbackStopsOnParentCancellation(t *testing.T) {
	p := inputTLS(t)
	p.addresses = func(string) []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("8.8.4.4"), netip.MustParseAddr("8.8.8.8")}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	p.boundary.dial = func(context.Context, string, string) (net.Conn, error) {
		calls++
		cancel()
		return nil, errors.New("fixture first-address failure")
	}
	f := automaticFetcher(t, p, "https://inputs.example/tool")
	client, err := f.automaticClient("https://inputs.example/tool")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Transport.(*http.Transport).DialContext(ctx, "tcp", "inputs.example:443")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled connection continued or lost cancellation: calls=%d err=%v", calls, err)
	}
}
