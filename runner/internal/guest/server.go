package guest

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Runner starts processes for the guest server: the real helper runs
// them in the VM; tests run them on the host. Nothing here decides what
// runs — the host's EXEC does — but the helper is the only thing that
// can, and only under its root.
type Runner interface {
	Start(ctx context.Context, e Exec, out io.Writer) (Process, error)
}

type Process interface {
	Wait() int
	Signal(name string) error
}

// ShellRunner is the os/exec implementation.
type ShellRunner struct{}

func (ShellRunner) Start(ctx context.Context, e Exec, out io.Writer) (Process, error) {
	if len(e.Argv) == 0 {
		return nil, errors.New("empty argv")
	}
	cmd := exec.CommandContext(ctx, e.Argv[0], e.Argv[1:]...)
	cmd.Env = append(os.Environ(), e.Env...)
	cmd.Dir = e.Cwd
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &shellProcess{cmd: cmd}, nil
}

type shellProcess struct{ cmd *exec.Cmd }

func (p *shellProcess) Wait() int {
	err := p.cmd.Wait()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code >= 0 {
			return code
		}
		return 128
	}
	return 127
}

func (p *shellProcess) Signal(name string) error {
	sig := syscall.SIGTERM
	switch strings.ToUpper(strings.TrimPrefix(name, "SIG")) {
	case "KILL":
		sig = syscall.SIGKILL
	case "INT":
		sig = syscall.SIGINT
	case "TERM":
	default:
		return fmt.Errorf("unknown signal %q", name)
	}
	// the whole process group: act's children go with it
	return syscall.Kill(-p.cmd.Process.Pid, sig)
}

// Server is the guest side of one session. It serves exactly one HELLO,
// extracts streams only under root, runs one process at a time, exports
// only from under root, and answers ERROR to everything else.
type Server struct {
	root   string
	ready  Ready
	runner Runner
	// OnShutdown runs after SHUTDOWN is acknowledged (the helper powers
	// the guest off there).
	OnShutdown func()
}

func NewServer(root string, ready Ready, runner Runner) *Server {
	return &Server{root: filepath.Clean(root), ready: ready, runner: runner}
}

// under resolves a request path under the root, refusing escapes.
func (s *Server) under(p string) (string, error) {
	rel, err := cleanRel(p)
	if err != nil {
		if p == "." || p == "" {
			return s.root, nil
		}
		return "", err
	}
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	if full != s.root && !strings.HasPrefix(full, s.root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, p)
	}
	return full, nil
}

type serverConn struct {
	conn io.ReadWriteCloser
	wmu  sync.Mutex
}

func (c *serverConn) send(typ byte, v any) error {
	var payload []byte
	switch b := v.(type) {
	case nil:
		payload = []byte("{}")
	case []byte:
		payload = b
	default:
		var err error
		if payload, err = json.Marshal(v); err != nil {
			return err
		}
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteFrame(c.conn, typ, payload)
}

func (c *serverConn) errorf(code, format string, args ...any) error {
	return c.send(TypeError, Error{Code: code, Message: fmt.Sprintf(format, args...)})
}

// outputWriter frames a process's output as OUTPUT frames.
type outputWriter struct {
	c  *serverConn
	id uint32
}

func (o outputWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > MaxPayload-4 {
			chunk = p[:MaxPayload-4]
		}
		payload := make([]byte, 4+len(chunk))
		binary.BigEndian.PutUint32(payload[:4], o.id)
		copy(payload[4:], chunk)
		if err := o.c.send(TypeOutput, payload); err != nil {
			return total, err
		}
		total += len(chunk)
		p = p[len(chunk):]
	}
	return total, nil
}

// Serve runs the session until the stream ends or a violation closes it.
func (s *Server) Serve(conn io.ReadWriteCloser) error {
	c := &serverConn{conn: conn}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		hello   bool
		putW    *io.PipeWriter
		putDone chan extractResult
		proc    Process
		procID  uint32
		procMu  sync.Mutex
	)
	violate := func(format string, args ...any) error {
		_ = c.errorf("protocol", format, args...)
		return fmt.Errorf("%w: "+format, append([]any{ErrProtocol}, args...)...)
	}
	for {
		typ, payload, err := ReadFrame(conn, MaxPayload)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				return nil
			}
			_ = c.errorf("protocol", "%v", err)
			return err
		}
		if !hello && typ != TypeHello {
			return violate("frame %#x before HELLO", typ)
		}
		switch typ {
		case TypeHello:
			if hello {
				_ = c.errorf("hello", "session already opened")
				continue
			}
			var h Hello
			if err := json.Unmarshal(payload, &h); err != nil {
				return violate("bad HELLO")
			}
			hello = true
			if err := c.send(TypeReady, s.ready); err != nil {
				return err
			}
		case TypePutBegin:
			var pb PutBegin
			if err := json.Unmarshal(payload, &pb); err != nil {
				return violate("bad PUT_BEGIN")
			}
			if putW != nil {
				return violate("PUT_BEGIN inside a put")
			}
			dest, err := s.under(pb.Dest)
			if err != nil {
				_ = c.errorf("path", "%v", err)
				continue
			}
			pr, pw := io.Pipe()
			putW = pw
			putDone = make(chan extractResult, 1)
			limits := Limits{MaxBytes: pb.MaxBytes, MaxEntries: pb.MaxEntries}
			go func() {
				got, err := Extract(pr, dest, limits)
				if err != nil {
					// drain so the sender is not blocked on a dead pipe
					_, _ = io.Copy(io.Discard, pr)
				}
				putDone <- extractResult{got, err}
			}()
		case TypePutData:
			if putW == nil {
				return violate("PUT_DATA outside a put")
			}
			if _, err := putW.Write(payload); err != nil {
				// the extractor already failed; its error is reported at PUT_END
				continue
			}
		case TypePutEnd:
			if putW == nil {
				return violate("PUT_END outside a put")
			}
			var pe PutEnd
			if err := json.Unmarshal(payload, &pe); err != nil {
				return violate("bad PUT_END")
			}
			putW.Close()
			res := <-putDone
			putW, putDone = nil, nil
			if res.err != nil {
				_ = c.errorf("extract", "%v", res.err)
				continue
			}
			if res.got.SHA256 != pe.SHA256 {
				_ = c.errorf("digest", "stream digest %s is not the announced %s", res.got.SHA256, pe.SHA256)
				continue
			}
			if err := c.send(TypePutOK, PutOK{Entries: res.got.Entries, Bytes: res.got.Bytes, SHA256: res.got.SHA256}); err != nil {
				return err
			}
		case TypeVerify:
			var v Verify
			if err := json.Unmarshal(payload, &v); err != nil {
				return violate("bad VERIFY")
			}
			p, err := s.under(v.Path)
			if err != nil {
				_ = c.errorf("path", "%v", err)
				continue
			}
			sum, err := WorkspaceDigest(p)
			if err != nil {
				_ = c.errorf("digest", "%v", err)
				continue
			}
			if err := c.send(TypeVerified, Verified{OK: sum == v.SHA256, SHA256: sum}); err != nil {
				return err
			}
		case TypeExec:
			var e Exec
			if err := json.Unmarshal(payload, &e); err != nil {
				return violate("bad EXEC")
			}
			procMu.Lock()
			busy := proc != nil
			procMu.Unlock()
			if busy {
				_ = c.errorf("busy", "a process is already running")
				continue
			}
			if e.Cwd != "" {
				cwd, err := s.under(e.Cwd)
				if err != nil {
					_ = c.errorf("path", "%v", err)
					continue
				}
				e.Cwd = cwd
			}
			p, err := s.runner.Start(ctx, e, outputWriter{c, e.ID})
			if err != nil {
				_ = c.errorf("exec", "%v", err)
				// the host is waiting on EXIT for this id: report failure to start
				_ = c.send(TypeExit, Exit{ID: e.ID, Code: 127})
				continue
			}
			procMu.Lock()
			proc, procID = p, e.ID
			procMu.Unlock()
			go func(p Process, id uint32) {
				code := p.Wait()
				procMu.Lock()
				if proc == p {
					proc = nil
				}
				procMu.Unlock()
				_ = c.send(TypeExit, Exit{ID: id, Code: code})
			}(p, e.ID)
		case TypeSignal:
			var sg Signal
			if err := json.Unmarshal(payload, &sg); err != nil {
				return violate("bad SIGNAL")
			}
			procMu.Lock()
			p, id := proc, procID
			procMu.Unlock()
			if p == nil || id != sg.ID {
				_ = c.errorf("signal", "no running process with id %d", sg.ID)
				continue
			}
			if err := p.Signal(sg.Signal); err != nil {
				_ = c.errorf("signal", "%v", err)
			}
		case TypeExport:
			var ex Export
			if err := json.Unmarshal(payload, &ex); err != nil {
				return violate("bad EXPORT")
			}
			p, err := s.under(ex.Path)
			if err != nil || p == s.root {
				_ = c.errorf("path", "export path must be a directory under the root")
				continue
			}
			if err := s.export(c, p, ex.MaxBytes); err != nil {
				return err
			}
		case TypeShutdown:
			cancel()
			if s.OnShutdown != nil {
				s.OnShutdown()
			}
			return nil
		default:
			return violate("unknown frame type %#x", typ)
		}
	}
}

type extractResult struct {
	got Extracted
	err error
}

// export streams a canonical archive of dir in EXPORT_DATA frames up to
// max bytes, then EXPORT_END with the total, digest and whether it was
// cut. The archive is built into a pipe so a huge tree is never held.
func (s *Server) export(c *serverConn, dir string, max int64) error {
	pr, pw := io.Pipe()
	var sum string
	var archiveErr error
	go func() {
		sum, _, archiveErr = Archive(dir, pw)
		pw.CloseWithError(archiveErr)
	}()
	var sent int64
	truncated := false
	buf := make([]byte, 256*1024)
	for {
		n, err := pr.Read(buf)
		if n > 0 && !truncated {
			chunk := buf[:n]
			if max > 0 && sent+int64(len(chunk)) > max {
				chunk = chunk[:max-sent]
				truncated = true
			}
			if len(chunk) > 0 {
				if err := c.send(TypeExportData, chunk); err != nil {
					return err
				}
				sent += int64(len(chunk))
			}
		}
		if err != nil {
			break
		}
	}
	if archiveErr != nil {
		_ = c.errorf("export", "%v", archiveErr)
		return nil
	}
	return c.send(TypeExportEnd, ExportEnd{Bytes: sent, SHA256: sum, Truncated: truncated})
}

// waitFor is a small helper for the guest binary's startup loops.
func waitFor(timeout time.Duration, test func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if test() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

var _ = path.Join
