package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// Wire: one JSON object per line, one reply per request, on a unix
// socket the launcher owns. A `connect` reply carries the vsock fd as
// SCM_RIGHTS ancillary data on the reply's own write, so only the
// connection that asked receives it.

type Request struct {
	Op           string   `json:"op"`
	Daemon       string   `json:"daemon,omitempty"`
	ID           string   `json:"id,omitempty"`
	Port         uint32   `json:"port,omitempty"`
	Attempt      string   `json:"attempt,omitempty"`
	Image        string   `json:"image,omitempty"`
	CPUs         int      `json:"cpus,omitempty"`
	MemoryMiB    int      `json:"memory_mib,omitempty"`
	DiskMiB      int      `json:"disk_mib,omitempty"`
	DeadlineUnix int64    `json:"deadline_unix,omitempty"`
	Network      string   `json:"network,omitempty"`
	Destinations []string `json:"destinations,omitempty"`
	// Label is the job, for the operator's selection only (reserve)
	Label string `json:"label,omitempty"`
	// Incarnation (with CID and Created) names the incarnation a create,
	// connect, stop or destroy means (INTEGRATION.md §11.1): its token; a
	// record written before tokens by its cid and creation time. A request
	// that names none is refused.
	Incarnation string `json:"incarnation,omitempty"`
	CID         uint32 `json:"cid,omitempty"`
	Created     int64  `json:"created,omitempty"`
	// Request is a reserve's request token, and what a settle names with
	// its Attempt (INTEGRATION.md §11.10)
	Request string `json:"request,omitempty"`
}

type Reply struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	Quarantined bool   `json:"quarantined,omitempty"`
	// Retained: the request failed and the reservation it made stays
	// charged (ErrRetained); ID and Incarnation (with CID and Created)
	// name it, as they name every reservation a reserve answers
	Retained    bool     `json:"retained,omitempty"`
	ID          string   `json:"id,omitempty"`
	Incarnation string   `json:"incarnation,omitempty"`
	CID         uint32   `json:"cid,omitempty"`
	Created     int64    `json:"created,omitempty"`
	PID         int      `json:"pid,omitempty"`
	Record      *Record  `json:"record,omitempty"`
	VMs         []Record `json:"vms,omitempty"`
	Launcher    string   `json:"launcher,omitempty"`
	Protocol    int      `json:"protocol,omitempty"`
	Budget      *Budget  `json:"budget,omitempty"`
	Ceiling     []string `json:"ceiling,omitempty"`
	// Release is the disposition of a release the launcher proves by its
	// durable evidence (INTEGRATION.md §11.8): the answer to released, and
	// to a destroy that is done; LateAccounting says its accounting was
	// confirmed at or after its obligation's deadline — or at an instant
	// never recorded — so it is never claimed on time
	Release        *Release `json:"release,omitempty"`
	LateAccounting bool     `json:"late_accounting,omitempty"`
	// Settled is a settle's outcome (INTEGRATION.md §11.10): admitted (its
	// Record), released (its Release) or closed (SettledWhy says how); a
	// reserve's answer echoes its Request
	Settled    string `json:"settled,omitempty"`
	SettledWhy string `json:"settled_why,omitempty"`
	// Pinned is a create's answer for a VM granted DNS names: each name
	// with the addresses it was pinned to (CI-P4-NET-1, pinned addresses
	// shown per run). Absent, the VM was granted no name: a launcher that
	// cannot pin names has no name in its ceiling, so it refuses a
	// reservation that grants one.
	Pinned  []Pin  `json:"pinned,omitempty"`
	Request string `json:"request,omitempty"`
}

// Budget is what hello reports: the launcher's own limits and use.
type Budget struct {
	CPUs, MemoryMiB, Guests             int
	UsedCPUs, UsedMemoryMiB, UsedGuests int
}

// WireProtocol is the launcher protocol version in hello; a client refuses a
// launcher of another (Dial). 2: every create, connect, stop and destroy
// names its incarnation (INTEGRATION.md §11.1), and one that names none is
// refused. 3: a destroy of an incarnation the launcher does not hold is
// done only on the durable evidence of its release, and released asks for
// that evidence (§11.8) — which a runner of protocol 2 would not ask, and
// whose absence it would take for a release. 4: every reserve names its
// request token, admitted at most once, ever, and settle answers for it
// (§11.10) — a runner of protocol 3 would take an empty list for its
// settlement. So each refuses the other's older protocol at hello.
const WireProtocol = 4

// ---- server --------------------------------------------------------------

// Server serves the core on a unix socket, authenticating every
// connection by peer uid against AllowedUIDs.
type Server struct {
	Service     *Service
	AllowedUIDs map[uint32]bool
	Version     string
	Log         *log.Logger
}

// Serve accepts connections until the listener closes.
func (sv *Server) Serve(l net.Listener) error {
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go sv.handle(c.(*net.UnixConn))
	}
}

func peerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var cerr error
	err = raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}
	return cred.Uid, nil
}

func (sv *Server) handle(c *net.UnixConn) {
	defer c.Close()
	uid, err := peerUID(c)
	if err != nil || !sv.AllowedUIDs[uid] {
		_ = writeReply(c, Reply{Error: fmt.Sprintf("peer uid %d is not an allowed runner uid", uid)}, nil)
		if sv.Log != nil {
			sv.Log.Printf("refused connection from uid %d", uid)
		}
		return
	}
	var owner *Owner
	reader := bufio.NewReaderSize(c, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = writeReply(c, Reply{Error: "bad request: " + err.Error()}, nil)
			return
		}
		if req.Op != "hello" && owner == nil {
			_ = writeReply(c, Reply{Error: "hello first"}, nil)
			continue
		}
		var reply Reply
		var fd *os.File
		switch req.Op {
		case "hello":
			if req.Daemon == "" {
				reply = Reply{Error: "hello needs a daemon id"}
				break
			}
			owner = &Owner{UID: uid, Daemon: req.Daemon}
			b1, b2, b3, u1, u2, u3 := sv.Service.Budget()
			reply = Reply{OK: true, Launcher: sv.Version, Protocol: WireProtocol, Budget: &Budget{b1, b2, b3, u1, u2, u3}, Ceiling: sv.Service.cfg.Ceiling}
		case "reserve":
			// every reserve on the wire names its request, so that its answer,
			// if lost, can be settled (INTEGRATION.md §11.10)
			if req.Request == "" {
				reply = Reply{Error: fmt.Sprintf("%v: reserve %s names no request token", ErrInvalid, req.Attempt)}
				break
			}
			r, err := sv.Service.Reserve(*owner, ReserveRequest{Attempt: req.Attempt, Image: req.Image, CPUs: req.CPUs, MemoryMiB: req.MemoryMiB, DiskMiB: req.DiskMiB, DeadlineUnix: req.DeadlineUnix, Network: req.Network, Destinations: req.Destinations, Label: req.Label, Request: req.Request})
			var kept *retainedError
			switch {
			case errors.As(err, &kept):
				// not acknowledged; what stays charged is named, for its
				// owner's exact destroy
				c := kept.charged
				reply = Reply{Error: err.Error(), Retained: true, ID: c.ID, Incarnation: c.Incarnation, CID: c.CID, Created: c.Created, Request: c.Request}
			case err != nil:
				reply = Reply{Error: err.Error()}
			default:
				reply = Reply{OK: true, ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created, Request: r.Request}
			}
		case "settle":
			st, err := sv.Service.Settle(*owner, req.Attempt, req.Request)
			if err != nil {
				reply = Reply{Error: err.Error()}
				break
			}
			reply = Reply{OK: true, Settled: st.Outcome, SettledWhy: st.Why, Record: st.Record, Release: st.Release}
			if st.Release != nil {
				reply.LateAccounting = st.Release.Late()
			}
		case "create", "connect", "stop", "destroy", "released":
			// a mutation acts on the incarnation it names and no other; one
			// that names none is refused — no bare-id route reaches whatever
			// the id names now (INTEGRATION.md §11.1)
			ref := Ref{ID: req.ID, Incarnation: req.Incarnation, CID: req.CID, Created: req.Created}
			if !ref.Named() {
				reply = Reply{Error: fmt.Sprintf("%v: %s %s names no incarnation (its token)", ErrInvalid, req.Op, req.ID)}
				break
			}
			reply, fd = sv.mutate(*owner, req.Op, ref, req.Port)
		case "inspect":
			rec, err := sv.Service.Inspect(*owner, req.ID)
			if err != nil {
				reply = Reply{Error: err.Error()}
			} else {
				reply = Reply{OK: true, Record: &rec}
			}
		case "list":
			// an absence from this list is what its consumers take for a
			// release: it is refused while the inventory is not
			// authoritative (INTEGRATION.md §§11.6, 11.7)
			vms, err := sv.Service.List(*owner)
			if err != nil {
				reply = Reply{Error: err.Error()}
				break
			}
			reply = Reply{OK: true, VMs: vms}
		default:
			reply = Reply{Error: "unknown op " + req.Op}
		}
		if sv.Log != nil {
			// a hello refused for lack of a daemon id leaves owner nil
			daemon := ""
			if owner != nil {
				daemon = owner.Daemon
			}
			sv.Log.Printf("uid %d daemon %s: %s %s -> ok=%v %s", uid, daemon, req.Op, req.ID+req.Attempt, reply.OK, reply.Error)
		}
		if err := writeReply(c, reply, fd); err != nil {
			return
		}
		if fd != nil {
			fd.Close()
		}
	}
}

// mutate is create, connect, stop or destroy of exactly the incarnation ref
// names — or released, the evidence of its release.
func (sv *Server) mutate(owner Owner, op string, ref Ref, port uint32) (Reply, *os.File) {
	switch op {
	case "released":
		rel, err := sv.Service.Released(owner, ref)
		if err != nil {
			return Reply{Error: err.Error()}, nil
		}
		return Reply{OK: true, Release: &rel, LateAccounting: rel.Late()}, nil
	case "create":
		pid, pinned, err := sv.Service.CreateOfPinned(owner, ref)
		if err != nil {
			return Reply{Error: err.Error(), Quarantined: errors.Is(err, ErrQuarantined)}, nil
		}
		return Reply{OK: true, PID: pid, Pinned: pinned}, nil
	case "connect":
		f, err := sv.Service.ConnectOf(owner, ref, port)
		if err != nil {
			return Reply{Error: err.Error()}, nil
		}
		return Reply{OK: true}, f
	case "stop":
		if err := sv.Service.StopOf(owner, ref); err != nil {
			return Reply{Error: err.Error()}, nil
		}
		return Reply{OK: true}, nil
	}
	rel, err := sv.Service.DestroyOfRelease(owner, ref)
	if err != nil {
		return Reply{Error: err.Error(), Quarantined: errors.Is(err, ErrQuarantined)}, nil
	}
	return Reply{OK: true, Release: &rel, LateAccounting: rel.Late()}, nil
}

func writeReply(c *net.UnixConn, r Reply, fd *os.File) error {
	data, _ := json.Marshal(r)
	data = append(data, '\n')
	if fd == nil {
		_, err := c.Write(data)
		return err
	}
	rights := syscall.UnixRights(int(fd.Fd()))
	_, _, err := c.WriteMsgUnix(data, rights, nil)
	return err
}

// ---- client --------------------------------------------------------------

// ReplyError is the launcher's own answer to a request that failed, as
// opposed to a connection that failed (whose request's outcome is
// unknown). Is reports ErrQuarantined and ErrRetained from its flags.
type ReplyError struct{ Reply Reply }

func (e *ReplyError) Error() string { return "launcher: " + e.Reply.Error }
func (e *ReplyError) Is(target error) bool {
	return target == ErrQuarantined && e.Reply.Quarantined || target == ErrRetained && e.Reply.Retained
}

// Client is the daemon's connection to the launcher.
type Client struct {
	conn   *net.UnixConn
	reader *bufio.Reader
	mu     sync.Mutex
	Hello  Reply
}

// Bind bounds the client's calls by ctx (runner/launcher/INTEGRATION.md
// §3): its deadline becomes the connection's, and its end interrupts a
// call in progress. A call cut short fails with its outcome unknown, and
// the connection is spent. stop undoes the binding.
func (cl *Client) Bind(ctx context.Context) (stop func()) {
	if d, ok := ctx.Deadline(); ok {
		_ = cl.conn.SetDeadline(d)
	}
	after := context.AfterFunc(ctx, func() { _ = cl.conn.SetDeadline(time.Unix(1, 0)) })
	return func() {
		after()
		_ = cl.conn.SetDeadline(time.Time{})
	}
}

// Dial connects and performs hello; a launcher of another protocol
// version is refused here, before any work.
func Dial(ctx context.Context, path, daemon string) (*Client, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("launcher socket %s is not available: %w", path, err)
	}
	cl := &Client{conn: c.(*net.UnixConn), reader: bufio.NewReaderSize(c, 1<<20)}
	reply, _, err := cl.call(Request{Op: "hello", Daemon: daemon}, false)
	if err != nil {
		c.Close()
		return nil, err
	}
	if reply.Protocol != WireProtocol {
		c.Close()
		return nil, fmt.Errorf("launcher speaks protocol %d, this runner speaks %d", reply.Protocol, WireProtocol)
	}
	cl.Hello = reply
	return cl, nil
}

func (cl *Client) Close() error { return cl.conn.Close() }

func (cl *Client) call(req Request, wantFD bool) (Reply, *os.File, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := cl.conn.Write(data); err != nil {
		// the launcher may have answered and closed before reading this
		// request — it refuses a peer that way — and its answer says why
		_ = cl.conn.SetReadDeadline(time.Now().Add(time.Second))
		if line, rerr := cl.reader.ReadBytes('\n'); rerr == nil {
			var reply Reply
			if json.Unmarshal(line, &reply) == nil && !reply.OK && reply.Error != "" {
				return reply, nil, &ReplyError{reply}
			}
		}
		return Reply{}, nil, err
	}
	var reply Reply
	var fd *os.File
	if wantFD {
		buf := make([]byte, 64*1024)
		oob := make([]byte, syscall.CmsgSpace(4))
		n, oobn, _, _, err := cl.conn.ReadMsgUnix(buf, oob)
		if err != nil {
			return Reply{}, nil, err
		}
		line := buf[:n]
		if oobn > 0 {
			msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
			if err == nil {
				for _, m := range msgs {
					fds, err := syscall.ParseUnixRights(&m)
					if err == nil && len(fds) > 0 {
						syscall.CloseOnExec(fds[0])
						fd = os.NewFile(uintptr(fds[0]), "vsock")
					}
				}
			}
		}
		if err := json.Unmarshal(line, &reply); err != nil {
			return Reply{}, fd, fmt.Errorf("launcher: bad reply: %w", err)
		}
	} else {
		line, err := cl.reader.ReadBytes('\n')
		if err != nil {
			return Reply{}, nil, err
		}
		if err := json.Unmarshal(line, &reply); err != nil {
			return Reply{}, nil, fmt.Errorf("launcher: bad reply: %w", err)
		}
	}
	if !reply.OK {
		if fd != nil {
			fd.Close()
		}
		return reply, nil, &ReplyError{reply}
	}
	return reply, fd, nil
}

// Reserve answers the reservation's identity. A failed reserve answers
// none; when the launcher kept a reservation it could not acknowledge
// (errors.Is(err, ErrRetained)), the *ReplyError's Reply names it — it
// stays charged until destroyed (Retained). A caller that must settle a
// lost answer names the request token it has kept durably (req.Request;
// INTEGRATION.md §11.10); one that names none gets a fresh token, which it
// never knew before the answer and so cannot settle with.
func (cl *Client) Reserve(req ReserveRequest) (ReserveReply, error) {
	if req.Request == "" {
		token, err := NewRequest()
		if err != nil {
			return ReserveReply{}, err
		}
		req.Request = token
	}
	r, _, err := cl.call(Request{Op: "reserve", Attempt: req.Attempt, Image: req.Image, CPUs: req.CPUs, MemoryMiB: req.MemoryMiB, DiskMiB: req.DiskMiB, DeadlineUnix: req.DeadlineUnix, Network: req.Network, Destinations: req.Destinations, Label: req.Label, Request: req.Request}, false)
	if err != nil {
		return ReserveReply{}, err
	}
	return ReserveReply{ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created, Request: r.Request}, nil
}

// Settle is the launcher's conclusive answer for request, this daemon's
// reserve for attempt (Service.Settle; INTEGRATION.md §11.10): admitted
// (Record: the reservation held now), released (Release) or closed; or
// the launcher's refusal, which settles nothing.
func (cl *Client) Settle(attempt, request string) (Settlement, error) {
	r, _, err := cl.call(Request{Op: "settle", Attempt: attempt, Request: request}, false)
	if err != nil {
		return Settlement{}, err
	}
	st := Settlement{Outcome: r.Settled, Record: r.Record, Release: r.Release, Why: r.SettledWhy}
	switch {
	case st.Outcome == SettledAdmitted && st.Record != nil && st.Record.Request == request && st.Record.Attempt == attempt:
	case st.Outcome == SettledReleased && st.Release != nil:
	case st.Outcome == SettledClosed:
	default:
		return Settlement{}, fmt.Errorf("launcher: settle answered %q without what it settles", r.Settled)
	}
	return st, nil
}

// Retained names the reservation a failed Reserve left charged, if the
// launcher answered that it did (ErrRetained).
func Retained(err error) (ReserveReply, bool) {
	var re *ReplyError
	if !errors.As(err, &re) || !re.Reply.Retained {
		return ReserveReply{}, false
	}
	return ReserveReply{ID: re.Reply.ID, Incarnation: re.Reply.Incarnation, CID: re.Reply.CID, Created: re.Reply.Created, Request: re.Reply.Request}, true
}

// refRequest is a request naming ref's incarnation.
func refRequest(op string, ref Ref) Request {
	return Request{Op: op, ID: ref.ID, Incarnation: ref.Incarnation, CID: ref.CID, Created: ref.Created}
}

// Create boots exactly the incarnation ref names (ErrStale: another one).
func (cl *Client) Create(ref Ref) (int, error) {
	pid, _, err := cl.CreatePinned(ref)
	return pid, err
}

// CreatePinned is Create, answering also each granted DNS name with the
// addresses the launcher pinned it to (Reply.Pinned).
func (cl *Client) CreatePinned(ref Ref) (int, []Pin, error) {
	r, _, err := cl.call(refRequest("create", ref), false)
	return r.PID, r.Pinned, err
}

// Connect connects to exactly the incarnation ref names.
func (cl *Client) Connect(ref Ref, port uint32) (*os.File, error) {
	req := refRequest("connect", ref)
	req.Port = port
	_, fd, err := cl.call(req, true)
	if err != nil {
		return nil, err
	}
	if fd == nil {
		return nil, errors.New("launcher: connect answered without a descriptor")
	}
	return fd, nil
}

func (cl *Client) Inspect(id string) (Record, error) {
	r, _, err := cl.call(Request{Op: "inspect", ID: id}, false)
	if err != nil {
		return Record{}, err
	}
	return *r.Record, nil
}

// Stop signals exactly the incarnation ref names.
func (cl *Client) Stop(ref Ref) error {
	_, _, err := cl.call(refRequest("stop", ref), false)
	return err
}

// DestroyOf destroys exactly the incarnation ref names; when the launcher
// does not hold it, it answers done only on the durable evidence of its
// release (Service.DestroyOf; INTEGRATION.md §11.8). quarantined says the
// reservation stays counted on the launcher side.
func (cl *Client) DestroyOf(ref Ref) (quarantined bool, err error) {
	r, _, err := cl.call(refRequest("destroy", ref), false)
	return r.Quarantined, err
}

// DestroyOfRelease is DestroyOf answering, when it is done, the release's
// disposition — its late bookkeeping included.
func (cl *Client) DestroyOfRelease(ref Ref) (Release, error) {
	r, _, err := cl.call(refRequest("destroy", ref), false)
	if err != nil || r.Release == nil {
		return Release{}, err
	}
	return *r.Release, nil
}

// Released is the launcher's durable evidence of the release of exactly
// the incarnation ref names (Service.Released; INTEGRATION.md §11.8): its
// disposition, or the launcher's refusal — pending, held, unproven, or
// the fence's. A consumer takes nothing else for a release.
func (cl *Client) Released(ref Ref) (Release, error) {
	r, _, err := cl.call(refRequest("released", ref), false)
	if err != nil {
		return Release{}, err
	}
	if r.Release == nil {
		return Release{}, errors.New("launcher: released answered without its evidence")
	}
	return *r.Release, nil
}

func (cl *Client) List() ([]Record, error) {
	r, _, err := cl.call(Request{Op: "list"}, false)
	return r.VMs, err
}
