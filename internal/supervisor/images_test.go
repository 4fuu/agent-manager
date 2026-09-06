package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/4fuu/agent-manager/internal/backend"
	"github.com/4fuu/agent-manager/internal/manager"
)

type imageBackend struct {
	backend.Fake
	started chan struct{}
	finish  chan error
}

func (b *imageBackend) DownloadImage(ctx context.Context, _ manager.ImageProfile, report func(manager.ImageDownload)) (string, error) {
	report(manager.ImageDownload{Status: "downloading", Completed: 50, Total: 100})
	b.started <- struct{}{}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-b.finish:
		return "sha256:checked", err
	}
}

func TestImageDownloadLifecycleAndSourceIdentity(t *testing.T) {
	b := &imageBackend{started: make(chan struct{}, 8), finish: make(chan error, 8)}
	s, err := New(t.TempDir(), b)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := manager.ImageProfile{ID: "custom", Name: "My image", Image: "ghcr.io/example/agent:v1", Command: "agent"}
	if err = s.Image(p); err != nil {
		t.Fatal(err)
	}
	wait := func(status string) manager.ImageProfile {
		t.Helper()
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			for _, p := range s.State().Images {
				if p.ID == "custom" && p.Download.Status == status {
					return p
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("did not reach %s", status)
		return manager.ImageProfile{}
	}
	if err = s.DownloadImage(p.ID); err != nil {
		t.Fatal(err)
	}
	<-b.started
	wait("downloading")
	if err = s.DownloadImage(p.ID); err == nil {
		t.Fatal("duplicate download accepted")
	}
	if err = s.DeleteImage(p.ID); err == nil {
		t.Fatal("active profile deleted")
	}
	p.Image = "ghcr.io/example/agent:v2"
	if err = s.Image(p); err == nil {
		t.Fatal("active source replaced")
	}
	p.Image, p.Name = "ghcr.io/example/agent:v1", "Renamed"
	if err = s.Image(p); err != nil {
		t.Fatal(err)
	}
	if wait("downloading").Download.Completed != 50 {
		t.Fatal("edit reset progress")
	}
	if err = s.CancelImageDownload(p.ID); err != nil {
		t.Fatal(err)
	}
	wait("cancelled")
	if err = s.DownloadImage(p.ID); err != nil {
		t.Fatal(err)
	}
	<-b.started
	b.finish <- errors.New("registry unavailable")
	if got := wait("failed"); got.Download.Error != "registry unavailable" {
		t.Fatal(got)
	}
	if err = s.DownloadImage(p.ID); err != nil {
		t.Fatal(err)
	}
	<-b.started
	b.finish <- nil
	if got := wait("ready"); got.Download.Digest != "sha256:checked" || got.Name != "Renamed" {
		t.Fatal(got)
	}
	stored, err := manager.Load(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Images[len(stored.Images)-1].Download.Status != "ready" {
		t.Fatal("completion not persisted")
	}
	if len(stored.Instances) != 0 {
		t.Fatal("download created a session")
	}
	p.Image = "ghcr.io/example/agent:v2"
	if err = s.Image(p); err != nil {
		t.Fatal(err)
	}
	if wait("").Download.Digest != "" {
		t.Fatal("source change retained old result")
	}
	if err = s.DownloadImage(p.ID); err != nil {
		t.Fatal(err)
	}
	<-b.started
	s.Close()
	wait("cancelled")
	if err = s.DownloadImage(p.ID); err == nil {
		t.Fatal("download allowed after close")
	}
}

func TestInterruptedImageDownloadRecovery(t *testing.T) {
	dir := t.TempDir()
	st := manager.State{Version: manager.StateVersion, Images: manager.BuiltinImages()}
	st.Images[0].Download.Status = "importing"
	if err := manager.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, backend.Fake{Dir: filepath.Join(dir, "vms")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.State().Images[0].Download; got.Status != "interrupted" || got.Error == "" {
		t.Fatal(got)
	}
}
