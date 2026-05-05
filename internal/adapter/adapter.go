package adapter

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
)

type AgentAdapter interface {
	Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error)
	Name() string
}

type AdapterRegistry struct {
	factories map[string]func(domain.AdapterConfig) (AgentAdapter, error)
}

func NewRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		factories: make(map[string]func(domain.AdapterConfig) (AgentAdapter, error)),
	}
}

func (r *AdapterRegistry) Register(name string, factory func(domain.AdapterConfig) (AgentAdapter, error)) {
	r.factories[name] = factory
}

func (r *AdapterRegistry) Create(name string, config domain.AdapterConfig) (AgentAdapter, error) {
	factory, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q", name)
	}
	return factory(config)
}

func (r *AdapterRegistry) Names() []string {
	names := make([]string, 0, len(r.factories))
	for k := range r.factories {
		names = append(names, k)
	}
	return names
}

func (r *AdapterRegistry) SelectableNames() []string {
	names := make([]string, 0, len(r.factories))
	for k := range r.factories {
		names = append(names, k)
	}
	return names
}

func (r *AdapterRegistry) Has(name string) bool {
	_, ok := r.factories[name]
	return ok
}
