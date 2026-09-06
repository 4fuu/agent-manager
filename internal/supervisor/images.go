package supervisor

import (
	"context"
	"errors"
	"strings"

	"github.com/4fuu/agent-manager/internal/manager"
)

// Caller holds s.mu.
func (s *Supervisor) image(id string) *manager.ImageProfile {
	for i := range s.state.Images {
		if s.state.Images[i].ID == id {
			return &s.state.Images[i]
		}
	}
	return nil
}

func (s *Supervisor) DownloadImage(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errors.New("supervisor is closing")
	}
	p := s.image(id)
	if p == nil {
		return errors.New("image profile not found")
	}
	for other := range s.downloads {
		if q := s.image(other); q != nil && q.Image == p.Image && q.Archive == p.Archive {
			return errors.New("this image source already has an active download")
		}
	}
	p.Download = manager.ImageDownload{Status: "resolving"}
	copy := *p
	if err := s.save(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.downloads[id] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		digest, err := s.backend.DownloadImage(ctx, copy, func(update manager.ImageDownload) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if p := s.image(id); p != nil && ctx.Err() == nil {
				if update.Total == 0 {
					update.Completed, update.Total = p.Download.Completed, p.Download.Total
				}
				p.Download = update
			}
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.downloads, id)
		if p := s.image(id); p != nil {
			switch {
			case err == nil:
				p.Download.Status, p.Download.Digest = "ready", digest
			case ctx.Err() != nil:
				p.Download.Status, p.Download.Error = "cancelled", "Download cancelled. You can retry."
			default:
				p.Download.Status = "failed"
				text := strings.Map(func(r rune) rune {
					if r < 32 && r != '\n' {
						return -1
					}
					return r
				}, err.Error())
				p.Download.Error = string([]rune(text)[:min(len([]rune(text)), 2048)])
			}
			if saveErr := s.save(); saveErr != nil {
				if p = s.image(id); p != nil {
					p.Download.Status, p.Download.Error = "failed", "Could not persist download result: "+saveErr.Error()
				}
			}
		}
	}()
	return nil
}

func (s *Supervisor) CancelImageDownload(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel := s.downloads[id]
	if cancel == nil {
		return errors.New("no active download for this profile")
	}
	cancel()
	if p := s.image(id); p != nil {
		p.Download.Status = "cancelling"
	}
	return nil
}
