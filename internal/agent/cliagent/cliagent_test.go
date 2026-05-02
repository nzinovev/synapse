package cliagent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

type stubAdapter struct {
	name       string
	result     domain.AgentResult
	err        error
	lastParams domain.InvokeParams
	calls      int
}

func (s *stubAdapter) Name() string {
	return s.name
}

func (s *stubAdapter) Invoke(_ context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	s.calls++
	s.lastParams = params
	return s.result, s.err
}

func TestCLIAgentRunTranslatesInputAndResult(t *testing.T) {
	exitCode := 0
	previousStdout := "previous stdout"
	previousStderr := "previous stderr"
	feedback := &agent.FeedbackDetail{Kind: FeedbackKindRejection, Text: "fix the naming"}
	stub := &stubAdapter{
		name: "stub_cli",
		result: domain.AgentResult{
			Success:          true,
			Stdout:           "agent stdout",
			Stderr:           "agent stderr",
			ArtifactsCreated: []string{"docs/specs/foo.md", "docs/adr/0004-foo.md"},
			DurationSeconds:  12.5,
			ExitCode:         &exitCode,
		},
	}
	cliAgent, err := New("spec-writer", stub)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := agent.RunInput{
		SchemaVersion: agent.SchemaVersion,
		TaskID:        "task-001",
		TaskNumber:    "001",
		Goal:          "write the spec",
		WorkspacePath: "/work/project",
		StageWorkdir:  "/work/project/.synapse/tasks/task-001/stages/spec/attempt-1",
		StageID:       "spec",
		PipelineName:  "backend",
		Gate:          domain.GateHumanApproval,
		PRIndex:       2,
		FixCycleCount: 3,
		PriorOutputs: []agent.PriorOutput{
			{
				StageID: "adr",
				Artifacts: []agent.ArtifactRef{
					{Path: "docs/adr/0001-choice.md", StageID: "adr", Description: "ADR"},
					{Path: "docs/adr/0002-choice.md", StageID: "adr", Description: "ADR"},
				},
				Summary: "ADR written",
			},
			{
				StageID: "review",
				Artifacts: []agent.ArtifactRef{
					{Path: "docs/reviews/spec-review.md", StageID: "review"},
				},
			},
		},
		Feedback:       feedback,
		PreviousStdout: &previousStdout,
		PreviousStderr: &previousStderr,
		Model:          "claude-sonnet-4-6",
		Metadata:       map[string]string{"ignored": "for now"},
	}

	got, err := cliAgent.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if stub.calls != 1 {
		t.Fatalf("Invoke calls = %d, want 1", stub.calls)
	}
	wantParams := domain.InvokeParams{
		AgentName:       "spec-writer",
		TaskDescription: "write the spec",
		WorkingDir:      "/work/project",
		ContextArtifacts: map[string][]string{
			"adr":    {"docs/adr/0001-choice.md", "docs/adr/0002-choice.md"},
			"review": {"docs/reviews/spec-review.md"},
		},
		RejectionFeedback: feedbackText("fix the naming"),
		StageWorkdir:      "/work/project/.synapse/tasks/task-001/stages/spec/attempt-1",
		StageID:           "spec",
		Gate:              domain.GateHumanApproval,
		PipelineName:      "backend",
		FixCycleCount:     3,
		PRIndex:           2,
		PreviousStdout:    &previousStdout,
		PreviousStderr:    &previousStderr,
		Model:             "claude-sonnet-4-6",
	}
	if !reflect.DeepEqual(stub.lastParams, wantParams) {
		t.Fatalf("InvokeParams mismatch\ngot:  %+v\nwant: %+v", stub.lastParams, wantParams)
	}

	want := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
		Artifacts: []agent.ArtifactRef{
			{Path: "docs/specs/foo.md", StageID: "spec"},
			{Path: "docs/adr/0004-foo.md", StageID: "spec"},
		},
		Stdout:          "agent stdout",
		Stderr:          "agent stderr",
		DurationSeconds: 12.5,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RunResult mismatch\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestCLIAgentRunMapsAnswersFeedback(t *testing.T) {
	stub := &stubAdapter{result: domain.AgentResult{Success: true}}
	cliAgent, err := New("fixer", stub)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := agent.RunInput{
		Feedback: &agent.FeedbackDetail{Kind: FeedbackKindAnswer, Text: "Use option B"},
	}
	if _, err := cliAgent.Run(context.Background(), input); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if stub.lastParams.RejectionFeedback != nil {
		t.Fatalf("RejectionFeedback = %v, want nil", stub.lastParams.RejectionFeedback)
	}
	if stub.lastParams.OpenQuestionAnswers == nil || *stub.lastParams.OpenQuestionAnswers != "Use option B" {
		t.Fatalf("OpenQuestionAnswers = %v, want %q", stub.lastParams.OpenQuestionAnswers, "Use option B")
	}
}

func TestCLIAgentRunFailedResult(t *testing.T) {
	stub := &stubAdapter{
		result: domain.AgentResult{
			Success:         false,
			Stdout:          "partial",
			Stderr:          "failed",
			DurationSeconds: 1.25,
		},
	}
	cliAgent, err := New("reviewer", stub)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := cliAgent.Run(context.Background(), agent.RunInput{StageID: "review"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Status != agent.StatusFailed {
		t.Fatalf("Status = %q, want %q", got.Status, agent.StatusFailed)
	}
	if got.SchemaVersion != agent.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", got.SchemaVersion, agent.SchemaVersion)
	}
	if got.Stdout != "partial" || got.Stderr != "failed" || got.DurationSeconds != 1.25 {
		t.Fatalf("logs/duration not copied: %+v", got)
	}
}

func TestCLIAgentRunReturnsInvokeError(t *testing.T) {
	wantErr := errors.New("boom")
	stub := &stubAdapter{err: wantErr}
	cliAgent, err := New("impl", stub)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = cliAgent.Run(context.Background(), agent.RunInput{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
}

func TestRegisterCreatesCLIAgent(t *testing.T) {
	registry := agent.NewAgentRegistry()
	stub := &stubAdapter{name: "stub_cli"}
	err := Register(registry, "spec-writer", func(cfg domain.AdapterConfig) (adapter.AgentAdapter, error) {
		if cfg.AgentPromptsDir != "prompts" {
			t.Fatalf("AgentPromptsDir = %q, want %q", cfg.AgentPromptsDir, "prompts")
		}
		return stub, nil
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	created, err := registry.Create("spec-writer", domain.AdapterConfig{AgentPromptsDir: "prompts"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name() != "spec-writer" {
		t.Fatalf("Name = %q, want %q", created.Name(), "spec-writer")
	}
	if _, ok := created.(*CLIAgent); !ok {
		t.Fatalf("created agent type = %T, want *CLIAgent", created)
	}
}

func TestRegisterBuiltInCLIAdapters(t *testing.T) {
	registry := agent.NewAgentRegistry()
	if err := RegisterClaudeCLI(registry); err != nil {
		t.Fatalf("RegisterClaudeCLI: %v", err)
	}
	if err := RegisterCursorCLI(registry); err != nil {
		t.Fatalf("RegisterCursorCLI: %v", err)
	}
	names := registry.Names()
	want := []string{"claude_cli", "cursor_cli"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Names = %v, want %v", names, want)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("", &stubAdapter{}); err == nil {
		t.Fatal("New with empty agent name returned nil error")
	}
	if _, err := New("agent", nil); err == nil {
		t.Fatal("New with nil adapter returned nil error")
	}
}

func feedbackText(text string) *string {
	return &text
}
