package cli

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/engine"
	"github.com/nzinovev/synapse/internal/store"
	"github.com/spf13/cobra"
)

type Dependencies struct {
	Registry      *adapter.AdapterRegistry
	AgentRegistry *agent.AgentRegistry
	Version       string
}

func NewRootCmd(deps *Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:          "synapse",
		Short:        "Multi-agent pipeline orchestrator with human approval gates.",
		SilenceUsage: true,
		Version:      deps.Version,
	}

	root.AddCommand(
		newInitCmd(),
		newRunCmd(deps),
		newStatusCmd(deps),
		newApproveCmd(deps),
		newRejectCmd(deps),
		newAnswerCmd(deps),
		newRetryCmd(deps),
		newCancelCmd(deps),
		newListPipelinesCmd(deps),
		newShowPipelineCmd(deps),
		newWebCmd(deps),
		newMigrateCmd(),
	)

	return root
}

func loadConfig() (*domain.SynapseConfig, error) {
	cfg, err := domain.LoadConfigGlobal()
	if err != nil {
		return nil, fmt.Errorf("no synapse configuration found. Run `synapse init` first: %w", err)
	}
	return cfg, nil
}

func openStore(ctx context.Context, cfg *domain.SynapseConfig) (*store.SQLiteStore, error) {
	s, err := store.NewSQLiteStore(ctx, cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return s, nil
}

func createEngine(ctx context.Context, cfg *domain.SynapseConfig, s *store.SQLiteStore, deps *Dependencies) (*engine.PipelineEngine, error) {
	return engine.NewPipelineEngineWithRegistry(s, deps.AgentRegistry, deps.Registry, cfg.AdapterConfig, cfg.Adapter, cfg.PipelinesDir), nil
}

func validateAdapterName(deps *Dependencies, adapterName string) error {
	if !deps.Registry.Has(adapterName) {
		return fmt.Errorf("unknown adapter %q; available: %v", adapterName, deps.Registry.SelectableNames())
	}
	return nil
}
