package adapter

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
)

type DockerRunner struct {
	config domain.DockerConfig
}

func NewDockerRunner(config domain.DockerConfig) *DockerRunner {
	return &DockerRunner{config: config}
}

func (d *DockerRunner) Run(ctx context.Context, params RunnerParams) (domain.AgentResult, error) {
	return domain.AgentResult{}, fmt.Errorf("docker sandbox mode is not yet implemented; use --no-sandbox to run on host")
}
