package model

import "context"

// Message is a single entry in the conversation history.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant" | "tool_result"
	Content string `json:"content"`
}

// ToolDef describes a tool that the model may call.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolCall is a single tool invocation requested by the model.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// TokenUsage reports prompt and completion token counts.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CompletionRequest holds all parameters for a single model call.
type CompletionRequest struct {
	Messages       []Message      `json:"messages"`
	Tools          []ToolDef      `json:"tools,omitempty"`
	SystemPrompt   string         `json:"system_prompt,omitempty"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	Temperature    float64        `json:"temperature,omitempty"`
	ResponseSchema map[string]any `json:"response_schema,omitempty"` // for structured outputs
}

// CompletionResponse is the result of a single model call.
type CompletionResponse struct {
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	Usage      TokenUsage  `json:"usage"`
	ModelID    string      `json:"model_id"`
	StopReason string      `json:"stop_reason"` // "end_turn" | "tool_use" | "max_tokens"
}

// Model is the provider-agnostic interface for LLM completions.
type Model interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
