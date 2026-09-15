package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDockerLiveBoundary drives the docker-rootless backend against a real
// rootless daemon (URGIT_DOCKER_HOST, the config's docker_host: never the
// host's rootful socket) through the interface and nothing else: Prepare
// creates the labeled network, volume and runner container; Copy is the
// only way in (a byte-exact copy, NUL included); Orphans lists the labeled
// sandbox; Run hands back the stream and the exit code; Destroy removes
// it and Orphans no longer lists it. Skipped without the socket, so
// `go test ./...` stays hermetic; foreground.sh sets it.
func TestDockerLiveBoundary(t *testing.T) {
	host := os.Getenv("URGIT_DOCKER_HOST")
	if host == "" {
		t.Skip("set URGIT_DOCKER_HOST to a dedicated rootless socket (unix:///run/user/…/docker.sock)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	d, err := NewDocker(host)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ci-0v%x", time.Now().UnixNano())
	h, err := d.Prepare(ctx, Spec{Image: "catthehacker/ubuntu:act-latest", CPUs: 1, MemoryMiB: 256, DiskMiB: 64, Network: name})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Destroy(context.Background(), h)
	if h.ID != name || h.Network != name {
		t.Fatalf("handle %+v does not carry the attempt's network name %q", h, name)
	}
	input := t.TempDir()
	if err = os.WriteFile(filepath.Join(input, "marker"), []byte("copied input\x00\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = d.Copy(ctx, h, input+"/.", "/work/repo"); err != nil {
		t.Fatal(err)
	}
	ids, err := d.Orphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == h.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("labeled sandbox %s is absent from the orphan inventory %v", h.ID, ids)
	}
	stream, exit, err := d.Run(ctx, h, "/work/repo", []string{"cat", "marker"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream)
	stream.Close()
	code := <-exit
	if err != nil || code != 0 || string(data) != "copied input\x00\n" {
		t.Fatalf("data=%q code=%d err=%v", data, code, err)
	}
	if err = d.Destroy(ctx, h); err != nil {
		t.Fatal(err)
	}
	ids, err = d.Orphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == h.ID {
			t.Fatalf("sandbox %s survived teardown", h.ID)
		}
	}
}
