package backend

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/privatefs"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	ms "github.com/superradcompany/microsandbox/sdk/go"
)

// The pinned runtime has no standalone Go pull API. Stream a verified registry
// image to a temporary Docker archive, then import it without creating a VM.
func (Microsandbox) DownloadImage(ctx context.Context, p manager.ImageProfile, report func(manager.ImageDownload)) (string, error) {
	archive := p.Archive
	tag := "agent-manager-profile:" + p.ID
	if archive == "" {
		dir, err := os.MkdirTemp("", "agent-manager-pull-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		if err = privatefs.EnsureDir(dir); err != nil {
			return "", err
		}
		archive = filepath.Join(dir, "image.tar")
		if err = downloadArchive(ctx, p.Image, archive, report); err != nil {
			return "", err
		}
		tag = p.Image
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	report(manager.ImageDownload{Status: "importing"})
	images, err := ms.Image.Load(ctx, archive, tag)
	if err != nil {
		return "", fmt.Errorf("import into microsandbox: %w", err)
	}
	if len(images) == 0 {
		return "", fmt.Errorf("archive contains no compatible Linux image")
	}
	return images[0].ManifestDigest(), nil
}

func downloadArchive(ctx context.Context, source, path string, report func(manager.ImageDownload)) error {
	report(manager.ImageDownload{Status: "resolving"})
	ref, err := name.ParseReference(source)
	if err != nil {
		return fmt.Errorf("invalid OCI reference: %w", err)
	}
	image, err := remote.Image(ref, remote.WithContext(ctx), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("resolve registry image: %w", err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		return err
	}
	if config.OS != "linux" || config.Architecture != runtime.GOARCH {
		return fmt.Errorf("image is %s/%s; need linux/%s", config.OS, config.Architecture, runtime.GOARCH)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	updates := make(chan v1.Update, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		last := time.Time{}
		for u := range updates {
			if time.Since(last) >= 150*time.Millisecond || u.Error == io.EOF {
				report(manager.ImageDownload{Status: "downloading", Completed: u.Complete, Total: u.Total})
				last = time.Now()
			}
		}
	}()
	err = tarball.Write(ref, image, f, tarball.WithProgress(updates))
	close(updates)
	<-done
	if err != nil {
		return fmt.Errorf("download image archive: %w", err)
	}
	return f.Close()
}
