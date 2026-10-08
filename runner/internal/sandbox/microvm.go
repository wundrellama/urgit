package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/guest"
	"urgit/runner/internal/launcher"
)

// Microvm is the P4 backend: one Firecracker microVM per attempt,
// started through the administrator's privileged launcher (rider 01),
// which jails the VMM, owns the disk copy, the cgroup and any network,
// enforces the independent deadline and quarantines failed cleanups.
// The daemon never touches a VMM, a socket path or a device: it asks
// the launcher for a reservation, a boot and a connected vsock
// descriptor, then speaks the guest helper protocol over it.
//
// Every input the guest sees enters by bounded copy over that
// connection; the guest's own Docker daemon is what act binds into job
// containers; a locked guest has no NIC and act runs in offline mode
// against the action cache the bundle carries.
//
// Every launcher operation runs on a connection of its own, bounded by its
// context (runner/launcher/INTEGRATION.md §3): one attempt's long create
// never delays another's destroy, and a launcher restart is met by the
// next dial. A destroy always names the incarnation it means.
type Microvm struct {
	cfg      *config.Config
	socket   string
	imageDir string

	mu sync.Mutex
	// image is the image last verified, for this runner's own reports (Name,
	// ImageDigest): a Prepare boots the image its own verification returned,
	// never this (INTEGRATION.md §11.16)
	image    verifiedImage
	daemonID string
	sessions map[string]*guest.Session // by handle id
	vms      map[string]string         // handle id -> launcher vm id
	incs     map[string]string         // handle id -> its incarnation token
	cids     map[string]uint32
	created  map[string]int64
	labels   map[string]string // handle id -> the job's label
	bridges  map[string]string // handle id -> the guest's job bridge address (READY)
}

// rollbackBound bounds the wait for the answer to the destroy that undoes
// a failed Prepare: one cleanup allowance (rider 04's 120 s) from this
// request, which is the trigger of the launcher's own obligation — its
// teardown, the wait for the create it asks to roll back included, ends by
// then (runner/launcher/INTEGRATION.md §7.2). A client's wait, never later
// than the obligation: an answer not in by then leaves the reservation
// retained — charged, visible — never taken for released.
const rollbackBound = launcher.CleanupBound

// verifiedImage is one verification of the image directory: its manifest,
// and the manifest's sha256 — the image's identity for the launcher.
type verifiedImage struct {
	manifest ImageManifest
	digest   string
}

// ImageManifest is runner/guest/manifest.json as the recipe writes it.
type ImageManifest struct {
	Version int `json:"version"`
	Kernel  struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	} `json:"kernel"`
	Rootfs struct {
		Name        string `json:"name"`
		SHA256      string `json:"sha256"`
		BaselineMiB int    `json:"baseline_mib"`
	} `json:"rootfs"`
	ActImage struct {
		Reference string `json:"reference"`
		Digest    string `json:"digest"`
	} `json:"act_image"`
	Helper struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
	} `json:"helper"`
	Act struct {
		Version string `json:"version"`
	} `json:"act"`
	BootArgs string `json:"boot_args"`
}

// guest-side layout (contract §6)
const (
	guestWork    = "/work"
	guestSource  = "/work/src"
	guestActions = "/work/actions"
	guestCache   = "/work/cache"
	guestArtif   = "/work/artifacts"
	guestTools   = "/work/toolcache"
	guestAct     = "/usr/local/bin/act"
	guestDocker  = "unix:///run/docker.sock"
	guestNetwork = "urgit-ci"
)

// copy bounds (contract §4; the bundle is bounded, never open-ended)
var bundleLimits = guest.Limits{MaxBytes: 4 << 30, MaxEntries: 400000}

// NewMicrovm verifies the image directory against its manifest (kernel
// and rootfs digests, helper protocol version, act image reference)
// and that the launcher answers with a compatible protocol. Anything
// missing is ErrMicrovmUnavailable with the reason: no fallback.
func NewMicrovm(cfg *config.Config) (Sandbox, error) {
	m := &Microvm{cfg: cfg, socket: cfg.LauncherSocket, imageDir: cfg.ImagePath, sessions: map[string]*guest.Session{}, vms: map[string]string{}, incs: map[string]string{}, cids: map[string]uint32{}, created: map[string]int64{}, bridges: map[string]string{}, labels: map[string]string{}}
	if _, err := m.verified(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMicrovmUnavailable, err)
	}
	// the launcher must answer now: a runner with no launcher enrolls
	// nothing and runs nothing
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cl, err := launcher.Dial(ctx, m.socket, "preflight")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMicrovmUnavailable, err)
	}
	cl.Close()
	return m, nil
}

// verifyImage reads the manifest and hashes the kernel and the rootfs
// against it: the image as the directory holds it now, returned to its
// caller and written nowhere shared (INTEGRATION.md §11.16).
func (m *Microvm) verifyImage() (verifiedImage, error) {
	data, err := os.ReadFile(filepath.Join(m.imageDir, "manifest.json"))
	if err != nil {
		return verifiedImage{}, fmt.Errorf("image manifest: %w", err)
	}
	var mf ImageManifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return verifiedImage{}, fmt.Errorf("image manifest: %w", err)
	}
	if mf.Version != 1 || mf.Kernel.Name == "" || mf.Rootfs.Name == "" {
		return verifiedImage{}, errors.New("image manifest: not a version 1 guest image manifest")
	}
	sum := sha256.Sum256(data)
	for _, f := range []struct{ name, want string }{{mf.Kernel.Name, mf.Kernel.SHA256}, {mf.Rootfs.Name, mf.Rootfs.SHA256}} {
		got, err := fileSHA256(filepath.Join(m.imageDir, f.name))
		if err != nil {
			return verifiedImage{}, fmt.Errorf("image %s: %w", f.name, err)
		}
		if got != f.want {
			return verifiedImage{}, fmt.Errorf("image %s: sha256 %s does not match the manifest's %s; refusing to boot it", f.name, got, f.want)
		}
	}
	return verifiedImage{manifest: mf, digest: hex.EncodeToString(sum[:])}, nil
}

// verified is the image this runner may boot now: verifyImage's, its guest
// helper speaking this runner's protocol and preloading its act image — all
// that the start checks, checked at the start and before every Prepare
// (M10; INTEGRATION.md §11.16). The image last verified is kept, under mu,
// for the runner's own reports.
func (m *Microvm) verified() (verifiedImage, error) {
	img, err := m.verifyImage()
	if err != nil {
		return verifiedImage{}, err
	}
	if ref := img.manifest.ActImage.Reference; ref != m.cfg.ActImage && strings.TrimPrefix(ref, "docker.io/") != m.cfg.ActImage {
		return verifiedImage{}, fmt.Errorf("act_image %q is not the image the guest preloads (%s)", m.cfg.ActImage, ref)
	}
	if img.manifest.Helper.Version != guest.ProtocolVersion {
		return verifiedImage{}, fmt.Errorf("the guest helper speaks protocol %d, this runner %d", img.manifest.Helper.Version, guest.ProtocolVersion)
	}
	m.mu.Lock()
	m.image = img
	m.mu.Unlock()
	return img, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ImageDigest is the sha256 of the manifest last verified: the identity
// the launcher boots, for this runner's own reports. A Prepare reserves the
// image its own verification returned.
func (m *Microvm) ImageDigest() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.image.digest
}

func (m *Microvm) Name() string {
	digest := m.ImageDigest()
	if len(digest) > 12 {
		digest = digest[:12]
	}
	return fmt.Sprintf("microvm (Firecracker + jailer; one disposable VM per attempt via urgit-vm-launcher; image %s)", digest)
}

func (m *Microvm) Kind() string { return "microvm" }

// SetOwner names the daemon whose reservations these are — every one is
// owned by (uid, daemon id) from here on — and proves the launcher answers
// it.
func (m *Microvm) SetOwner(daemonID string) error {
	m.mu.Lock()
	m.daemonID = daemonID
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cl, err := m.dial(ctx)
	if err != nil {
		return err
	}
	return cl.Close()
}

// conn is a launcher connection of one operation's own, bound by its
// context until Close.
type conn struct {
	*launcher.Client
	unbind func()
}

func (c conn) Close() error {
	c.unbind()
	return c.Client.Close()
}

// dial opens a connection of the operation's own to the launcher, as this
// daemon, bound by ctx: a call that has not answered by ctx's end fails
// with its outcome unknown.
func (m *Microvm) dial(ctx context.Context) (conn, error) {
	m.mu.Lock()
	daemon := m.daemonID
	m.mu.Unlock()
	if daemon == "" {
		return conn{}, errors.New("microvm: no daemon identity yet (SetOwner)")
	}
	cl, err := launcher.Dial(ctx, m.socket, daemon)
	if err != nil {
		return conn{}, err
	}
	return conn{Client: cl, unbind: cl.Bind(ctx)}, nil
}

func (m *Microvm) HandleFor(id string) Handle {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Handle{ID: id, Attempt: strings.TrimPrefix(id, "ci-"), VM: m.vms[id], Incarnation: m.incs[id], CID: m.cids[id], Created: m.created[id], Address: m.bridges[id], Label: m.labels[id]}
}

// track and untrack keep a handle's launcher identity for HandleFor.
func (m *Microvm) track(h Handle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vms[h.ID], m.incs[h.ID], m.cids[h.ID], m.created[h.ID], m.labels[h.ID] = h.VM, h.Incarnation, h.CID, h.Created, h.Label
}

func (m *Microvm) untrack(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.vms, id)
	delete(m.incs, id)
	delete(m.cids, id)
	delete(m.created, id)
	delete(m.bridges, id)
	delete(m.labels, id)
}

func (m *Microvm) ExecEnv(h Handle) ExecEnv {
	return ExecEnv{
		WorkRoot: guestWork, SourceDir: guestSource, ActBinary: guestAct, CopyAct: false,
		DockerSocket: guestDocker, JobNetwork: guestNetwork,
		CachePath: guestCache, ArtifactPath: guestArtif, ActionCachePath: guestActions, ToolCache: guestTools,
		ServerAddr: h.Address,
		Env:        []string{"RUNNER_TOOL_CACHE=" + guestTools},
		Locked:     true,
	}
}

// Prepare reserves, boots and opens the helper session, on a launcher
// connection of its own bounded by ctx. Any failure after the reservation
// destroys exactly that incarnation; when that destroy fails — the
// launcher quarantines what it cannot remove — or cannot be answered, the
// error is a *RetainedError naming the reservation, which stays charged
// (runner/launcher/INTEGRATION.md §3). A spec the launcher refuses is the
// daemon's refusal reason, verbatim — once the refused request is settled
// (§11.10).
func (m *Microvm) Prepare(ctx context.Context, spec Spec) (Handle, error) {
	if spec.Attempt == "" {
		return Handle{}, errors.New("prepare: an attempt id is required")
	}
	// the image this Prepare boots is the one it verifies here, whatever
	// another Prepare verifies meanwhile (INTEGRATION.md §11.16)
	img, err := m.verified()
	if err != nil {
		return Handle{}, fmt.Errorf("%w: %v", ErrMicrovmUnavailable, err)
	}
	cl, err := m.dial(ctx)
	if err != nil {
		return Handle{}, err
	}
	defer cl.Close()
	profile := spec.Profile
	if profile == "" {
		profile = "locked"
	}
	dests := append([]string(nil), spec.Destinations...)
	sort.Strings(dests)
	deadline := spec.Deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(time.Hour)
	}
	// the request's token: the daemon's, recorded durably before this
	// Prepare (INTEGRATION.md §11.10); a caller that keeps none gets a fresh
	// one, with which only this Prepare can settle a lost answer
	request := spec.Request
	if request == "" {
		if request, err = launcher.NewRequest(); err != nil {
			return Handle{}, err
		}
	}
	h := Handle{ID: "ci-" + spec.Attempt, Attempt: spec.Attempt, Label: spec.Label, Request: request}
	rsv, err := cl.Reserve(launcher.ReserveRequest{
		Attempt: spec.Attempt, Image: img.digest, CPUs: spec.CPUs, MemoryMiB: spec.MemoryMiB, DiskMiB: spec.DiskMiB,
		DeadlineUnix: deadline.Unix(), Network: profile, Destinations: dests, Label: spec.Label, Request: request,
	})
	if err != nil {
		if kept, ok := launcher.Retained(err); ok {
			// not acknowledged, but charged: that exact reservation goes
			h.VM, h.Incarnation, h.CID, h.Created = kept.ID, kept.Incarnation, kept.CID, kept.Created
			return m.rollback(h, "reserve", err)
		}
		// refused, or its answer lost: settled by its request either way. A
		// refusal reserved nothing on this delivery, but it is kept nowhere —
		// a replayed or delayed copy of the request could still be admitted
		// — so it is not taken for nothing reserved until the request is
		// closed (INTEGRATION.md §11.10)
		return m.recoverReservation(h, err)
	}
	h.VM, h.Incarnation, h.CID, h.Created = rsv.ID, rsv.Incarnation, rsv.CID, rsv.Created
	m.track(h)
	fail := func(step string, cause error) (Handle, error) { return m.rollback(h, step, cause) }
	if _, err := cl.Create(rsv.Ref()); err != nil {
		return fail("boot", err)
	}
	// the helper listens once Docker answers; the launcher's connect
	// fails until then, so retry within a bounded window
	var sess *guest.Session
	var ready guest.Ready
	connectDeadline := time.Now().Add(90 * time.Second)
	for {
		fd, err := cl.Connect(rsv.Ref(), guest.HelperPort)
		if err == nil {
			sess = guest.NewSession(fd)
			hctx, hcancel := context.WithTimeout(ctx, 20*time.Second)
			ready, err = sess.Hello(hctx, guest.Hello{Attempt: spec.Attempt, Nonce: rsv.ID})
			hcancel()
			if err == nil {
				break
			}
			sess.Close()
			sess = nil
		}
		if time.Now().After(connectDeadline) || ctx.Err() != nil {
			return fail("guest helper", fmt.Errorf("no READY within 90 s: %v", err))
		}
		time.Sleep(500 * time.Millisecond)
	}
	if ready.Bridge == "" {
		sess.Close()
		return fail("guest helper", errors.New("READY names no job bridge address"))
	}
	h.Address = ready.Bridge
	m.mu.Lock()
	m.sessions[h.ID] = sess
	m.bridges[h.ID] = ready.Bridge
	m.mu.Unlock()
	return h, nil
}

// rollback destroys exactly the incarnation h names, on a fresh
// connection (the attempt's may be spent, its context over) within
// rollbackBound. Released, or already gone: the step's error. Otherwise
// what h names stays charged, and the error says so (*RetainedError).
func (m *Microvm) rollback(h Handle, step string, cause error) (Handle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackBound)
	defer cancel()
	if err := m.destroyExact(ctx, h); err != nil {
		m.track(h)
		return Handle{}, &RetainedError{Handle: h, Err: fmt.Errorf("%s: %v; and the reservation could not be released: %w", step, cause, err)}
	}
	m.untrack(h.ID)
	return Handle{}, fmt.Errorf("%s: %w", step, cause)
}

// recoverReservation settles a reserve whose answer was lost or refused, by
// the launcher's conclusive answer for exactly its request, on a fresh
// connection (INTEGRATION.md §11.10; settled-admission ruling 01):
// admitted — that reservation, rolled back by its incarnation; released or
// closed — nothing is left, and the request will never be admitted (a
// refusal's reason stays the error's). An answer that settles nothing — the
// launcher cannot be asked, or refuses the settlement — retains the attempt
// by its request, its outcome unsettled: an empty list is never taken for
// "nothing reserved", since the request may still be admitted after it.
func (m *Microvm) recoverReservation(h Handle, cause error) (Handle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := m.Settle(ctx, h)
	if err != nil {
		return Handle{}, &RetainedError{Handle: h, Err: fmt.Errorf("reserve: %v; its request %s is not settled: %w", cause, h.Request, err)}
	}
	switch s.Outcome {
	case launcher.SettledAdmitted:
		return m.rollback(s.Handle, "reserve", cause)
	case launcher.SettledReleased:
		return Handle{}, fmt.Errorf("reserve: %w (settled: the reservation it admitted has been released, on the launcher's evidence: %s)", cause, s.Detail)
	}
	return Handle{}, fmt.Errorf("reserve: %w (settled: the launcher never admitted request %s, and never will: %s)", cause, h.Request, s.Detail)
}

// Settle is the launcher's conclusive answer for h's reserve request, this
// daemon's, on a connection of its own (INTEGRATION.md §11.10): admitted —
// h with the exact identity of the reservation it admitted; released;
// closed. An error — no request, no launcher, a refusal — settles nothing.
func (m *Microvm) Settle(ctx context.Context, h Handle) (Settlement, error) {
	if h.Request == "" {
		return Settlement{}, errors.New("its reserve request is not known: it cannot be settled")
	}
	cl, err := m.dial(ctx)
	if err != nil {
		return Settlement{}, err
	}
	defer cl.Close()
	s, err := cl.Settle(h.Attempt, h.Request)
	if err != nil {
		return Settlement{}, err
	}
	out := Settlement{Outcome: s.Outcome, Detail: s.Why}
	switch s.Outcome {
	case launcher.SettledAdmitted:
		r := s.Record
		out.Handle = h
		out.Handle.VM, out.Handle.Incarnation, out.Handle.CID, out.Handle.Created = r.ID, r.Incarnation, r.CID, r.Created
		out.Detail = fmt.Sprintf("admitted as %s (%s)", r.Ref(), r.State)
	case launcher.SettledReleased:
		out.Detail = s.Release.By
		if s.Release.Late() {
			out.Detail += ", its accounting confirmed late"
		}
	}
	return out, nil
}

// destroyExact asks the launcher to destroy the incarnation h names; the
// launcher answers done when h's incarnation is gone already, whatever
// holds its id now (Service.DestroyOf).
func (m *Microvm) destroyExact(ctx context.Context, h Handle) error {
	if h.VM == "" {
		return errors.New("the launcher's identity of this reservation is unknown")
	}
	cl, err := m.dial(ctx)
	if err != nil {
		return err
	}
	defer cl.Close()
	_, err = cl.DestroyOf(launcher.Ref{ID: h.VM, Incarnation: h.Incarnation, CID: h.CID, Created: h.Created})
	return err
}

// ProveReleased asks the launcher for the durable evidence of the release
// of exactly the incarnation h names (INTEGRATION.md §11.8): nil only when
// it proves it — never on the record's absence alone.
func (m *Microvm) ProveReleased(ctx context.Context, h Handle) error {
	if h.VM == "" {
		return errors.New("the launcher's identity of this reservation is unknown")
	}
	cl, err := m.dial(ctx)
	if err != nil {
		return err
	}
	defer cl.Close()
	_, err = cl.Released(launcher.Ref{ID: h.VM, Incarnation: h.Incarnation, CID: h.CID, Created: h.Created})
	return err
}

// Inventory is what the launcher answers for a legacy release, on a
// connection of its own (INTEGRATION.md §11.12): hello — whose protocol the
// client has checked is this runner's — and this daemon's records, from a
// list the launcher answers only when its inventory is authoritative. It
// asks nothing else and changes nothing.
func (m *Microvm) Inventory(ctx context.Context) (Inventory, error) {
	cl, err := m.dial(ctx)
	if err != nil {
		return Inventory{}, err
	}
	defer cl.Close()
	recs, err := cl.List()
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{Socket: m.socket, Launcher: cl.Hello.Launcher, Protocol: cl.Hello.Protocol, Records: recs}, nil
}

// list is this daemon's launcher records, on a connection of its own.
func (m *Microvm) list(ctx context.Context) ([]launcher.Record, error) {
	cl, err := m.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	return cl.List()
}

func (m *Microvm) session(h Handle) (*guest.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[h.ID]
	if !ok {
		return nil, fmt.Errorf("no guest session for %s", h.ID)
	}
	return s, nil
}

// Copy streams a host directory (or file's parent) into the guest under
// /work; guestPath must be under it. The guest echoes the digest.
func (m *Microvm) Copy(ctx context.Context, h Handle, hostPath, guestPath string) error {
	s, err := m.session(h)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(guestPath, guestWork), "/")
	root := guestPath == guestWork || guestPath == guestWork+"/"
	clean := filepath.Clean(rel)
	if !root && (rel == "" || strings.HasPrefix(guestPath, "/") && !strings.HasPrefix(guestPath, guestWork+"/")) ||
		filepath.IsAbs(rel) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("copy: %s is not under %s", guestPath, guestWork)
	}
	src := strings.TrimSuffix(hostPath, "/.")
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		if root {
			return fmt.Errorf("copy: %s is not under %s", guestPath, guestWork)
		}
		// a single file: stage it under a directory named for the target
		dir, err := os.MkdirTemp(m.cfg.WorkDir, "copy-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(guestPath)), data, st.Mode().Perm()); err != nil {
			return err
		}
		src = dir
		rel = filepath.Dir(rel)
	}
	_, err = s.Put(ctx, src, rel, bundleLimits)
	return err
}

// Verify asks the guest to confirm a workspace digest (P07).
func (m *Microvm) Verify(ctx context.Context, h Handle, guestPath, sha256 string) (bool, error) {
	s, err := m.session(h)
	if err != nil {
		return false, err
	}
	return s.Verify(ctx, strings.TrimPrefix(strings.TrimPrefix(guestPath, guestWork), "/"), sha256)
}

func (m *Microvm) Run(ctx context.Context, h Handle, workDir string, argv []string, env []string) (io.ReadCloser, <-chan int, error) {
	s, err := m.session(h)
	if err != nil {
		return nil, nil, err
	}
	cwd := strings.TrimPrefix(strings.TrimPrefix(workDir, guestWork), "/")
	return s.Exec(ctx, argv, env, cwd)
}

func (m *Microvm) Signal(ctx context.Context, h Handle, processName, signal string) error {
	s, err := m.session(h)
	if err != nil {
		return err
	}
	return s.Signal(ctx, signal)
}

// Export streams a bounded archive of a guest directory to w (artifacts).
func (m *Microvm) Export(ctx context.Context, h Handle, guestPath string, max int64, w io.Writer) (guest.ExportEnd, error) {
	s, err := m.session(h)
	if err != nil {
		return guest.ExportEnd{}, err
	}
	return s.Export(ctx, strings.TrimPrefix(strings.TrimPrefix(guestPath, guestWork), "/"), max, w)
}

// Destroy ends the session and asks the launcher to tear the VM down —
// exactly the incarnation h names, never a later one of the attempt; the
// launcher's failure (a quarantine, a halted teardown, no answer within
// ctx) is the error the daemon sees.
func (m *Microvm) Destroy(ctx context.Context, h Handle) error {
	m.mu.Lock()
	s := m.sessions[h.ID]
	delete(m.sessions, h.ID)
	if h.VM == "" {
		h.VM, h.Incarnation, h.CID, h.Created = m.vms[h.ID], m.incs[h.ID], m.cids[h.ID], m.created[h.ID]
	}
	m.mu.Unlock()
	m.untrack(h.ID)
	if s != nil {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = s.Shutdown(sctx)
		cancel()
	}
	if h.VM == "" {
		return nil
	}
	return m.destroyExact(ctx, h)
}

// Orphans: the VMs the launcher still holds for this daemon, as
// `ci-<attempt>` ids the daemon asks the ship about; HandleFor then names
// each one's record and incarnation.
func (m *Microvm) Orphans(ctx context.Context) ([]string, error) {
	recs, err := m.list(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range recs {
		id := "ci-" + r.Attempt
		m.vms[id], m.incs[id], m.cids[id], m.created[id], m.labels[id] = r.ID, r.Incarnation, r.CID, r.Created, r.Label
		ids = append(ids, id)
	}
	return ids, nil
}
