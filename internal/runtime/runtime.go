package runtime

import (
	"context"
	"io"

	"github.com/example/game-server-control-plane/internal/model"
)

type PortMapping struct {
	HostPort   int
	TargetPort int
	Protocol   string
}

type Spec struct {
	ID          string
	Image       string
	InstallPath string
	Binary      string
	Args        []string
	Environment map[string]string
	Ports       []PortMapping
	CPUQuota    int64
	MemoryBytes int64
}

type Runtime interface {
	Create(context.Context, Spec) error
	Delete(context.Context, string) error
	Start(context.Context, string) error
	Stop(context.Context, string, bool) error
	Restart(context.Context, string) error
	Status(context.Context, string) (model.RuntimeStatus, error)
	Logs(context.Context, string, int, bool) (io.ReadCloser, error)
}
