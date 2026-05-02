package agent

import (
	"fmt"
	"sort"
	"sync"

	"github.com/nzinovev/synapse/internal/domain"
)

type AgentRegistry struct {
	mu        sync.RWMutex
	factories map[string]func(domain.AdapterConfig) (Agent, error)
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		factories: make(map[string]func(domain.AdapterConfig) (Agent, error)),
	}
}

func (r *AgentRegistry) Register(name string, factory func(domain.AdapterConfig) (Agent, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("agent %q already registered", name)
	}
	r.factories[name] = factory
	return nil
}

func (r *AgentRegistry) Create(name string, cfg domain.AdapterConfig) (Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, exists := r.factories[name]
	if !exists {
		return nil, fmt.Errorf("unknown agent %q; registered: %v", name, r.namesLocked())
	}
	return factory(cfg)
}

func (r *AgentRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.factories[name]
	return exists
}

func (r *AgentRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namesLocked()
}

func (r *AgentRegistry) namesLocked() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
