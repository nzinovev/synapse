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
	TaskID        string
	StageID       string
	PipelineName  string
	PromptDir     string
	EnvVars       map[string]string
}
