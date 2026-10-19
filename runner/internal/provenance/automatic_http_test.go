package provenance

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/inputpolicy"
)

// This is an owned in-memory TLS server, not a mocked RoundTripper. Production
// HTTP, redirects, DNS answer validation, literal dialing, TLS host verification,
// hashing and cache writes all execute. No listener or external network is used.
type inputPeer struct {
	mu        sync.Mutex
	requests  []string
	dials     []string
	lookups   []string
	addresses func(string) []netip.Addr
	response  func(*http.Request) *http.Response
	boundary  *automaticConnection
}

func inputTLS(t *testing.T) *inputPeer {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "input fixture"}, DNSNames: []string{"inputs.example", "cdn.example"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	peer := &inputPeer{addresses: func(string) []netip.Addr { return []netip.Addr{netip.MustParseAddr("8.8.8.8")} }, response: func(r *http.Request) *http.Response {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fixture bytes\x00\x00"))}
	}}
	peer.boundary = &automaticConnection{tls: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	peer.boundary.lookup = func(_ context.Context, network, host string) ([]netip.Addr, error) {
		peer.mu.Lock()
		defer peer.mu.Unlock()
		peer.lookups = append(peer.lookups, host)
		return peer.addresses(host), nil
	}
	peer.boundary.dial = func(_ context.Context, network, address string) (net.Conn, error) {
		peer.mu.Lock()
		peer.dials = append(peer.dials, address)
		peer.mu.Unlock()
		client, server := net.Pipe()
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		go func() {
			conn := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
			defer conn.Close()
			r, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				return
			}
			peer.mu.Lock()
			peer.requests = append(peer.requests, r.Host+r.URL.String())
			peer.mu.Unlock()
			if r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("ambient credentials reached fixture")
			}
			resp := peer.response(r)
			resp.Close = true
			_ = resp.Write(conn)
		}()
		return client, nil
	}
	return peer
}
func automaticFetcher(t *testing.T, peer *inputPeer, sources ...string) *GitFetcher {
	t.Helper()
	return &GitFetcher{Purpose: AutomaticTag, AutomaticPolicy: &inputpolicy.Policy{Version: 1, Sources: sources}, AutomaticCeiling: append([]string{}, sources...), Cache: filepath.Join(t.TempDir(), "cache"), automaticConnection: peer.boundary, Timeout: 5 * time.Second}
}
func TestAutomaticHTTPSFunctionalWalk(t *testing.T) {
	peer := inputTLS(t)
	f := automaticFetcher(t, peer, "https://inputs.example/tool", "https://cdn.example/tool")
	for _, v := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(v, "http://user:secret@127.0.0.1:9")
	}
	t.Setenv("NETRC", filepath.Join(t.TempDir(), "credentials"))
	peer.response = func(r *http.Request) *http.Response {
		if r.Host == "inputs.example" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://cdn.example/tool"}}, Body: io.NopCloser(strings.NewReader(""))}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fixture bytes\x00\x00"))}
	}
	root := checkout(t, map[string]string{".github/workflows/release.yml": "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -fL https://inputs.example/tool -o tool\n"})
	inv, err := WalkWith(root, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Nodes) != 1 || inv.Nodes[0].Refusal != "" {
		t.Fatalf("walk: %+v", inv)
	}
	sum := sha256.Sum256([]byte("fixture bytes\x00\x00"))
	n := inv.Nodes[0]
	if n.SHA256 != hex.EncodeToString(sum[:]) || n.Size != 15 {
		t.Fatalf("exact bytes: %+v", n)
	}
	data, err := os.ReadFile(f.DownloadPath(n.SHA256))
	if err != nil || string(data) != "fixture bytes\x00\x00" {
		t.Fatalf("cache: %q %v", data, err)
	}
	evidence, err := f.InputEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Acquisitions) != 1 || evidence.Acquisitions[0].ResolvedURL != "https://cdn.example/tool" || evidence.PolicyID != inputpolicy.Identity(evidence.Policy) || evidence.CeilingID != inputpolicy.Identity(evidence.Ceiling) {
		t.Fatalf("provenance %+v", evidence)
	}
	if len(peer.dials) != 2 || len(peer.lookups) != 2 {
		t.Fatalf("dial/lookup %v %v", peer.dials, peer.lookups)
	}
	for _, address := range peer.dials {
		if address != "8.8.8.8:443" {
			t.Fatalf("second DNS opportunity: %s", address)
		}
	}
}
func TestAutomaticHTTPSRefusesAtConnectionBoundary(t *testing.T) {
	for _, ips := range [][]string{{"127.0.0.1"}, {"10.0.0.1"}, {"100.64.0.1"}, {"169.254.169.254"}, {"192.0.0.1"}, {"192.0.2.1"}, {"224.0.0.1"}, {"240.0.0.1"}, {"::1"}, {"fc00::1"}, {"fe80::1"}, {"::ffff:8.8.8.8"}, {"::ffff:127.0.0.1"}, {"64:ff9b::808:808"}, {"2002:808:808::1"}, {"2001:db8::1"}, {"3fff::1"}, {"8.8.8.8", "192.168.1.1"}, {"2606:4700:4700::1111", "fd00::1"}, {}} {
		t.Run(fmt.Sprint(ips), func(t *testing.T) {
			peer := inputTLS(t)
			peer.addresses = func(string) []netip.Addr {
				var a []netip.Addr
				for _, s := range ips {
					a = append(a, netip.MustParseAddr(s))
				}
				return a
			}
			f := automaticFetcher(t, peer, "https://inputs.example/tool")
			_, err := f.Download("https://inputs.example/tool")
			if err == nil || len(peer.dials) != 0 || len(peer.requests) != 0 {
				t.Fatalf("effect before refusal: %v %+v", err, peer.dials)
			}
		})
	}
	if !publicAddress(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public IPv6 refused")
	}
}
func TestAutomaticRedirectAndDNSChange(t *testing.T) {
	for _, target := range []string{"http://inputs.example/tool", "https://evil.example/tool", "https://inputs.example/tool/child", "https://inputs.example/toolkit", "https://inputs.example.evil/tool", "https://user@inputs.example/tool", "https://[::ffff:127.0.0.1]/tool", "file:///etc/passwd", "https://inputs.example/tool?token=secret"} {
		t.Run(target, func(t *testing.T) {
			peer := inputTLS(t)
			peer.response = func(*http.Request) *http.Response {
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader(""))}
			}
			f := automaticFetcher(t, peer, "https://inputs.example/tool")
			if _, err := f.Download("https://inputs.example/tool"); err == nil || len(peer.dials) != 1 {
				t.Fatalf("redirect escaped: %v %v", err, peer.dials)
			}
		})
	}
	peer := inputTLS(t)
	calls := 0
	peer.addresses = func(string) []netip.Addr {
		calls++
		if calls == 1 {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}
	}
	peer.response = func(*http.Request) *http.Response {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://inputs.example/next"}}, Body: io.NopCloser(strings.NewReader(""))}
	}
	f := automaticFetcher(t, peer, "https://inputs.example/tool", "https://inputs.example/next")
	if _, err := f.Download("https://inputs.example/tool"); err == nil || calls != 2 || len(peer.dials) != 1 {
		t.Fatalf("DNS change escaped %v %d %v", err, calls, peer.dials)
	}
}
func TestAutomaticCeilingAndCacheNeverGrant(t *testing.T) {
	peer := inputTLS(t)
	f := automaticFetcher(t, peer, "https://inputs.example/tool")
	f.AutomaticCeiling = nil
	if _, err := f.Download("https://inputs.example/tool"); err == nil || !strings.Contains(err.Error(), "ceiling") {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Cache); !os.IsNotExist(err) {
		t.Fatalf("denied policy touched cache: %v", err)
	}
	f.AutomaticCeiling = []string{"https://inputs.example/tool"}
	if _, err := f.Download("https://inputs.example/tool"); err != nil {
		t.Fatal(err)
	}
	f.AutomaticPolicy.Sources = []string{}
	before := len(peer.dials)
	if _, err := f.Download("https://inputs.example/tool"); err == nil || len(peer.dials) != before {
		t.Fatalf("prior cache granted fetch: %v", err)
	}
}
func TestAutomaticUnsupportedHelpersNeverRun(t *testing.T) {
	peer := inputTLS(t)
	f := automaticFetcher(t, peer, "https://inputs.example/tool")
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	for _, name := range []string{"git", "skopeo"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf bad > '"+marker+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file:///tmp/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://")
	// Prime every cache path so a cache hit cannot bypass purpose/transport checks.
	dir := f.repoDir("https://inputs.example/tool")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "HEAD"), []byte("fixture"), 0600)
	f.images = map[string]Image{"sha256:fixture": {Digest: "sha256:fixture"}}
	_, a := f.Resolve("https://inputs.example/tool", "v1")
	_, b := f.ReadFile("https://inputs.example/tool", "abc", "action.yml")
	_, c := f.MirrorCommit("https://inputs.example/tool", "v1", "abc", "def")
	_, d := f.PushMirror("https://inputs.example/tool", "abc", "http://controller.example", "mirror", "token")
	_, e := f.Image("inputs.example/tool:latest")
	for _, err := range []error{a, b, c, d, e} {
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("unsupported path: %v", err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("ambient helper ran")
	}
	if len(peer.dials) != 0 {
		t.Fatal("unsupported transport connected")
	}
}
