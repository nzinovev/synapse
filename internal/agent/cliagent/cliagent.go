package cliagent

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

const (
	FeedbackKindRejection = "rejection"
	FeedbackKindAnswer    = "answer"
	FeedbackKindAnswers   = "answers"
)

type CLIAgent struct {
	agentName string
	inner     adapter.AgentAdapter
}

func New(agentName string, inner adapter.AgentAdapter) (*CLIAgent, error) {
	if agentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if inner == nil {
		return nil, fmt.Errorf("inner adapter is required")
	}
	return &CLIAgent{
		agentName: agentName,
		inner:     inner,
	}, nil
}

func (a *CLIAgent) Name() string {
	return a.agentName
}

func (a *CLIAgent) Run(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
	params := domain.InvokeParams{
		AgentName:           a.agentName,
		TaskDescription:     input.Goal,
		WorkingDir:          input.WorkspacePath,
		ContextArtifacts:    contextArtifacts(input.PriorOutputs),
		RejectionFeedback:   rejectionFeedback(input.Feedback),
		OpenQuestionAnswers: openQuestionAnswers(input.Feedback),
		StageWorkdir:        input.StageWorkdir,
		StageID:             input.StageID,
		Gate:                input.Gate,
		PipelineName:        input.PipelineName,
		FixCycleCount:       input.FixCycleCount,
		PRIndex:             input.PRIndex,
		PreviousStdout:      input.PreviousStdout,
		PreviousStderr:      input.PreviousStderr,
		Model:               input.Model,
	}

	result, err := a.inner.Invoke(ctx, params)
	if err != nil {
		return agent.RunResult{}, err
	}

	status := agent.StatusFailed
	if result.Success {
		status = agent.StatusCompleted
	}

	return agent.RunResult{
		SchemaVersion:   agent.SchemaVersion,
		Status:          status,
		Artifacts:       artifactRefs(input.StageID, result.ArtifactsCreated),
		Stdout:          result.Stdout,
		Stderr:          result.Stderr,
		DurationSeconds: result.DurationSeconds,
	}, nil
}

func Register(registry *agent.AgentRegistry, agentName string, factory func(domain.AdapterConfig) (adapter.AgentAdapter, error)) error {
	if registry == nil {
		return fmt.Errorf("agent registry is required")
	}
	if factory == nil {
		return fmt.Errorf("adapter factory is required")
	}
	return registry.Register(agentName, func(cfg domain.AdapterConfig) (agent.Agent, error) {
		inner, err := factory(cfg)
		if err != nil {
			return nil, err
		}
		return New(agentName, inner)
	})
}

func RegisterClaudeCLI(registry *agent.AgentRegistry) error {
	return Register(registry, "claude_cli", func(cfg domain.AdapterConfig) (adapter.AgentAdapter, error) {
		return &adapter.ClaudeCliAdapter{
			AgentPromptsDir: cfg.AgentPromptsDir,
			ClaudeBinary:    cfg.ClaudeBinary,
		}, nil
	})
}

func RegisterCursorCLI(registry *agent.AgentRegistry) error {
	return Register(registry, "cursor_cli", func(cfg domain.AdapterConfig) (adapter.AgentAdapter, error) {
		return &adapter.CursorCliAdapter{
			AgentPromptsDir: cfg.AgentPromptsDir,
			AgentBinary:     cfg.AgentBinary,
			ExtraArgs:       cfg.CursorExtraArgs,
		}, nil
	})
}

func contextArtifacts(outputs []agent.PriorOutput) map[string][]string {
	if len(outputs) == 0 {
		return nil
	}
	artifacts := make(map[string][]string)
	for _, output := range outputs {
		for _, artifact := range output.Artifacts {
			artifacts[output.StageID] = append(artifacts[output.StageID], artifact.Path)
		}
	}
	if len(artifacts) == 0 {
		return nil
	}
	return artifacts
}

func rejectionFeedback(feedback *agent.FeedbackDetail) *string {
	if feedback == nil || feedback.Kind != FeedbackKindRejection {
		return nil
	}
	return &feedback.Text
}

func openQuestionAnswers(feedback *agent.FeedbackDetail) *string {
	if feedback == nil {
		return nil
	}
	if feedback.Kind != FeedbackKindAnswer && feedback.Kind != FeedbackKindAnswers {
		return nil
	}
	return &feedback.Text
}

func artifactRefs(stageID string, paths []string) []agent.ArtifactRef {
	if len(paths) == 0 {
		return nil
	}
	artifacts := make([]agent.ArtifactRef, 0, len(paths))
	for _, path := range paths {
		artifacts = append(artifacts, agent.ArtifactRef{
			Path:    path,
			StageID: stageID,
		})
	}
	return artifacts
}
