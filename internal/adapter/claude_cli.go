package adapter

import (
	"context"
	"fmt"
	"os"

	"github.com/nzinovev/synapse/internal/domain"
)

type ClaudeCliAdapter struct {
	AgentPromptsDir string
	ClaudeBinary    string
	Runner          Runner
}

func (c *ClaudeCliAdapter) Name() string {
	return "claude_cli"
}

func (c *ClaudeCliAdapter) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	promptFile := c.AgentPromptsDir + "/" + params.AgentName + ".md"
	systemPrompt, err := os.ReadFile(promptFile)
	if err != nil {
		return domain.AgentResult{}, fmt.Errorf("read agent prompt file: %w", err)
	}

	userMessage := BuildTaskPrompt(
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

	cmd := []string{
		c.ClaudeBinary,
		"--print",
		"--dangerously-skip-permissions",
		"--append-system-prompt", string(systemPrompt),
	}
	if params.Model != "" {
		cmd = append(cmd, "--model", params.Model)
	}
	cmd = append(cmd, userMessage)

	return c.Runner.Run(ctx, RunnerParams{
		Command:      cmd,
		WorkingDir:   params.WorkingDir,
		StageWorkdir: params.StageWorkdir,
		AgentName:    params.AgentName,
		TaskID:       params.TaskID,
		StageID:      params.StageID,
		PipelineName: params.PipelineName,
		PromptDir:    c.AgentPromptsDir,
	})
}

func RegisterClaudeCLI(registry *AdapterRegistry, runner Runner) {
	registry.Register("claude_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) {
		return &ClaudeCliAdapter{
			AgentPromptsDir: cfg.AgentPromptsDir,
			ClaudeBinary:    cfg.ClaudeBinary,
			Runner:          runner,
		}, nil
	})
}
