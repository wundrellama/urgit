package guest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// the guest side serves a session over any stream; here a net.Pipe, in
// the VM a vsock connection. fakeRunner runs argv on the host in place of
// the guest's exec so the stream and exit code paths are exercised.

func startServer(t *testing.T, root string) (*Session, *Server) {
	t.Helper()
	hostEnd, guestEnd := net.Pipe()
	srv := NewServer(root, Ready{Helper: ProtocolVersion, Kernel: "test", Docker: "none"}, ShellRunner{})
	go srv.Serve(guestEnd)
	s := NewSession(hostEnd)
	t.Cleanup(func() { hostEnd.Close(); guestEnd.Close() })
	return s, srv
}

func TestHelloReadyAndSecondHelloRefused(t *testing.T) {
	s, _ := startServer(t, t.TempDir())
	ready, err := s.Hello(context.Background(), Hello{Attempt: "0v1", Nonce: "0v2"})
	if err != nil || ready.Kernel != "test" {
		t.Fatalf("hello: %v %+v", err, ready)
	}
	if _, err := s.Hello(context.Background(), Hello{Attempt: "0v1", Nonce: "0v2"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a second HELLO must be refused: %v", err)
	}
}

func TestPutVerifiesDigestAndWorkspace(t *testing.T) {
	root := t.TempDir()
	s, _ := startServer(t, root)
	if _, err := s.Hello(context.Background(), Hello{Attempt: "0v1", Nonce: "0v2"}); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hi"), 0o644)
	ok, err := s.Put(context.Background(), src, "src", Limits{MaxBytes: 1 << 20, MaxEntries: 100})
	if err != nil || ok.Entries != 1 {
		t.Fatalf("put: %v %+v", err, ok)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "src", "hello.txt")); string(data) != "hi" {
		t.Fatalf("not extracted: %q", data)
	}
	// the guest recomputes the workspace digest the host announced; a
	// different one is a refused bundle, not a warning
	want, _ := WorkspaceDigest(src)
	if got, err := s.Verify(context.Background(), "src", want); err != nil || !got {
		t.Fatalf("verify: %v %v", err, got)
	}
	if got, err := s.Verify(context.Background(), "src", "0000"); err != nil || got {
		t.Fatalf("a wrong digest verified: %v %v", err, got)
	}
	// a destination outside /work is refused by the guest
	if _, err := s.Put(context.Background(), src, "../etc", Limits{MaxBytes: 1 << 20, MaxEntries: 100}); err == nil {
		t.Fatal("a destination outside the root must be refused")
	}
}

func TestExecStreamsOutputAndExitCode(t *testing.T) {
	s, _ := startServer(t, t.TempDir())
	s.Hello(context.Background(), Hello{Attempt: "0v1", Nonce: "0v2"})
	out, done, err := s.Exec(context.Background(), []string{"sh", "-c", "echo one; echo two >&2; exit 7"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(out)
	code := <-done
	if code != 7 || !strings.Contains(string(data), "one") || !strings.Contains(string(data), "two") {
		t.Fatalf("code %d output %q", code, data)
	}
}

func TestSignalStopsTheProcess(t *testing.T) {
	s, _ := startServer(t, t.TempDir())
	s.Hello(context.Background(), Hello{Attempt: "0v1", Nonce: "0v2"})
	out, done, err := s.Exec(context.Background(), []string{"sh", "-c", "sleep 30"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := s.Signal(context.Background(), "TERM"); err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, out)
	select {
	case code := <-done:
		if code == 0 {
			t.Fatalf("a signalled process reported exit 0")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the process did not stop")
	}
}

func TestExportIsBounded(t *testing.T) {
	root := t.TempDir()
	s, _ := startServer(t, root)
	s.Hello(context.Background(), Hello{Attempt: "0v1", Nonce: "0v2"})
	os.MkdirAll(filepath.Join(root, "artifacts"), 0o755)
	os.WriteFile(filepath.Join(root, "artifacts", "big"), bytes.Repeat([]byte("a"), 5000), 0o644)
	var sink bytes.Buffer
	end, err := s.Export(context.Background(), "artifacts", 1024, &sink)
	if err != nil {
		t.Fatal(err)
	}
	if !end.Truncated || sink.Len() > 1024+1024 {
		t.Fatalf("export not bounded: truncated=%v bytes=%d", end.Truncated, sink.Len())
	}
	if _, err := s.Export(context.Background(), "../", 1024, &sink); err == nil {
		t.Fatal("an export outside the root must be refused")
	}
}

// a forged EXIT for another id, or an EXIT before any EXEC, never
// completes the host's wait: the session fails safely instead
func TestForgedExitIsRefused(t *testing.T) {
	hostEnd, guestEnd := net.Pipe()
	defer hostEnd.Close()
	defer guestEnd.Close()
	s := NewSession(hostEnd)
	go func() {
		// a fake guest: answers READY, then forges frames
		typ, _, _ := ReadFrame(guestEnd, MaxPayload)
		if typ != TypeHello {
			return
		}
		b, _ := json.Marshal(Ready{Helper: ProtocolVersion})
		WriteFrame(guestEnd, TypeReady, b)
		// wait for EXEC, then answer EXIT for a different id
		typ, payload, _ := ReadFrame(guestEnd, MaxPayload)
		var ex Exec
		json.Unmarshal(payload, &ex)
		if typ != TypeExec {
			return
		}
		forged, _ := json.Marshal(Exit{ID: ex.ID + 99, Code: 0})
		WriteFrame(guestEnd, TypeExit, forged)
	}()
	if _, err := s.Hello(context.Background(), Hello{Attempt: "0v1"}); err != nil {
		t.Fatal(err)
	}
	out, done, err := s.Exec(context.Background(), []string{"true"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, out)
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("a forged EXIT for another id was accepted as success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the session hung on a forged EXIT instead of failing")
	}
	if !errors.Is(s.Err(), ErrProtocol) {
		t.Fatalf("session error: %v", s.Err())
	}
}

// an oversize frame from the guest ends the session with a protocol
// error; the host never allocates the announced length
func TestOversizeGuestFrameEndsSession(t *testing.T) {
	hostEnd, guestEnd := net.Pipe()
	defer hostEnd.Close()
	defer guestEnd.Close()
	s := NewSession(hostEnd)
	go func() {
		ReadFrame(guestEnd, MaxPayload)
		guestEnd.Write([]byte{'U', 'G', ProtocolVersion, TypeReady, 0xff, 0xff, 0xff, 0xff})
	}()
	_, err := s.Hello(context.Background(), Hello{Attempt: "0v1"})
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}
