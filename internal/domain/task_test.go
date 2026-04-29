package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestTaskStatusValues(t *testing.T) {
	statuses := map[string]TaskStatus{
		"pending":       StatusPending,
		"running":       StatusRunning,
		"awaiting_gate": StatusAwaitingGate,
		"done":          StatusDone,
		"blocked":       StatusBlocked,
		"escalated":     StatusEscalated,
		"cancelled":     StatusCancelled,
	}
	for want, got := range statuses {
		if string(got) != want {
			t.Errorf("TaskStatus = %q, want %q", got, want)
		}
	}
}

func TestRunTriggerValues(t *testing.T) {
	triggers := map[string]RunTrigger{
		"initial":   TriggerInitial,
		"rejection": TriggerRejection,
		"retry":     TriggerRetry,
	}
	for want, got := range triggers {
		if string(got) != want {
			t.Errorf("RunTrigger = %q, want %q", got, want)
		}
	}
}

func TestTaskJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	task := Task{
		ID:             "task-001",
		PipelineName:   "backend",
		Description:    "Add logging",
		WorkingDir:     "/tmp/work",
		CurrentStageID: "review",
		Status:         StatusRunning,
		CreatedAt:      now,
		UpdatedAt:      now,
		Runs: []StageRun{
			{
				StageID:   "spec",
				Attempt:   1,
				Trigger:   TriggerInitial,
				StartedAt: now,
				AgentResult: &AgentResult{
					Success:          true,
					Stdout:           "output",
					Stderr:           "",
					ArtifactsCreated: []string{"docs/specs/001.md"},
					DurationSeconds:  1.5,
					ExitCode:         nil,
				},
			},
		},
		Artifacts:     map[string][]string{"spec": {"docs/specs/001.md"}},
		FixCycleCount: 0,
		PRIndex:       1,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != task.ID {
		t.Errorf("ID = %q, want %q", got.ID, task.ID)
	}
	if got.Status != task.Status {
		t.Errorf("Status = %q, want %q", got.Status, task.Status)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("Runs = %d, want 1", len(got.Runs))
	}
	if got.Runs[0].StageID != "spec" {
		t.Errorf("Runs[0].StageID = %q, want spec", got.Runs[0].StageID)
	}
	if got.Runs[0].AgentResult == nil {
		t.Fatal("Runs[0].AgentResult is nil")
	}
	if !got.Runs[0].AgentResult.Success {
		t.Error("Runs[0].AgentResult.Success = false, want true")
	}
	if len(got.Artifacts["spec"]) != 1 {
		t.Errorf("Artifacts[spec] = %v, want [docs/specs/001.md]", got.Artifacts["spec"])
	}

	// Test nil artifacts serializes as {}
	task2 := Task{ID: "t2", Artifacts: nil}
	data2, _ := json.Marshal(task2)
	s2 := string(data2)
	if !containsStr(s2, `"artifacts":{}`) {
		t.Errorf("nil Artifacts should serialize as {}, got %s", s2)
	}
}

func TestStageRunWithRejection(t *testing.T) {
	now := time.Now().UTC()
	sr := StageRun{
		StageID:           "fix",
		Attempt:           2,
		Trigger:           TriggerRejection,
		StartedAt:         now,
		RejectionFeedback: strPtr("needs work"),
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got StageRun
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RejectionFeedback == nil || *got.RejectionFeedback != "needs work" {
		t.Errorf("RejectionFeedback = %v, want 'needs work'", got.RejectionFeedback)
	}
}

func TestAgentResultWithExitCode(t *testing.T) {
	code := 1
	ar := AgentResult{
		Success:         false,
		Stdout:          "",
		Stderr:          "error",
		DurationSeconds: 0.5,
		ExitCode:        &code,
	}
	data, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ExitCode == nil || *got.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", got.ExitCode)
	}
	if got.Success {
		t.Error("Success = true, want false")
	}
}

func TestInvokeParams(t *testing.T) {
	feedback := "fix the bug"
	stdout := "prev out"
	stderr := "prev err"

	p := InvokeParams{
		AgentName:         "spec-writer",
		TaskDescription:   "Write a spec for X",
		WorkingDir:        "/tmp/work",
		ContextArtifacts:  map[string][]string{"spec": {"docs/specs/001.md"}},
		RejectionFeedback: &feedback,
		StageWorkdir:      "/tmp/work/.synapse/tasks/t1/stages/spec/attempt-1",
		StageID:           "spec",
		Gate:              GateAuto,
		PipelineName:      "backend",
		FixCycleCount:     0,
		PRIndex:           1,
		PreviousStdout:    &stdout,
		PreviousStderr:    &stderr,
	}

	if p.AgentName != "spec-writer" {
		t.Errorf("AgentName = %q, want %q", p.AgentName, "spec-writer")
	}
	if p.Gate != GateAuto {
		t.Errorf("Gate = %q, want %q", p.Gate, GateAuto)
	}
	if p.RejectionFeedback == nil || *p.RejectionFeedback != feedback {
		t.Errorf("RejectionFeedback = %v, want %q", p.RejectionFeedback, feedback)
	}
	if p.PreviousStdout == nil || *p.PreviousStdout != stdout {
		t.Errorf("PreviousStdout = %v, want %q", p.PreviousStdout, stdout)
	}
	if len(p.ContextArtifacts["spec"]) != 1 || p.ContextArtifacts["spec"][0] != "docs/specs/001.md" {
		t.Errorf("ContextArtifacts = %v, want map with spec artifact", p.ContextArtifacts)
	}
}

func strPtr(s string) *string { return &s }

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGenerateTaskID(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		number string
		suffix int
		want   string
	}{
		{"13", 0, "task-013-20260429-143000"},
		{"1", 0, "task-001-20260429-143000"},
		{"1234", 0, "task-1234-20260429-143000"},
		{"13", 1, "task-013-20260429-143000-1"},
		{"13", 5, "task-013-20260429-143000-5"},
		{"0", 0, "task-000-20260429-143000"},
	}

	for _, tt := range tests {
		t.Run(tt.number+"/"+string(rune('0'+tt.suffix)), func(t *testing.T) {
			got := GenerateTaskID(tt.number, now, tt.suffix)
			if got != tt.want {
				t.Errorf("GenerateTaskID(%q, now, %d) = %q, want %q", tt.number, tt.suffix, got, tt.want)
			}
			if len(got) > 30 {
				t.Errorf("ID %q is %d chars, exceeds 30", got, len(got))
			}
		})
	}
}

func TestIsDuplicateIDError(t *testing.T) {
	if IsDuplicateIDError(nil) {
		t.Error("nil error should not be duplicate")
	}
	if IsDuplicateIDError(errors.New("some other error")) {
		t.Error("unrelated error should not be duplicate")
	}
	if !IsDuplicateIDError(errors.New("UNIQUE constraint failed: tasks.id")) {
		t.Error("UNIQUE constraint error should be duplicate")
	}
}
