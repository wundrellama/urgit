package main

// Named destinations (CI-P4-NET-1, named destinations, 2026-10-08;
// specs/ci-execution-contract.md §2b). A destination may name its host:
// `tcp:archive.ubuntu.com:80`. The launcher resolves each granted name on the
// host when it creates the VM's network, refuses a name that resolves to
// anything but public IPv4 addresses, allows exactly those addresses (at the
// entry's proto and port) in the VM's forward chain, and starts a responder
// in the VM's own network namespace that answers the granted names from that
// fixed table and refuses every other query (internal/pindns). Nothing is
// forwarded, so DNS carries nothing out of the VM, and a name never gets an
// input-hook exception (D1): host services stay reachable only by literal.
//
// The responder lives in this process. It is stopped before the namespace is
// deleted (its sockets would otherwise keep the namespace, and its veth and
// TAP, alive). A launcher restart ends it, and a VM still running then fails
// its lookups closed: refused, never answered wider.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"urgit/runner/internal/netname"
	"urgit/runner/internal/pindns"
)

// lookupBound bounds one name's resolution on the host.
const lookupBound = 5 * time.Second

// namedDestination reads `proto:name:port` where name is a DNS name in the
// shared grammar (internal/netname) and port is plain decimal 1-65535. ok
// is false for anything else, an IP literal included.
func namedDestination(d string) (proto, name, port string, ok bool) {
	proto, rest, found := strings.Cut(d, ":")
	if !found || (proto != "tcp" && proto != "udp") {
		return "", "", "", false
	}
	// SplitHostPort strips brackets; a name has one spelling, unbracketed
	name, port, err := net.SplitHostPort(rest)
	if err != nil || strings.HasPrefix(rest, "[") || net.ParseIP(name) != nil || !netname.Valid(name) {
		return "", "", "", false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return "", "", "", false
	}
	return proto, name, port, true
}

// unpinnable is every IPv4 range a name may never be pinned to: private,
// shared (CGNAT), loopback, link-local, documentation, benchmarking,
// multicast and reserved space. A public name that answers one of these is
// refused, so DNS can never turn a grant into a path to the LAN, the host or
// another VM (rebinding). A LAN service is named by its literal address.
var unpinnable = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// pinnable says whether a resolved address may be pinned: public IPv4.
func pinnable(a netip.Addr) bool {
	a = a.Unmap()
	if !a.Is4() {
		return false
	}
	for _, p := range unpinnable {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// pin resolves one granted name on the host and answers its addresses,
// sorted and without duplicates, or refuses: no IPv4 answer, more than
// pindns.MaxAddrs, or any answer that is not pinnable. Every answer must be
// public — one private answer refuses the name, it is never silently dropped.
func (h *realHost) pin(name string) ([]netip.Addr, error) {
	if h.lookupIP == nil {
		return nil, errors.New("no resolver is configured")
	}
	ctx, cancel := context.WithTimeout(h.context(), lookupBound)
	defer cancel()
	got, err := h.lookupIP(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolving %s on the host: %v", name, err)
	}
	own, err := h.hostAddrs()
	if err != nil {
		return nil, fmt.Errorf("the host's own addresses, which a name may not pin: %v", err)
	}
	var addrs []netip.Addr
	for _, a := range got {
		a = a.Unmap()
		if !pinnable(a) {
			return nil, fmt.Errorf("%s resolves to %s, which is not a public IPv4 address; a LAN or host service is named by its literal address", name, a)
		}
		// a public address of the host itself: the input hook would drop
		// it anyway (D1), but the job is told why instead of timing out
		if slices.Contains(own, a) {
			return nil, fmt.Errorf("%s resolves to %s, an address of this host; a host service is named by its literal address", name, a)
		}
		if !slices.Contains(addrs, a) {
			addrs = append(addrs, a)
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%s resolves to no IPv4 address", name)
	}
	if len(addrs) > pindns.MaxAddrs {
		return nil, fmt.Errorf("%s resolves to %d addresses, more than %d", name, len(addrs), pindns.MaxAddrs)
	}
	slices.SortFunc(addrs, func(a, b netip.Addr) int { return a.Compare(b) })
	return addrs, nil
}

// hostAddrs is every address on the host's own interfaces (a test puts a
// model in ownAddrs).
func (h *realHost) hostAddrs() ([]netip.Addr, error) {
	if h.ownAddrs != nil {
		return h.ownAddrs()
	}
	ifaddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, ia := range ifaddrs {
		if p, err := netip.ParsePrefix(ia.String()); err == nil {
			out = append(out, p.Addr().Unmap())
		}
	}
	return out, nil
}

// hostLookup is the production resolver: the host's own (systemd-resolved
// here), IPv4 only.
func hostLookup(ctx context.Context, name string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip4", name)
}

// dnsRegistry holds the running responders by VM id. Every bounded view of
// the host shares it (Bound copies the realHost, not the registry).
type dnsRegistry struct {
	mu      sync.Mutex
	running map[string]io.Closer
}

func newDNSRegistry() *dnsRegistry { return &dnsRegistry{running: map[string]io.Closer{}} }

func (r *dnsRegistry) put(id string, c io.Closer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[id] = c
}

// stop closes and forgets id's responder; none running is no error.
func (r *dnsRegistry) stop(id string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	c, ok := r.running[id]
	delete(r.running, id)
	r.mu.Unlock()
	if !ok {
		return nil
	}
	if err := c.Close(); err != nil {
		return fmt.Errorf("stopping the name responder of %s: %v", id, err)
	}
	return nil
}

// startPinnedDNS opens the responder's datagram and stream sockets inside
// the network namespace at nsPath, on listen (the VM's gateway, port 53),
// and serves table on them.
func startPinnedDNS(nsPath, listen string, table pindns.Table) (io.Closer, error) {
	var pc net.PacketConn
	var ln net.Listener
	err := inNetns(nsPath, func() error {
		var err error
		if pc, err = net.ListenPacket("udp4", listen); err != nil {
			return err
		}
		if ln, err = net.Listen("tcp4", listen); err != nil {
			pc.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("the name responder's sockets in %s: %v", nsPath, err)
	}
	s, err := pindns.Serve(table, pc, ln)
	if err != nil {
		pc.Close()
		ln.Close()
		return nil, err
	}
	return s, nil
}

// inNetns runs fn on an OS thread that has joined the network namespace at
// path; a socket fn creates belongs to that namespace for its life. The
// thread is returned to the scheduler only once it is proven back in its own
// namespace; otherwise it stays locked and is discarded when its goroutine
// ends, so no other goroutine ever runs in the VM's namespace.
func inNetns(path string, fn func() error) error {
	target, err := os.Open(path)
	if err != nil {
		return err
	}
	defer target.Close()
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		own, err := os.Open("/proc/thread-self/ns/net")
		if err != nil {
			runtime.UnlockOSThread()
			done <- err
			return
		}
		defer own.Close()
		ownIno, err := fdInode(own)
		if err != nil {
			runtime.UnlockOSThread()
			done <- err
			return
		}
		if err := setns(int(target.Fd())); err != nil {
			runtime.UnlockOSThread() // still in its own namespace
			done <- fmt.Errorf("setns %s: %w", path, err)
			return
		}
		ferr := fn()
		if err := setns(int(own.Fd())); err != nil {
			done <- errors.Join(ferr, fmt.Errorf("returning to the launcher's namespace: %w", err))
			return // the thread stays locked and is discarded
		}
		if ino, err := pathInode("/proc/thread-self/ns/net"); err != nil || ino != ownIno {
			done <- errors.Join(ferr, fmt.Errorf("the thread is not back in the launcher's namespace (%d, %v)", ino, err))
			return // the thread stays locked and is discarded
		}
		runtime.UnlockOSThread()
		done <- ferr
	}()
	return <-done
}

func setns(fd int) error {
	if _, _, e := syscall.RawSyscall(sysSetns, uintptr(fd), uintptr(syscall.CLONE_NEWNET), 0); e != 0 {
		return e
	}
	return nil
}

func fdInode(f *os.File) (uint64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return st.Sys().(*syscall.Stat_t).Ino, nil
}

func pathInode(p string) (uint64, error) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return st.Sys().(*syscall.Stat_t).Ino, nil
}
