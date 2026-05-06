package native_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/agent/native"
	"github.com/nzinovev/synapse/internal/model"
	"github.com/nzinovev/synapse/internal/tool"
)

// helpers

func makeRegistry(t *testing.T, perm tool.ToolPermission) *tool.ToolRegistry {
	t.Helper()
	reg := tool.NewToolRegistry()
	if err := reg.Register(tool.NewReadFileTool(perm)); err != nil {
		t.Fatalf("register read_file: %v", err)
	}
	if err := reg.Register(tool.NewWriteFileTool(perm)); err != nil {
		t.Fatalf("register write_file: %v", err)
	}
	if err := reg.Register(tool.NewListFilesTool(perm)); err != nil {
		t.Fatalf("register list_files: %v", err)
	}
	return reg
}

func makeWorkdir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "native-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func resultJSON(status, summary string) string {
	b, _ := json.Marshal(map[string]any{
		"status":  status,
		"summary": summary,
	})
	return string(b)
}

// TestEndToEnd verifies that a NativeAgent completes a stage end-to-end:
// MockModel returns one tool_use response then a final end_turn JSON result.
func TestEndToEnd(t *testing.T) {
	workdir := makeWorkdir(t)
	// Create a file the agent will read.
	testFile := filepath.Join(workdir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	perm := tool.ToolPermission{
		AllowWrites:  true,
		WorkspaceDir: workdir,
	}
	reg := makeRegistry(t, perm)

	// Round 1: tool_use — ask to read the file
	// Round 2: end_turn — return JSON result
	mock := &model.MockModel{
		Responses: []model.CompletionResponse{
			{
				StopReason: "tool_use",
				Content:    "reading the file",
				ToolCalls: []model.ToolCall{
					{
						ID:    "call-1",
						Name:  "read_file",
						Input: map[string]any{"path": testFile},
					},
				},
				Usage: model.TokenUsage{InputTokens: 10, OutputTokens: 5},
			},
			{
				StopReason: "end_turn",
				Content:    resultJSON("completed", "read the file successfully"),
				Usage:      model.TokenUsage{InputTokens: 20, OutputTokens: 8},
			},
		},
	}

	def := agent.AgentDefinition{Name: "test-agent", Prompt: "you are a helpful agent"}
	a, err := native.New(def, mock, reg, perm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := agent.RunInput{
		Goal:         "read hello.txt",
		StageWorkdir: workdir,
	}
	result, err := a.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if result.Status != agent.StatusCompleted {
		t.Errorf("Status = %q, want %q", result.Status, agent.StatusCompleted)
	}
	if result.Summary != "read the file successfully" {
		t.Errorf("Summary = %q, want %q", result.Summary, "read the file successfully")
	}

	// ModelUsage should be populated and totalled across both rounds.
	if result.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if result.Usage.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 13 {
		t.Errorf("OutputTokens = %d, want 13", result.Usage.OutputTokens)
	}

	// Tool was actually executed — model should have received a tool_result message.
	if len(mock.Calls) != 2 {
		t.Errorf("model was called %d times, want 2", len(mock.Calls))
	}

	// result.json must have been written to StageWorkdir.
	saved, err := agent.ReadStageResult(workdir)
	if err != nil {
		t.Fatalf("ReadStageResult: %v", err)
	}
	if saved == nil {
		t.Fatal("result.json not written to StageWorkdir")
	}
	if saved.Status != agent.StatusCompleted {
		t.Errorf("saved Status = %q, want %q", saved.Status, agent.StatusCompleted)
	}
}

// TestBadJSONPayload verifies that an unparseable model response sets StatusFailed.
func TestBadJSONPayload(t *testing.T) {
	perm := tool.ToolPermission{}
	reg := tool.NewToolRegistry()

	mock := &model.MockModel{
		Responses: []model.CompletionResponse{
			{
				StopReason: "end_turn",
				Content:    "this is not json at all",
				Usage:      model.TokenUsage{InputTokens: 5, OutputTokens: 3},
			},
		},
	}

	def := agent.AgentDefinition{Name: "test-agent"}
	a, err := native.New(def, mock, reg, perm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := a.Run(context.Background(), agent.RunInput{Goal: "do something"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if result.Status != agent.StatusFailed {
		t.Errorf("Status = %q, want %q", result.Status, agent.StatusFailed)
	}
	if result.Summary == "" {
		t.Error("Summary should contain the parse error message")
	}
	if result.Usage == nil {
		t.Fatal("Usage is nil even on failure")
	}
}

// TestPermissionViolation verifies that a tool error (permission violation) is
// captured in the message history rather than panicking or propagating as a
// hard error. The run should continue and ultimately succeed or fail gracefully.
func TestPermissionViolation(t *testing.T) {
	workdir := makeWorkdir(t)

	// Build a permission that blocks writes.
	perm := tool.ToolPermission{
		AllowWrites:  false,
		WorkspaceDir: workdir,
	}
	reg := tool.NewToolRegistry()
	if err := reg.Register(tool.NewWriteFileTool(perm)); err != nil {
		t.Fatal(err)
	}

	// Round 1: try to write (will fail due to AllowWrites=false)
	// Round 2: model sees the error in tool_result, returns end_turn
	mock := &model.MockModel{
		Responses: []model.CompletionResponse{
			{
				StopReason: "tool_use",
				Content:    "trying to write",
				ToolCalls: []model.ToolCall{
					{
						ID:    "call-1",
						Name:  "write_file",
						Input: map[string]any{"path": filepath.Join(workdir, "out.txt"), "content": "data"},
					},
				},
				Usage: model.TokenUsage{InputTokens: 10, OutputTokens: 5},
			},
			{
				StopReason: "end_turn",
				Content:    resultJSON("failed", "write was blocked"),
				Usage:      model.TokenUsage{InputTokens: 15, OutputTokens: 6},
			},
		},
	}

	def := agent.AgentDefinition{Name: "test-agent"}
	a, err := native.New(def, mock, reg, perm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := a.Run(context.Background(), agent.RunInput{Goal: "write a file"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// The agent should not panic — the error is surfaced through the tool_result
	// message and the model returns an end_turn with "failed".
	if result.Status != agent.StatusFailed {
		t.Errorf("Status = %q, want %q (tool error propagated through messages)", result.Status, agent.StatusFailed)
	}

	// Model was called twice: once for the tool call, once after the error result.
	if len(mock.Calls) != 2 {
		t.Errorf("model called %d times, want 2", len(mock.Calls))
	}

	// The second call should contain a tool_result message carrying the error.
	secondCall := mock.Calls[1]
	var foundToolResult bool
	for _, msg := range secondCall.Messages {
		if msg.Role == "tool_result" {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Error("second model call did not contain a tool_result message")
	}
}

// TestContextCancellation verifies that context cancellation returns StatusFailed
// with "context cancelled" in Summary.
func TestContextCancellation(t *testing.T) {
	perm := tool.ToolPermission{}
	reg := tool.NewToolRegistry()

	ctx, cancel := context.WithCancel(context.Background())

	mock := &model.MockModel{
		RunFn: func(ctx context.Context, req model.CompletionRequest) (model.CompletionResponse, error) {
			// Cancel the context as if the caller signalled cancellation.
			cancel()
			// Return a response — the engine should detect ctx.Err() before processing.
			return model.CompletionResponse{
				StopReason: "end_turn",
				Content:    resultJSON("completed", "done"),
			}, nil
		},
	}

	def := agent.AgentDefinition{Name: "test-agent"}
	a, err := native.New(def, mock, reg, perm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := a.Run(ctx, agent.RunInput{Goal: "run forever"})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if result.Status != agent.StatusFailed {
		t.Errorf("Status = %q, want %q", result.Status, agent.StatusFailed)
	}
	if result.Summary != "context cancelled" {
		t.Errorf("Summary = %q, want %q", result.Summary, "context cancelled")
	}
}

// TestResultJSONRoundTrip verifies that result.json written by NativeAgent is
// readable back as a valid RunResult with all fields intact.
func TestResultJSONRoundTrip(t *testing.T) {
	workdir := makeWorkdir(t)
	perm := tool.ToolPermission{}
	reg := tool.NewToolRegistry()

	payload, _ := json.Marshal(map[string]any{
		"status":  "completed",
		"summary": "all done",
		"verdict": "APPROVED",
		"open_questions": []map[string]any{
			{"id": "q1", "text": "Is this OK?"},
		},
		"metadata": map[string]string{"key": "val"},
	})

	mock := &model.MockModel{
		Responses: []model.CompletionResponse{
			{
				StopReason: "end_turn",
				Content:    string(payload),
				Usage:      model.TokenUsage{InputTokens: 50, OutputTokens: 20},
			},
		},
	}

	def := agent.AgentDefinition{Name: "test-agent"}
	a, err := native.New(def, mock, reg, perm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := agent.RunInput{Goal: "produce a result", StageWorkdir: workdir}
	result, err := a.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify in-memory result fields.
	if result.Status != agent.StatusCompleted {
		t.Errorf("in-memory Status = %q, want %q", result.Status, agent.StatusCompleted)
	}
	if result.Verdict != agent.VerdictApproved {
		t.Errorf("in-memory Verdict = %q, want %q", result.Verdict, agent.VerdictApproved)
	}
	if len(result.OpenQuestions) != 1 || result.OpenQuestions[0].ID != "q1" {
		t.Errorf("OpenQuestions = %+v, want one entry with id=q1", result.OpenQuestions)
	}
	if result.Usage == nil || result.Usage.InputTokens != 50 {
		t.Errorf("Usage = %+v, want InputTokens=50", result.Usage)
	}

	// Verify round-trip through result.json.
	saved, err := agent.ReadStageResult(workdir)
	if err != nil {
		t.Fatalf("ReadStageResult: %v", err)
	}
	if saved == nil {
		t.Fatal("result.json not written")
	}
	if saved.Status != agent.StatusCompleted {
		t.Errorf("saved Status = %q, want %q", saved.Status, agent.StatusCompleted)
	}
	if saved.Verdict != agent.VerdictApproved {
		t.Errorf("saved Verdict = %q, want %q", saved.Verdict, agent.VerdictApproved)
	}
	if len(saved.OpenQuestions) != 1 || saved.OpenQuestions[0].Text != "Is this OK?" {
		t.Errorf("saved OpenQuestions = %+v", saved.OpenQuestions)
	}
	if saved.Usage == nil || saved.Usage.InputTokens != 50 {
		t.Errorf("saved Usage = %+v, want InputTokens=50", saved.Usage)
	}
	if v, ok := saved.Metadata["key"]; !ok || v != "val" {
		t.Errorf("saved Metadata = %+v, want key=val", saved.Metadata)
	}
}

// TestName verifies Name() returns the definition name or "native" as fallback.
func TestName(t *testing.T) {
	reg := tool.NewToolRegistry()
	perm := tool.ToolPermission{}
	mock := &model.MockModel{}

	t.Run("with name", func(t *testing.T) {
		def := agent.AgentDefinition{Name: "my-agent"}
		a, _ := native.New(def, mock, reg, perm)
		if a.Name() != "my-agent" {
			t.Errorf("Name() = %q, want %q", a.Name(), "my-agent")
		}
	})

	t.Run("without name fallback", func(t *testing.T) {
		def := agent.AgentDefinition{}
		a, _ := native.New(def, mock, reg, perm)
		if a.Name() != "native" {
			t.Errorf("Name() = %q, want %q", a.Name(), "native")
		}
	})
}
