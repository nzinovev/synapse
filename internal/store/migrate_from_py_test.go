package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
)

func writePythonTask(t *testing.T, baseDir, taskID string, task *domain.Task, events []domain.AgentEvent) {
	t.Helper()
	dir := filepath.Join(baseDir, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if len(events) > 0 {
		f, err := os.Create(filepath.Join(dir, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		for _, e := range events {
			if err := enc.Encode(e); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestImportFromPython(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// Set up Go SQLite store.
	store, err := NewSQLiteStore(ctx, filepath.Join(tmp, "synapse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	pyDir := filepath.Join(tmp, "python-tasks")

	now := time.Now().UTC().Truncate(time.Millisecond)
	feedback := "needs work"

	task1 := &domain.Task{
		ID:             "task-001",
		PipelineName:   "backend",
		Description:    "Add logging to engine",
		WorkingDir:     "/tmp/project",
		CurrentStageID: "implement",
		Status:         domain.StatusAwaitingGate,
		CreatedAt:      now,
		UpdatedAt:      now,
		FixCycleCount:  1,
		PRIndex:        2,
		Artifacts: map[string][]string{
			"spec": {"docs/specs/logging.md"},
		},
		Runs: []domain.StageRun{
			{
				StageID:   "spec",
				Attempt:   1,
				Trigger:   domain.TriggerInitial,
				StartedAt: now,
				FinishedAt: func() *time.Time {
					t := now.Add(5 * time.Second)
					return &t
				}(),
				AgentResult: &domain.AgentResult{
					Success:          true,
					Stdout:           "SYNAPSE_AGENT_DONE: spec written",
					DurationSeconds:  4.2,
					ArtifactsCreated: []string{"docs/specs/logging.md"},
				},
			},
			{
				StageID:           "adr",
				Attempt:           1,
				Trigger:           domain.TriggerInitial,
				StartedAt:         now.Add(6 * time.Second),
				RejectionFeedback: &feedback,
			},
		},
	}

	events1 := []domain.AgentEvent{
		{TaskID: "task-001", StageID: "spec", Timestamp: now, Kind: domain.EventStageStarted, Message: "started", Metadata: map[string]any{}},
		{TaskID: "task-001", StageID: "spec", Timestamp: now.Add(5 * time.Second), Kind: domain.EventStageCompleted, Message: "done", Metadata: map[string]any{}},
	}

	writePythonTask(t, pyDir, "task-001", task1, events1)

	imported, err := ImportFromPython(ctx, store, pyDir)
	if err != nil {
		t.Fatalf("ImportFromPython: %v", err)
	}
	if imported != 1 {
		t.Fatalf("expected 1 imported, got %d", imported)
	}

	// Verify task was imported correctly.
	got, err := store.LoadTask(ctx, "task-001")
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}

	if got.PipelineName != "backend" {
		t.Errorf("pipeline = %q, want backend", got.PipelineName)
	}
	if got.Status != domain.StatusAwaitingGate {
		t.Errorf("status = %q, want awaiting_gate", got.Status)
	}
	if got.FixCycleCount != 1 {
		t.Errorf("fix_cycle_count = %d, want 1", got.FixCycleCount)
	}
	if got.PRIndex != 2 {
		t.Errorf("pr_index = %d, want 2", got.PRIndex)
	}
	if len(got.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(got.Runs))
	}
	if got.Runs[0].StageID != "spec" {
		t.Errorf("runs[0].stage_id = %q, want spec", got.Runs[0].StageID)
	}
	if got.Runs[0].AgentResult == nil || !got.Runs[0].AgentResult.Success {
		t.Error("runs[0].agent_result should be success")
	}
	if got.Runs[1].RejectionFeedback == nil || *got.Runs[1].RejectionFeedback != "needs work" {
		t.Error("runs[1].rejection_feedback not preserved")
	}
	if len(got.Artifacts["spec"]) != 1 || got.Artifacts["spec"][0] != "docs/specs/logging.md" {
		t.Errorf("artifacts = %v, want spec:[docs/specs/logging.md]", got.Artifacts)
	}

	// Verify events.
	gotEvents, err := store.LoadEvents(ctx, "task-001")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(gotEvents) != 2 {
		t.Fatalf("events = %d, want 2", len(gotEvents))
	}
	if gotEvents[0].Kind != domain.EventStageStarted {
		t.Errorf("event[0].kind = %q, want stage_started", gotEvents[0].Kind)
	}
}

func TestImportFromPython_SkipExisting(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	store, err := NewSQLiteStore(ctx, filepath.Join(tmp, "synapse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	pyDir := filepath.Join(tmp, "python-tasks")
	now := time.Now().UTC()

	task := &domain.Task{
		ID: "task-exists", PipelineName: "backend", Description: "test",
		WorkingDir: "/tmp", CurrentStageID: "spec", Status: domain.StatusDone,
		CreatedAt: now, UpdatedAt: now, Artifacts: map[string][]string{},
	}

	writePythonTask(t, pyDir, "task-exists", task, nil)

	// Import once.
	imported, _ := ImportFromPython(ctx, store, pyDir)
	if imported != 1 {
		t.Fatalf("first import: expected 1, got %d", imported)
	}

	// Import again — should skip (imported count stays 0 since task already exists).
	imported, _ = ImportFromPython(ctx, store, pyDir)
	if imported != 0 {
		t.Fatalf("second import: expected 0 (skipped existing), got %d", imported)
	}
}

func TestImportFromPython_EmptyDir(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	store, err := NewSQLiteStore(ctx, filepath.Join(tmp, "synapse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	pyDir := filepath.Join(tmp, "empty")
	os.MkdirAll(pyDir, 0o755)

	imported, err := ImportFromPython(ctx, store, pyDir)
	if err != nil {
		t.Fatalf("ImportFromPython empty dir: %v", err)
	}
	if imported != 0 {
		t.Fatalf("expected 0 imported, got %d", imported)
	}
}

func TestImportFromPython_NoEventsFile(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	store, err := NewSQLiteStore(ctx, filepath.Join(tmp, "synapse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	pyDir := filepath.Join(tmp, "python-tasks")
	now := time.Now().UTC()

	task := &domain.Task{
		ID: "no-events", PipelineName: "backend", Description: "test",
		WorkingDir: "/tmp", CurrentStageID: "spec", Status: domain.StatusPending,
		CreatedAt: now, UpdatedAt: now, Artifacts: map[string][]string{},
	}

	// Write state.json only, no events.jsonl.
	writePythonTask(t, pyDir, "no-events", task, nil)

	imported, err := ImportFromPython(ctx, store, pyDir)
	if err != nil {
		t.Fatalf("ImportFromPython: %v", err)
	}
	if imported != 1 {
		t.Fatalf("expected 1, got %d", imported)
	}

	events, _ := store.LoadEvents(ctx, "no-events")
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}
