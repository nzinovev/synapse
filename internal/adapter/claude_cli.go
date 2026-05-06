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
}

func (c *ClaudeCliAdapter) Name() string {
	return "claude_cli"
}

func (c *ClaudeCliAdapter) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
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

	result := RunCLICommand(ctx, cmd, params.WorkingDir, params.StageWorkdir)
	return result, nil
}

func RegisterClaudeCLI(registry *AdapterRegistry) {
	registry.Register("claude_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) {
		return &ClaudeCliAdapter{
			AgentPromptsDir: cfg.AgentPromptsDir,
			ClaudeBinary:    cfg.ClaudeBinary,
		}, nil
	})
}
