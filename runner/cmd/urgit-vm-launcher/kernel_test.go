package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"urgit/runner/internal/launcher"
)

// C7: exercise the real filesystem and metadata operations without root or
// external disk tools. Only cp/resize2fs/tune2fs are stubbed; cp makes a new file.
func kernelCopyHost(t *testing.T) (*realHost, launcher.Image) {
	t.Helper()
	h := testHost(t, &recorder{})
	h.vmUID, h.vmGID = os.Getuid(), os.Getgid()
	base := t.TempDir()
	img := launcher.Image{Rootfs: filepath.Join(base, "rootfs"), Kernel: filepath.Join(base, "kernel")}
	for _, p := range []string{img.Rootfs, img.Kernel} {
		if err := os.WriteFile(p, []byte("golden bytes\x00\xff\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.command = func(ctx context.Context, owned bool, name string, args ...string) (string, bool, error) {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		switch name {
		case "cp":
			data, err := os.ReadFile(args[len(args)-2])
			if err == nil {
				err = os.WriteFile(args[len(args)-1], data, 0o644)
			}
			return "", true, err
		case "resize2fs", "tune2fs":
			return "", true, nil
		default:
			return "", false, fmt.Errorf("unexpected command: %s", name)
		}
	}
	return h, img
}

func TestPrepareDiskKernelIndependentCopy(t *testing.T) {
	h, img := kernelCopyHost(t)
	stat := func(p string) syscall.Stat_t {
		t.Helper()
		var s syscall.Stat_t
		if err := syscall.Stat(p, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	read := func(p string) []byte {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	before, want := stat(img.Kernel), read(img.Kernel)
	id := launcher.IDFor("t", "0v1")
	if _, err := h.PrepareDisk(id, img, 2); err != nil {
		t.Fatal(err)
	}
	after := stat(img.Kernel)
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode ||
		before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink {
		t.Errorf("installed kernel metadata changed: before=%+v after=%+v", before, after)
	}
	kernel := filepath.Join(h.jailRoot(id), "vmlinux")
	jail := stat(kernel)
	if before.Dev == jail.Dev && before.Ino == jail.Ino {
		t.Error("jail kernel shares the installed kernel inode")
	}
	if jail.Uid != uint32(h.vmUID) || jail.Gid != uint32(h.vmGID) || jail.Mode&0o7777 != 0o400 || jail.Nlink != 1 {
		t.Errorf("jail kernel ownership/mode/link count: %+v", jail)
	}
	if !bytes.Equal(read(kernel), want) || !bytes.Equal(read(img.Kernel), want) {
		t.Error("kernel bytes differ")
	}
	// The VM user owns its copy: making it writable and changing it must not
	// alter the golden kernel used by later VMs.
	if err := os.Chmod(kernel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernel, []byte("changed by this VM"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read(img.Kernel), want) {
		t.Error("writing jail kernel changed installed kernel bytes")
	}
}

func TestPrepareDiskKernelCopyFailureIsNotNoEffect(t *testing.T) {
	h, img := kernelCopyHost(t)
	copyCommand := h.command
	h.command = func(ctx context.Context, owned bool, name string, args ...string) (string, bool, error) {
		if name == "cp" && args[len(args)-2] == img.Kernel {
			return "kernel copy failed", true, errors.New("injected copy failure")
		}
		return copyCommand(ctx, owned, name, args...)
	}
	_, err := h.PrepareDisk(launcher.IDFor("t", "0v1"), img, 2)
	if err == nil || errors.Is(err, launcher.ErrNoEffect) || err.Error() != "kernel copy failed" {
		t.Fatalf("copy failure after creating jail must retain effect/error semantics: %v", err)
	}
}
