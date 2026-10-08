package guest

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrUnsafePath  = errors.New("tar: unsafe path")
	ErrUnsafeLink  = errors.New("tar: unsafe link target")
	ErrUnsafeEntry = errors.New("tar: entry type is not allowed")
	ErrTooLarge    = errors.New("tar: byte bound exceeded")
	ErrTooMany     = errors.New("tar: entry bound exceeded")
)

// Limits bound one extraction (BRIEF D3: every length and path is
// validated; a stream may never write outside its destination or grow
// without bound).
type Limits struct {
	MaxBytes   int64
	MaxEntries int
}

// Extracted is what the guest reports back in PUT_OK: its own count,
// byte total and digest of the stream it consumed, for the host to
// compare with what it sent.
type Extracted struct {
	Entries int
	Bytes   int64
	SHA256  string
}

// cleanRel validates an archive member name: relative, no `..`, no
// empty component, no leading `/`, and returns the cleaned path.
func cleanRel(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." || part == "" {
			return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
		}
	}
	return cleaned, nil
}

// insideDest says whether target, resolved lexically from the directory
// of rel, stays inside the destination root.
func insideDest(rel, target string) bool {
	if strings.HasPrefix(target, "/") {
		return false
	}
	joined := path.Clean(path.Join(path.Dir(rel), target))
	return joined != ".." && !strings.HasPrefix(joined, "../")
}

// Extract reads a tar stream into dest, refusing every shape the guest
// must not honour: absolute paths, traversal, symlinks or hard links
// whose target leaves dest, hard links to entries not in the stream, an
// entry written through a symlink the stream created, device/fifo
// entries, and more entries or bytes than the limits allow. Exec bits
// and exact lengths are preserved; ownership is the extracting user's.
// The whole stream is hashed so the guest can echo the digest.
func Extract(r io.Reader, dest string, limits Limits) (Extracted, error) {
	var out Extracted
	dest = filepath.Clean(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return out, err
	}
	hasher := sha256.New()
	tr := tar.NewReader(io.TeeReader(r, hasher))
	symlinked := map[string]bool{} // relative paths created as symlinks
	written := map[string]bool{}   // relative paths of regular files written
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, fmt.Errorf("tar: %w", err)
		}
		out.Entries++
		if limits.MaxEntries > 0 && out.Entries > limits.MaxEntries {
			return out, fmt.Errorf("%w: more than %d entries", ErrTooMany, limits.MaxEntries)
		}
		rel, err := cleanRel(hdr.Name)
		if err != nil {
			return out, err
		}
		// no component of the path may be a symlink this stream created:
		// that is the write-through-symlink escape
		for parent := path.Dir(rel); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if symlinked[parent] {
				return out, fmt.Errorf("%w: %q traverses symlink %q", ErrUnsafePath, hdr.Name, parent)
			}
		}
		full := filepath.Join(dest, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(full, 0o755); err != nil {
				return out, err
			}
			if err := os.Chmod(full, fs.FileMode(hdr.Mode)&0o777|0o700); err != nil {
				return out, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if limits.MaxBytes > 0 && out.Bytes+hdr.Size > limits.MaxBytes {
				return out, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, limits.MaxBytes)
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return out, err
			}
			f, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return out, err
			}
			n, err := io.Copy(f, io.LimitReader(tr, hdr.Size))
			f.Close()
			if err != nil {
				return out, err
			}
			if n != hdr.Size {
				return out, fmt.Errorf("tar: entry %q shorter than its header (%d of %d bytes)", hdr.Name, n, hdr.Size)
			}
			if err := os.Chmod(full, fs.FileMode(hdr.Mode)&0o777); err != nil {
				return out, err
			}
			out.Bytes += n
			written[rel] = true
		case tar.TypeSymlink:
			if !insideDest(rel, hdr.Linkname) {
				return out, fmt.Errorf("%w: %q -> %q", ErrUnsafeLink, hdr.Name, hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return out, err
			}
			_ = os.Remove(full)
			if err := os.Symlink(hdr.Linkname, full); err != nil {
				return out, err
			}
			symlinked[rel] = true
		case tar.TypeLink:
			target, err := cleanRel(hdr.Linkname)
			if err != nil || !written[target] {
				return out, fmt.Errorf("%w: hard link %q -> %q", ErrUnsafeLink, hdr.Name, hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return out, err
			}
			if err := os.Link(filepath.Join(dest, filepath.FromSlash(target)), full); err != nil {
				return out, err
			}
			written[rel] = true
		default:
			return out, fmt.Errorf("%w: %q type %q", ErrUnsafeEntry, hdr.Name, hdr.Typeflag)
		}
	}
	// drain the trailer so the digest covers the same bytes the host hashed
	_, _ = io.Copy(io.Discard, io.TeeReader(r, hasher))
	out.SHA256 = hex.EncodeToString(hasher.Sum(nil))
	return out, nil
}

// Archive writes a canonical tar of a directory: entries sorted by path,
// mtime zero, uid/gid 0, no user names, regular files, directories and
// symlinks only (anything else is refused). The digest of the bytes
// written and the entry count come back; the same tree always yields
// the same bytes.
func Archive(src string, w io.Writer) (string, int, error) {
	hasher := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(w, hasher))
	var entries []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == src {
			return nil
		}
		entries = append(entries, p)
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Strings(entries)
	count := 0
	for _, p := range entries {
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(p)
		if err != nil {
			return "", 0, err
		}
		hdr := &tar.Header{Name: rel, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		switch {
		case info.IsDir():
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
			hdr.Mode = int64(info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return "", 0, err
			}
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = target
			hdr.Mode = 0o777
		case info.Mode().IsRegular():
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = int64(info.Mode().Perm())
			hdr.Size = info.Size()
		default:
			return "", 0, fmt.Errorf("%w: %q", ErrUnsafeEntry, rel)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return "", 0, err
		}
		if hdr.Typeflag == tar.TypeReg {
			f, err := os.Open(p)
			if err != nil {
				return "", 0, err
			}
			n, err := io.Copy(tw, f)
			f.Close()
			if err != nil {
				return "", 0, err
			}
			if n != hdr.Size {
				return "", 0, fmt.Errorf("tar: %q changed size while archiving", rel)
			}
		}
		count++
	}
	if err := tw.Close(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), count, nil
}

// WorkspaceDigest is the identity of a tree's contents: sha256 over the
// sorted lines `path\0mode\0sha256(content)\n` for regular files and
// `path\0l\0target\n` for symlinks (directories contribute nothing;
// `.git` is skipped). Both sides compute it: the host before the copy,
// the guest after extraction (P07).
func WorkspaceDigest(dir string) (string, error) {
	var lines []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != dir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			lines = append(lines, rel+"\x00l\x00"+target+"\n")
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %q", ErrUnsafeEntry, rel)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		mode := "f"
		if info.Mode()&0o111 != 0 {
			mode = "x"
		}
		lines = append(lines, rel+"\x00"+mode+"\x00"+hex.EncodeToString(h.Sum(nil))+"\n")
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
