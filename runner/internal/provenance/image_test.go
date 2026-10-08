package provenance

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// the local name a locked image loads under: the reference's path without
// its registry, tag or digest, under urgit-locked, tagged by the manifest
// digest — never a name a registry serves
func TestLockedTagAndImageName(t *testing.T) {
	const d = "sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"
	cases := map[string]string{
		"docker.io/library/busybox@" + d: "urgit-locked/library/busybox:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662",
		"busybox:1.36.1":                 "urgit-locked/busybox:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662",
		"ghcr.io/Org/Img:v1":             "urgit-locked/org/img:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662",
		"localhost:5000/x/y":             "urgit-locked/x/y:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662",
		"node:20-bookworm":               "urgit-locked/node:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662",
	}
	for ref, want := range cases {
		if got := LockedTag(ref, d); got != want {
			t.Errorf("LockedTag(%q) = %q, want %q", ref, got, want)
		}
	}
	names := map[string]string{
		"docker.io/library/busybox@" + d: "docker.io/library/busybox",
		"busybox:1.36.1":                 "busybox",
		"localhost:5000/x/y:tag":         "localhost:5000/x/y",
		"ghcr.io/org/img":                "ghcr.io/org/img",
	}
	for ref, want := range names {
		if got := ImageName(ref); got != want {
			t.Errorf("ImageName(%q) = %q, want %q", ref, got, want)
		}
	}
}

// the id an archive loads as is the sha256 of the config blob its
// manifest names; an archive with two images, or whose config is
// missing, is refused
func TestArchiveImageID(t *testing.T) {
	config := []byte(`{"architecture":"amd64","os":"linux","config":{},"rootfs":{"type":"layers","diff_ids":["sha256:aaaa","sha256:bbbb"]}}`)
	sum := sha256.Sum256(config)
	id := hex.EncodeToString(sum[:])
	write := func(t *testing.T, manifest string, files map[string][]byte) string {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		add := func(name string, data []byte) {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		for name, data := range files {
			add(name, data)
		}
		add("manifest.json", []byte(manifest))
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "image.tar")
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write(t, `[{"Config":"`+id+`.json","RepoTags":["urgit-locked/busybox:sha256-x"],"Layers":["l.tar"]}]`, map[string][]byte{id + ".json": config, "l.tar": []byte("layer")})
	got, err := ArchiveImageID(good)
	if err != nil || got != "sha256:"+id {
		t.Fatalf("id = %q, %v", got, err)
	}
	_, layers, err := ArchiveIdentity(good)
	if err != nil || len(layers) != 2 || layers[0] != "sha256:aaaa" || layers[1] != "sha256:bbbb" {
		t.Fatalf("layers = %v, %v", layers, err)
	}
	nolayers := write(t, `[{"Config":"c.json"}]`, map[string][]byte{"c.json": []byte(`{"rootfs":{"diff_ids":[]}}`)})
	if _, err := ArchiveImageID(nolayers); err == nil {
		t.Fatal("a config without rootfs layers must be refused")
	}
	two := write(t, `[{"Config":"a.json"},{"Config":"b.json"}]`, map[string][]byte{"a.json": config, "b.json": config})
	if _, err := ArchiveImageID(two); err == nil {
		t.Fatal("two images in one archive must be refused")
	}
	missing := write(t, `[{"Config":"gone.json"}]`, map[string][]byte{"other.json": config})
	if _, err := ArchiveImageID(missing); err == nil {
		t.Fatal("a manifest naming an absent config must be refused")
	}
}
