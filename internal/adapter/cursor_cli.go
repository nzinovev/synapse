package adapter

import (
	"context"
	"fmt"
	"os"

	"github.com/nzinovev/synapse/internal/domain"
)

type CursorCliAdapter struct {
	AgentPromptsDir string
	AgentBinary     string
	ExtraArgs       []string
}

func (c *CursorCliAdapter) Name() string {
	return "cursor_cli"
}

func (c *CursorCliAdapter) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	var systemPrompt []byte
	if params.SystemPrompt != "" {
		systemPrompt = []byte(params.SystemPrompt)
	} else {
		promptFile := c.AgentPromptsDir + "/" + params.AgentName + ".md"
		var err error
		systemPrompt, err = os.ReadFile(promptFile)
		if err != nil {
			return domain.AgentResult{}, fmt.Errorf("read agent prompt file: %w", err)
		}
	}

	taskBody := BuildTaskPrompt(
		params.TaskDescription,
		params.WorkingDir,
		params.ContextArtifacts,
		params.RejectionFeedback,
		params.OpenQuestionAnswers,
		params.StageID,
		params.Gate,
		params.PipelineName,
		params.AgentName,
		params.FixCycleCount,
		params.PRIndex,
		params.PreviousStdout,
		params.PreviousStderr,
	)

	fullPrompt := fmt.Sprintf("# Agent role (%s)\n\n%s\n\n---\n\n%s", params.AgentName, string(systemPrompt), taskBody)

	cmd := []string{
		c.AgentBinary,
		"--print",
		"--trust",
		"--force",
		"--workspace", params.WorkingDir,
	}
	cmd = append(cmd, c.ExtraArgs...)
	if params.Model != "" {
		cmd = append(cmd, "--model", params.Model)
	}
	cmd = append(cmd, fullPrompt)

	result := RunCLICommand(ctx, cmd, params.WorkingDir, params.StageWorkdir)
	return result, nil
}

func RegisterCursorCLI(registry *AdapterRegistry) {
	registry.Register("cursor_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) {
		return &CursorCliAdapter{
			AgentPromptsDir: cfg.AgentPromptsDir,
			AgentBinary:     cfg.AgentBinary,
			ExtraArgs:       cfg.CursorExtraArgs,
		}, nil
	})
}
