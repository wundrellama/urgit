package guest

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Session is the host side of one guest connection: it sends control
// frames, streams a bundle in, runs one process at a time and streams
// its output out, and exports a bounded archive. Every reply is checked
// against what was asked; a frame the guest was not entitled to send
// (a forged EXIT for another id, an oversize frame, a bad type) ends
// the session with ErrProtocol and fails whatever was in flight.
type Session struct {
	conn io.ReadWriteCloser
	wmu  sync.Mutex

	mu       sync.Mutex
	err      error
	replies  chan reply
	execID   uint32
	execOut  *io.PipeWriter
	execDone chan int
	export   io.Writer
	exportOK chan reply
	hello    bool
	nextExec uint32
	closed   chan struct{}
}

type reply struct {
	typ     byte
	payload []byte
}

// NewSession wraps a connected stream and starts reading it.
func NewSession(conn io.ReadWriteCloser) *Session {
	s := &Session{conn: conn, replies: make(chan reply, 16), closed: make(chan struct{})}
	go s.reader()
	return s
}

// Err is the session's terminal error, nil while it is healthy.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Session) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
		close(s.closed)
		if s.execOut != nil {
			s.execOut.CloseWithError(err)
			s.execOut = nil
		}
		if s.execDone != nil {
			s.execDone <- 255
			s.execDone = nil
		}
	}
	s.mu.Unlock()
	_ = s.conn.Close()
}

func (s *Session) reader() {
	for {
		typ, payload, err := ReadFrame(s.conn, MaxPayload)
		if err != nil {
			s.fail(err)
			return
		}
		switch typ {
		case TypeOutput:
			if len(payload) < 4 {
				s.fail(fmt.Errorf("%w: short OUTPUT", ErrProtocol))
				return
			}
			id := binary.BigEndian.Uint32(payload[:4])
			s.mu.Lock()
			out := s.execOut
			ok := s.execOut != nil && id == s.execID
			s.mu.Unlock()
			if !ok {
				s.fail(fmt.Errorf("%w: OUTPUT for id %d, not the running process", ErrProtocol, id))
				return
			}
			if _, err := out.Write(payload[4:]); err != nil {
				// the consumer went away; drop the bytes, keep the session
				continue
			}
		case TypeExit:
			var ex Exit
			if err := json.Unmarshal(payload, &ex); err != nil {
				s.fail(fmt.Errorf("%w: bad EXIT", ErrProtocol))
				return
			}
			s.mu.Lock()
			ok := s.execDone != nil && ex.ID == s.execID
			if ok {
				s.execOut.Close()
				s.execOut = nil
				done := s.execDone
				s.execDone = nil
				s.mu.Unlock()
				done <- ex.Code
				continue
			}
			s.mu.Unlock()
			s.fail(fmt.Errorf("%w: EXIT for id %d, not the running process", ErrProtocol, ex.ID))
			return
		case TypeExportData:
			s.mu.Lock()
			w := s.export
			s.mu.Unlock()
			if w == nil {
				s.fail(fmt.Errorf("%w: EXPORT_DATA outside an export", ErrProtocol))
				return
			}
			_, _ = w.Write(payload)
		default:
			select {
			case s.replies <- reply{typ, payload}:
			case <-s.closed:
				return
			}
		}
	}
}

func (s *Session) send(typ byte, v any) error {
	var payload []byte
	switch b := v.(type) {
	case nil:
		payload = []byte("{}")
	case []byte:
		payload = b
	default:
		var err error
		payload, err = json.Marshal(v)
		if err != nil {
			return err
		}
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.Err(); err != nil {
		return err
	}
	return WriteFrame(s.conn, typ, payload)
}

// await waits for the next control reply of one of the wanted types; an
// ERROR is returned as an error, another type is a protocol violation.
func (s *Session) await(ctx context.Context, want ...byte) (reply, error) {
	select {
	case r := <-s.replies:
		if r.typ == TypeError {
			var e Error
			_ = json.Unmarshal(r.payload, &e)
			return r, fmt.Errorf("guest refused: %s: %s", e.Code, e.Message)
		}
		for _, w := range want {
			if r.typ == w {
				return r, nil
			}
		}
		err := fmt.Errorf("%w: unexpected frame type %#x", ErrProtocol, r.typ)
		s.fail(err)
		return r, err
	case <-s.closed:
		return reply{}, s.Err()
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
}

// Hello opens the session; the guest's READY comes back.
func (s *Session) Hello(ctx context.Context, h Hello) (Ready, error) {
	var ready Ready
	if err := s.send(TypeHello, h); err != nil {
		return ready, err
	}
	r, err := s.await(ctx, TypeReady)
	if err != nil {
		return ready, err
	}
	if err := json.Unmarshal(r.payload, &ready); err != nil {
		return ready, fmt.Errorf("%w: bad READY", ErrProtocol)
	}
	if ready.Helper != ProtocolVersion {
		return ready, fmt.Errorf("guest helper speaks protocol %d, this runner speaks %d", ready.Helper, ProtocolVersion)
	}
	return ready, nil
}

// Put streams a canonical archive of a host directory into the guest at
// dest (relative to the guest's root); the guest's PUT_OK must echo the
// digest and count the host computed, or the copy is refused.
func (s *Session) Put(ctx context.Context, hostDir, dest string, limits Limits) (PutOK, error) {
	var ok PutOK
	if err := s.send(TypePutBegin, PutBegin{Dest: dest, MaxBytes: limits.MaxBytes, MaxEntries: limits.MaxEntries}); err != nil {
		return ok, err
	}
	// the archive is streamed in frames as it is produced; the digest is
	// known only at the end and rides PUT_END
	pr, pw := io.Pipe()
	var sum string
	var count int
	var archiveErr error
	go func() {
		sum, count, archiveErr = Archive(hostDir, pw)
		pw.CloseWithError(archiveErr)
	}()
	buf := make([]byte, 256*1024)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			if err := s.send(TypePutData, buf[:n]); err != nil {
				return ok, err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ok, err
		}
	}
	if archiveErr != nil {
		return ok, archiveErr
	}
	if err := s.send(TypePutEnd, PutEnd{SHA256: sum}); err != nil {
		return ok, err
	}
	r, err := s.await(ctx, TypePutOK)
	if err != nil {
		return ok, err
	}
	if err := json.Unmarshal(r.payload, &ok); err != nil {
		return ok, fmt.Errorf("%w: bad PUT_OK", ErrProtocol)
	}
	if ok.SHA256 != sum || ok.Entries != count {
		return ok, fmt.Errorf("guest extracted a different stream (digest %s entries %d; sent %s %d)", ok.SHA256, ok.Entries, sum, count)
	}
	return ok, nil
}

// Verify asks the guest to recompute a workspace digest.
func (s *Session) Verify(ctx context.Context, path, sha256 string) (bool, error) {
	if err := s.send(TypeVerify, Verify{Path: path, SHA256: sha256}); err != nil {
		return false, err
	}
	r, err := s.await(ctx, TypeVerified)
	if err != nil {
		return false, err
	}
	var v Verified
	if err := json.Unmarshal(r.payload, &v); err != nil {
		return false, fmt.Errorf("%w: bad VERIFIED", ErrProtocol)
	}
	return v.OK && v.SHA256 == sha256, nil
}

// Exec starts a process in the guest. The reader carries its combined
// output; the channel delivers the exit code once (255 when the session
// died first).
func (s *Session) Exec(ctx context.Context, argv, env []string, cwd string) (io.ReadCloser, <-chan int, error) {
	s.mu.Lock()
	if s.execDone != nil {
		s.mu.Unlock()
		return nil, nil, errors.New("a process is already running in this session")
	}
	s.nextExec++
	id := s.nextExec
	pr, pw := io.Pipe()
	done := make(chan int, 1)
	s.execID, s.execOut, s.execDone = id, pw, done
	s.mu.Unlock()
	if err := s.send(TypeExec, Exec{ID: id, Argv: argv, Env: env, Cwd: cwd}); err != nil {
		s.mu.Lock()
		s.execOut, s.execDone = nil, nil
		s.mu.Unlock()
		return nil, nil, err
	}
	return pr, done, nil
}

// Signal delivers a signal to the running process.
func (s *Session) Signal(ctx context.Context, signal string) error {
	s.mu.Lock()
	id := s.execID
	running := s.execDone != nil
	s.mu.Unlock()
	if !running {
		return errors.New("no process is running")
	}
	return s.send(TypeSignal, Signal{ID: id, Signal: signal})
}

// Export streams a bounded archive of a guest path into w; the trailer
// says how many bytes the guest produced and whether it stopped at the
// bound. The host writes at most max+one frame.
func (s *Session) Export(ctx context.Context, path string, max int64, w io.Writer) (ExportEnd, error) {
	var end ExportEnd
	s.mu.Lock()
	s.export = &boundedWriter{w: w, left: max + MaxPayload}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.export = nil
		s.mu.Unlock()
	}()
	if err := s.send(TypeExport, Export{Path: path, MaxBytes: max}); err != nil {
		return end, err
	}
	r, err := s.await(ctx, TypeExportEnd)
	if err != nil {
		return end, err
	}
	if err := json.Unmarshal(r.payload, &end); err != nil {
		return end, fmt.Errorf("%w: bad EXPORT_END", ErrProtocol)
	}
	return end, nil
}

// Shutdown asks the guest to power off; the session is closed either way.
func (s *Session) Shutdown(ctx context.Context) error {
	err := s.send(TypeShutdown, nil)
	_ = s.conn.Close()
	return err
}

// Close ends the session without asking the guest anything.
func (s *Session) Close() error {
	s.fail(errors.New("session closed"))
	return nil
}

type boundedWriter struct {
	w    io.Writer
	left int64
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	b.left -= int64(len(p))
	if len(p) == 0 {
		return 0, nil
	}
	return b.w.Write(p)
}

// ReadClose wraps a pipe reader so a caller may close it early.
var _ = os.ErrClosed
