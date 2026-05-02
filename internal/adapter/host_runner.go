package adapter

import (
	"context"

	"github.com/nzinovev/synapse/internal/domain"
)

type HostRunner struct{}

func (h *HostRunner) Run(ctx context.Context, params RunnerParams) (domain.AgentResult, error) {
	return RunCLICommand(ctx, params.Command, params.WorkingDir, params.StageWorkdir), nil
}
