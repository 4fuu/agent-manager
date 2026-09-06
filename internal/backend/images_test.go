package backend

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	ms "github.com/superradcompany/microsandbox/sdk/go"
)

func TestDownloadArchiveRegistryProgressCancellationAndPlatform(t *testing.T) {
	reg := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	blocked := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/blocked/") {
			blocked <- struct{}{}
			<-r.Context().Done()
			return
		}
		reg.ServeHTTP(w, r)
	}))
	defer server.Close()
	ref, _ := name.NewTag(strings.TrimPrefix(server.URL, "http://") + "/test:latest")
	image, err := random.Image(4096, 2)
	if err != nil {
		t.Fatal(err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	config.OS, config.Architecture = "linux", runtime.GOARCH
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Write(ref, image); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.tar")
	var last manager.ImageDownload
	if err = downloadArchive(context.Background(), ref.Name(), path, func(d manager.ImageDownload) { last = d }); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if last.Status != "downloading" || last.Completed != last.Total || last.Total != st.Size() {
		t.Fatal("wrong archive progress", last, st.Size())
	}
	loaded, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := image.ConfigName()
	got, _ := loaded.ConfigName()
	if got != want {
		t.Fatal("archive changed image config")
	}
	config.OS = "windows"
	wrong, _ := mutate.ConfigFile(image, config)
	if err = remote.Write(ref, wrong); err != nil {
		t.Fatal(err)
	}
	if err = downloadArchive(context.Background(), ref.Name(), filepath.Join(t.TempDir(), "wrong.tar"), func(manager.ImageDownload) {}); err == nil || !strings.Contains(err.Error(), "need linux/") {
		t.Fatal("wrong platform accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- downloadArchive(ctx, ref.RegistryStr()+"/blocked:latest", filepath.Join(t.TempDir(), "cancel.tar"), func(manager.ImageDownload) {})
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not return")
	}
}

func TestLiveRegistryDownloadWithoutSandbox(t *testing.T) {
	source := os.Getenv("AGENT_MANAGER_PULL_IMAGE")
	if os.Getenv("AGENT_MANAGER_LIVE_TEST") != "1" || source == "" {
		t.Skip("set live test and AGENT_MANAGER_PULL_IMAGE to a public registry reference")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	digest, err := (Microsandbox{}).DownloadImage(ctx, manager.ImageProfile{ID: "pull-check", Image: source}, func(d manager.ImageDownload) { t.Log(d.Status, d.Completed, d.Total) })
	if err != nil {
		t.Fatal(err)
	}
	image, err := ms.Image.Inspect(ctx, source)
	if err != nil || image.ManifestDigest() != digest {
		t.Fatal("import not available by original reference", digest, err)
	}
	ref, err := name.ParseReference(source)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := remote.Head(ref, remote.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	pinned := ref.Context().Digest(manifest.Digest.String()).Name()
	if _, err = (Microsandbox{}).DownloadImage(ctx, manager.ImageProfile{ID: "pinned-check", Image: pinned}, func(manager.ImageDownload) {}); err != nil {
		t.Fatal("pinned image pull", err)
	}
	if _, err = ms.Image.Inspect(ctx, pinned); err != nil {
		t.Fatal("pinned reference not imported", err)
	}
}
