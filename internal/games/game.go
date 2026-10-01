package games

import (
	"context"

	"github.com/example/game-server-control-plane/internal/model"
)

type Adapter interface {
	Name() string
	AppID() string
	Validate(model.GameServer, []model.ServerPort) error
	Prepare(context.Context, model.GameServer) error
	StartupArgs(model.GameServer) []string
	HealthAddress(model.GameServer, []model.ServerPort) string
}

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	items := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		items[adapter.Name()] = adapter
	}
	return &Registry{adapters: items}
}

func (r *Registry) Get(name string) (Adapter, bool) {
	adapter, ok := r.adapters[name]
	return adapter, ok
}
