package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/guest"
)

// Record the actual outbound protocol; the peer is the real guest Server.
type copyWire struct {
	net.Conn
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *copyWire) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	w.mu.Unlock()
	return w.Conn.Write(p)
}

func (w *copyWire) snapshot() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func copyGuest(t *testing.T) (*Microvm, Handle, string, *copyWire, context.Context) {
	t.Helper()
	root := t.TempDir()
	a, b := net.Pipe()
	deadline := time.Now().Add(10 * time.Second)
	if err := a.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := b.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	wire := &copyWire{Conn: a}
	sess := guest.NewSession(wire)
	server := guest.NewServer(root, guest.Ready{Helper: guest.ProtocolVersion}, nil)
	done := make(chan error, 1)
	go func() { done <- server.Serve(b) }()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(func() {
		cancel()
		sess.Close()
		b.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("guest server: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("guest server did not stop")
		}
	})
	if _, err := sess.Hello(ctx, guest.Hello{Attempt: "copy-test", Nonce: "in-process"}); err != nil {
		t.Fatal(err)
	}
	h := Handle{ID: "copy-test"}
	m := &Microvm{cfg: &config.Config{WorkDir: t.TempDir()}, sessions: map[string]*guest.Session{h.ID: sess}}
	return m, h, root, wire, ctx
}

func TestMicrovmCopyWorkRoot(t *testing.T) {
	for _, suffix := range []string{"", "/"} {
		t.Run("work"+suffix, func(t *testing.T) {
			m, h, root, wire, ctx := copyGuest(t)
			src := t.TempDir()
			files := map[string]string{"bundle.json": "job bundle\n", "src/main.txt": "source bytes\n"}
			for name, body := range files {
				p := filepath.Join(src, name)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, count, err := guest.Archive(src, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			// The daemon's real call shape: bundle.Dir+"/.", ExecEnv(h).WorkRoot.
			dest := m.ExecEnv(h).WorkRoot + suffix
			if err := m.Copy(ctx, h, src+"/.", dest); err != nil {
				t.Fatalf("copy bundle: %v", err)
			}
			for name, body := range files {
				got, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(got) != body {
					t.Fatalf("guest root file %s: %q, %v", name, got, err)
				}
			}
			got, entries, err := guest.Archive(root, io.Discard)
			if err != nil || got != want || entries != count {
				t.Fatalf("guest archive: digest=%s entries=%d err=%v; want %s/%d", got, entries, err, want, count)
			}
			r := bytes.NewReader(wire.snapshot())
			begins, ends := 0, 0
			for r.Len() > 0 {
				typ, payload, err := guest.ReadFrame(r, guest.MaxPayload)
				if err != nil {
					t.Fatal(err)
				}
				switch typ {
				case guest.TypePutBegin:
					begins++
					var pb guest.PutBegin
					if err := json.Unmarshal(payload, &pb); err != nil {
						t.Fatal(err)
					}
					if pb.Dest != "" {
						t.Errorf("root destination = %q, want empty", pb.Dest)
					}
				case guest.TypePutEnd:
					ends++
					var pe guest.PutEnd
					if err := json.Unmarshal(payload, &pe); err != nil {
						t.Fatal(err)
					}
					if pe.SHA256 != want {
						t.Errorf("announced digest = %s, want %s", pe.SHA256, want)
					}
				}
			}
			if begins != 1 || ends != 1 {
				t.Fatalf("PUT_BEGIN/END counts = %d/%d", begins, ends)
			}
			// Copy returning nil also requires the real server's PUT_OK digest echo.
		})
	}
}

func TestMicrovmCopyRefusesOutsideWork(t *testing.T) {
	for _, tc := range []struct {
		name, dest string
		file       bool
	}{
		{"root-file", "/work", true}, {"root-slash-file", "/work/", true},
		{"sibling-x", "/workx", false}, {"sibling-other", "/work-other", false},
		{"sibling-child", "/workx/a", false}, {"absolute", "/elsewhere/a", false},
		{"filesystem-root", "/", false}, {"empty", "", false},
		{"parent", "/work/..", false}, {"parent-child", "/work/../escape", false},
		{"nested-escape", "/work/a/../../escape", false},
		{"relative-parent", "..", false}, {"relative-escape", "../escape", false},
		{"relative-nested-escape", "a/../../escape", false},
		{"double-slash-absolute", "/work//elsewhere", false},
		{"file-escape", "/work/../file", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, h, root, wire, ctx := copyGuest(t)
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "file"), []byte("payload"), 0644); err != nil {
				t.Fatal(err)
			}
			if tc.file {
				src = filepath.Join(src, "file")
			}
			before := wire.snapshot()
			err := m.Copy(ctx, h, src, tc.dest)
			want := fmt.Sprintf("copy: %s is not under /work", tc.dest)
			if err == nil || err.Error() != want {
				t.Fatalf("got %v; want %q", err, want)
			}
			if !bytes.Equal(before, wire.snapshot()) {
				t.Error("refused path sent data to guest")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("refusal changed guest root: %v, %v", entries, err)
			}
		})
	}
}

func TestMicrovmCopyUnderWork(t *testing.T) {
	for _, file := range []bool{false, true} {
		t.Run(fmt.Sprintf("file=%v", file), func(t *testing.T) {
			m, h, root, _, ctx := copyGuest(t)
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "input"), []byte("payload"), 0644); err != nil {
				t.Fatal(err)
			}
			dest, name := "/work/sub", "sub/input"
			if file {
				src, dest, name = filepath.Join(src, "input"), "/work/sub/renamed", "sub/renamed"
			}
			if err := m.Copy(ctx, h, src, dest); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || string(got) != "payload" {
				t.Fatalf("copied file: %q, %v", got, err)
			}
		})
	}
}
