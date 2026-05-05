package model

import (
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
)

// TierResolver resolves a named tier ("low", "medium", "high") to a Model.
type TierResolver interface {
	Resolve(tier string) (Model, error)
}

// ConfigTierResolver implements TierResolver by reading tier→model-name
// mappings from domain.AdapterConfig.ModelTiers and delegating model
// construction to a caller-supplied factory.
type ConfigTierResolver struct {
	tiers   map[string]string
	factory func(modelName string) (Model, error)
}

// NewConfigTierResolver creates a ConfigTierResolver backed by the tier map
// in cfg and the provided factory function.
func NewConfigTierResolver(cfg domain.AdapterConfig, factory func(string) (Model, error)) *ConfigTierResolver {
	tiers := make(map[string]string, len(cfg.ModelTiers))
	for k, v := range cfg.ModelTiers {
		tiers[k] = v
	}
	return &ConfigTierResolver{
		tiers:   tiers,
		factory: factory,
	}
}

// Resolve maps tier to a model name and invokes the factory.
// Returns an error if the tier is unknown or the factory fails.
func (r *ConfigTierResolver) Resolve(tier string) (Model, error) {
	modelName, ok := r.tiers[tier]
	if !ok {
		return nil, fmt.Errorf("unknown model tier %q", tier)
	}
	return r.factory(modelName)
}
