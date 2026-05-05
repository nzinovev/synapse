package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/model"
	"github.com/nzinovev/synapse/internal/tool"
)

// MaxToolIterations is the maximum number of tool-call / completion cycles
// before the agent gives up and returns a failed result.
const MaxToolIterations = 25

// NativeAgent is an agent that calls a Model directly, executes tool calls,
// and returns a structured RunResult — no subprocess.
type NativeAgent struct {
	definition agent.AgentDefinition
	model      model.Model
	tools      *tool.ToolRegistry
	perm       tool.ToolPermission
}

// New constructs a NativeAgent. Returns an error if any required parameter is nil.
func New(def agent.AgentDefinition, m model.Model, tools *tool.ToolRegistry, perm tool.ToolPermission) (*NativeAgent, error) {
	if m == nil {
		return nil, fmt.Errorf("native.New: model must not be nil")
	}
	if tools == nil {
		return nil, fmt.Errorf("native.New: tool registry must not be nil")
	}
	return &NativeAgent{
		definition: def,
		model:      m,
		tools:      tools,
		perm:       perm,
	}, nil
}

// Name returns the agent's name from its definition, falling back to "native".
func (a *NativeAgent) Name() string {
	if a.definition.Name != "" {
		return a.definition.Name
	}
	return "native"
}

// Run executes the agent loop: build messages, call model, execute tool calls,
// repeat until end_turn or MaxToolIterations exhausted.
func (a *NativeAgent) Run(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
	messages := buildMessages(input, a.definition)

	// Build tool definitions from the registry.
	toolList := a.tools.List()
	toolDefs := make([]model.ToolDef, 0, len(toolList))
	for _, t := range toolList {
		toolDefs = append(toolDefs, model.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}

	var usage agent.ModelUsage
	var result agent.RunResult

	for i := 0; i < MaxToolIterations; i++ {
		// Check for context cancellation before each call.
		if ctx.Err() != nil {
			return agent.RunResult{
				SchemaVersion: agent.SchemaVersion,
				Status:        agent.StatusFailed,
				Summary:       "context cancelled",
			}, nil
		}

		req := model.CompletionRequest{
			Messages: messages,
			Tools:    toolDefs,
		}

		resp, err := a.model.Complete(ctx, req)
		if err != nil {
			// If the context was cancelled, surface a clean error.
			if ctx.Err() != nil {
				return agent.RunResult{
					SchemaVersion: agent.SchemaVersion,
					Status:        agent.StatusFailed,
					Summary:       "context cancelled",
				}, nil
			}
			return agent.RunResult{}, fmt.Errorf("model completion: %w", err)
		}

		// Check for cancellation after the call returns (the model may have
		// returned a response concurrently with context cancellation).
		if ctx.Err() != nil {
			return agent.RunResult{
				SchemaVersion: agent.SchemaVersion,
				Status:        agent.StatusFailed,
				Summary:       "context cancelled",
			}, nil
		}

		// Accumulate token usage.
		usage.InputTokens += resp.Usage.InputTokens
		usage.OutputTokens += resp.Usage.OutputTokens
		usage.Model = resp.ModelID

		switch resp.StopReason {
		case "tool_use":
			// Append assistant message with the tool call content.
			messages = append(messages, model.Message{
				Role:    "assistant",
				Content: resp.Content,
			})

			// Execute each requested tool call and collect results.
			var toolResults []string
			for _, call := range resp.ToolCalls {
				toolResult, toolErr := a.executeTool(ctx, call)
				if toolErr != nil {
					toolResults = append(toolResults, fmt.Sprintf("[tool:%s error] %s", call.Name, toolErr.Error()))
				} else {
					resultJSON, jsonErr := json.Marshal(toolResult)
					if jsonErr != nil {
						toolResults = append(toolResults, fmt.Sprintf("[tool:%s] (marshal error: %s)", call.Name, jsonErr.Error()))
					} else {
						toolResults = append(toolResults, fmt.Sprintf("[tool:%s] %s", call.Name, string(resultJSON)))
					}
				}
			}

			// Append combined tool results as a single user message.
			messages = append(messages, model.Message{
				Role:    "tool_result",
				Content: strings.Join(toolResults, "\n"),
			})
			// Continue the loop to get the model's next response.

		case "end_turn":
			// Parse the final content as a RunResult-shaped JSON payload.
			result = parseRunResult(resp.Content)
			result.Usage = &usage
			// Write result.json before returning so the engine's fallback write is a no-op.
			if input.StageWorkdir != "" {
				agent.WriteStageResult(input.StageWorkdir, result) //nolint:errcheck
			}
			return result, nil

		default:
			// Unexpected stop reason (e.g. "max_tokens") — treat as failure.
			result = agent.RunResult{
				SchemaVersion: agent.SchemaVersion,
				Status:        agent.StatusFailed,
				Summary:       fmt.Sprintf("unexpected stop reason: %q", resp.StopReason),
				Usage:         &usage,
			}
			if input.StageWorkdir != "" {
				agent.WriteStageResult(input.StageWorkdir, result) //nolint:errcheck
			}
			return result, nil
		}
	}

	// Exhausted MaxToolIterations without reaching end_turn.
	result = agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusFailed,
		Summary:       fmt.Sprintf("exceeded maximum tool iterations (%d)", MaxToolIterations),
		Usage:         &usage,
	}
	if input.StageWorkdir != "" {
		agent.WriteStageResult(input.StageWorkdir, result) //nolint:errcheck
	}
	return result, nil
}

// executeTool runs a single tool call, returning the result map or an error.
func (a *NativeAgent) executeTool(ctx context.Context, call model.ToolCall) (map[string]any, error) {
	t, err := a.tools.Get(call.Name)
	if err != nil {
		return nil, fmt.Errorf("unknown tool %q: %w", call.Name, err)
	}
	return t.Execute(ctx, call.Input)
}

// parseRunResult attempts to decode content as a JSON RunResult payload.
// On failure it returns a StatusFailed result with the parse error in Summary.
func parseRunResult(content string) agent.RunResult {
	// The model may wrap JSON in a markdown code fence — strip it.
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		// Remove opening fence (```json or ```)
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		// Remove closing fence
		if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
	}

	var payload struct {
		Status        string              `json:"status"`
		Summary       string              `json:"summary"`
		Verdict       string              `json:"verdict"`
		OpenQuestions []agent.Question    `json:"open_questions"`
		Metadata      map[string]string   `json:"metadata"`
		Artifacts     []agent.ArtifactRef `json:"artifacts"`
	}

	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return agent.RunResult{
			SchemaVersion: agent.SchemaVersion,
			Status:        agent.StatusFailed,
			Summary:       fmt.Sprintf("failed to parse model output as RunResult: %s", err.Error()),
		}
	}

	status := agent.StageStatus(payload.Status)
	switch status {
	case agent.StatusCompleted, agent.StatusNeedsInput, agent.StatusFailed:
		// valid
	default:
		status = agent.StatusCompleted // default to completed if unrecognised
	}

	return agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        status,
		Summary:       payload.Summary,
		Verdict:       agent.Verdict(payload.Verdict),
		OpenQuestions: payload.OpenQuestions,
		Metadata:      payload.Metadata,
		Artifacts:     payload.Artifacts,
	}
}
