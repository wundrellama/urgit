package guest

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// a tar built by hand so every hostile shape the guest must refuse (M09)
// can be written exactly: the writer in Archive would never emit them
func hostileTar(t *testing.T, entries ...*tar.Header) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range entries {
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		body := []byte(nil)
		if h.Typeflag == tar.TypeReg {
			body = bytes.Repeat([]byte("x"), int(h.Size))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	return &buf
}

func extractInto(t *testing.T, r *bytes.Buffer, limits Limits) (string, Extracted, error) {
	t.Helper()
	dest := t.TempDir()
	got, err := Extract(r, dest, limits)
	return dest, got, err
}

var lenient = Limits{MaxBytes: 1 << 20, MaxEntries: 100}

func TestExtractRefusesAbsolutePath(t *testing.T) {
	_, _, err := extractInto(t, hostileTar(t, &tar.Header{Name: "/etc/passwd", Mode: 0o644, Size: 3}), lenient)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("want ErrUnsafePath, got %v", err)
	}
}

func TestExtractRefusesTraversal(t *testing.T) {
	_, _, err := extractInto(t, hostileTar(t, &tar.Header{Name: "a/../../evil", Mode: 0o644, Size: 3}), lenient)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("want ErrUnsafePath, got %v", err)
	}
}

func TestExtractRefusesSymlinkEscape(t *testing.T) {
	_, _, err := extractInto(t, hostileTar(t, &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"}), lenient)
	if !errors.Is(err, ErrUnsafeLink) {
		t.Fatalf("want ErrUnsafeLink, got %v", err)
	}
	_, _, err = extractInto(t, hostileTar(t, &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc"}), lenient)
	if !errors.Is(err, ErrUnsafeLink) {
		t.Fatalf("absolute target: want ErrUnsafeLink, got %v", err)
	}
}

// a symlink inside the tree followed by a file written THROUGH it is the
// classic escape: dir -> ../../ then dir/file lands outside
func TestExtractRefusesWriteThroughSymlink(t *testing.T) {
	dest := t.TempDir()
	outside := filepath.Join(dest, "..", "escaped-"+filepath.Base(dest))
	defer os.Remove(outside)
	buf := hostileTar(t,
		&tar.Header{Name: "safe", Typeflag: tar.TypeSymlink, Linkname: "sub"},
		&tar.Header{Name: "safe/file", Mode: 0o644, Size: 3},
	)
	// "safe" -> "sub" is inside dest, but writing safe/file would go
	// through a symlink the stream itself created; refused regardless
	if _, err := Extract(buf, dest, lenient); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("want ErrUnsafePath, got %v", err)
	}
}

func TestExtractRefusesDevicesAndFifos(t *testing.T) {
	for _, tf := range []byte{tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
		_, _, err := extractInto(t, hostileTar(t, &tar.Header{Name: "dev", Typeflag: tf, Mode: 0o644}), lenient)
		if !errors.Is(err, ErrUnsafeEntry) {
			t.Fatalf("type %c: want ErrUnsafeEntry, got %v", tf, err)
		}
	}
}

func TestExtractRefusesHardLinkOutside(t *testing.T) {
	_, _, err := extractInto(t, hostileTar(t, &tar.Header{Name: "hl", Typeflag: tar.TypeLink, Linkname: "../../etc/passwd"}), lenient)
	if !errors.Is(err, ErrUnsafeLink) {
		t.Fatalf("want ErrUnsafeLink, got %v", err)
	}
	// a hard link to an entry that was never extracted is refused too
	_, _, err = extractInto(t, hostileTar(t, &tar.Header{Name: "hl", Typeflag: tar.TypeLink, Linkname: "never-written"}), lenient)
	if !errors.Is(err, ErrUnsafeLink) {
		t.Fatalf("dangling: want ErrUnsafeLink, got %v", err)
	}
}

func TestExtractEnforcesBounds(t *testing.T) {
	_, _, err := extractInto(t, hostileTar(t, &tar.Header{Name: "big", Mode: 0o644, Size: 2000}), Limits{MaxBytes: 1000, MaxEntries: 10})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("bytes: want ErrTooLarge, got %v", err)
	}
	hs := []*tar.Header{}
	for i := 0; i < 3; i++ {
		hs = append(hs, &tar.Header{Name: string(rune('a' + i)), Mode: 0o644, Size: 1})
	}
	_, _, err = extractInto(t, hostileTar(t, hs...), Limits{MaxBytes: 1000, MaxEntries: 2})
	if !errors.Is(err, ErrTooMany) {
		t.Fatalf("entries: want ErrTooMany, got %v", err)
	}
	// a header whose declared size exceeds the actual bytes is a
	// truncated archive, never a partial success
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "lie", Mode: 0o644, Size: 100})
	_, _ = tw.Write([]byte("only-five"))
	raw := buf.Bytes()
	if _, err := Extract(bytes.NewReader(raw), t.TempDir(), lenient); err == nil {
		t.Fatal("a tar whose entry is shorter than its header must fail")
	}
}

func TestExtractPreservesExecBitAndLength(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "bin"), 0o755)
	os.WriteFile(filepath.Join(src, "bin", "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	os.WriteFile(filepath.Join(src, "data.bin"), []byte{0, 1, 2, 3, 0, 0, 255}, 0o644)
	os.Symlink("bin/run.sh", filepath.Join(src, "link"))
	var buf bytes.Buffer
	// four entries: the directory, the script, the binary, the symlink
	sum, n, err := Archive(src, &buf)
	if err != nil || n != 4 {
		t.Fatalf("archive: %v entries=%d", err, n)
	}
	dest, got, err := extractInto(t, &buf, lenient)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != sum {
		t.Fatalf("digest differs: %s vs %s", got.SHA256, sum)
	}
	st, _ := os.Stat(filepath.Join(dest, "bin", "run.sh"))
	if st.Mode()&0o111 == 0 {
		t.Fatalf("exec bit lost: %v", st.Mode())
	}
	data, _ := os.ReadFile(filepath.Join(dest, "data.bin"))
	if !bytes.Equal(data, []byte{0, 1, 2, 3, 0, 0, 255}) {
		t.Fatalf("binary length/content changed: %v", data)
	}
	target, _ := os.Readlink(filepath.Join(dest, "link"))
	if target != "bin/run.sh" {
		t.Fatalf("symlink target %q", target)
	}
}

// Archive is canonical: the same tree gives the same bytes and digest
// whatever the mtimes and the directory walk order on the host
func TestArchiveIsCanonical(t *testing.T) {
	mk := func() string {
		d := t.TempDir()
		os.MkdirAll(filepath.Join(d, "z"), 0o755)
		os.WriteFile(filepath.Join(d, "z", "f"), []byte("1"), 0o644)
		os.WriteFile(filepath.Join(d, "a"), []byte("2"), 0o600)
		return d
	}
	var b1, b2 bytes.Buffer
	s1, _, _ := Archive(mk(), &b1)
	s2, _, _ := Archive(mk(), &b2)
	if s1 != s2 || !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatal("two archives of the same tree differ")
	}
}

func TestWorkspaceDigestSeesContentModeAndLinks(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "f"), []byte("1"), 0o644)
	d1, err := WorkspaceDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, "f"), []byte("2"), 0o644)
	d2, _ := WorkspaceDigest(d)
	os.Chmod(filepath.Join(d, "f"), 0o755)
	d3, _ := WorkspaceDigest(d)
	os.Symlink("f", filepath.Join(d, "l"))
	d4, _ := WorkspaceDigest(d)
	if d1 == d2 || d2 == d3 || d3 == d4 {
		t.Fatalf("digests did not change: %s %s %s %s", d1, d2, d3, d4)
	}
	// a .git directory is not part of the workspace identity
	os.MkdirAll(filepath.Join(d, ".git"), 0o755)
	os.WriteFile(filepath.Join(d, ".git", "HEAD"), []byte("ref"), 0o644)
	d5, _ := WorkspaceDigest(d)
	if d5 != d4 {
		t.Fatal(".git changed the workspace digest")
	}
}
