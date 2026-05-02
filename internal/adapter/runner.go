package adapter

import (
	"context"

	"github.com/nzinovev/synapse/internal/domain"
)

type Runner interface {
	Run(ctx context.Context, params RunnerParams) (domain.AgentResult, error)
}

type RunnerParams struct {
	Command       []string
	WorkingDir    string
	StageWorkdir  string
	AgentName     string
	SandboxConfig domain.DockerConfig
	EnvVars       map[string]string
	PromptFiles   map[string]string
}
