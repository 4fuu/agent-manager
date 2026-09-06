// Package backend owns runtime images and execution inside guests, never host command fallbacks.
package backend

import (
	"context"
	"errors"
	"io"

	"github.com/4fuu/agent-manager/internal/manager"
)

var ErrNotFound = errors.New("runtime instance not found")

type Process interface {
	Input([]byte) error
	Resize(rows, cols int) error
	Wait() error
	Close() error
}
type VM interface {
	ID() string
	Run(context.Context, string, string, io.Writer) error
	Terminal(context.Context, string, string, io.Writer) (Process, error)
	Stop(context.Context) error
	Destroy(context.Context) error
	Release() error
}
type Backend interface {
	DownloadImage(context.Context, manager.ImageProfile, func(manager.ImageDownload)) (string, error)
	Create(context.Context, string, manager.Project) (VM, error)
	Open(context.Context, string, string, bool) (VM, error)
	Inspect(context.Context, string, string) (string, error)
	Control(context.Context, string, string, bool) error // true deletes; false stops, never boots
}
