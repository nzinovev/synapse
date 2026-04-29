package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func makeTask(id string) *domain.Task {
	return &domain.Task{
		ID:             id,
		PipelineName:   "backend",
		Description:    "Test task " + id,
		WorkingDir:     "/tmp/test",
		CurrentStageID: "spec",
		Status:         domain.StatusPending,
		Artifacts:      map[string][]string{},
		FixCycleCount:  0,
		PRIndex:        1,
	}
}

func TestCreateAndLoadTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	task := makeTask("task-001")
	task.Artifacts = map[string][]string{
		"spec": {"/tmp/spec.md"},
	}

	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	loaded, err := store.LoadTask(ctx, "task-001")
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}

	if loaded.ID != "task-001" {
		t.Errorf("ID = %q, want %q", loaded.ID, "task-001")
	}
	if loaded.PipelineName != "backend" {
		t.Errorf("PipelineName = %q, want %q", loaded.PipelineName, "backend")
	}
	if loaded.Status != domain.StatusPending {
		t.Errorf("Status = %q, want %q", loaded.Status, domain.StatusPending)
	}
	if loaded.FixCycleCount != 0 {
		t.Errorf("FixCycleCount = %d, want 0", loaded.FixCycleCount)
	}
	if loaded.PRIndex != 1 {
		t.Errorf("PRIndex = %d, want 1", loaded.PRIndex)
	}
	if len(loaded.Artifacts) != 1 || len(loaded.Artifacts["spec"]) != 1 {
		t.Errorf("Artifacts = %v, want map[spec:[/tmp/spec.md]]", loaded.Artifacts)
	}
}

func TestSaveTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	task := makeTask("task-002")
	store.CreateTask(ctx, task)

	task.Status = domain.StatusRunning
	task.CurrentStageID = "implement"
	task.FixCycleCount = 2
	task.PRIndex = 3
	task.Artifacts["adr"] = []string{"/tmp/adr.md", "/tmp/adr2.md"}

	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	loaded, _ := store.LoadTask(ctx, "task-002")
	if loaded.Status != domain.StatusRunning {
		t.Errorf("Status = %q, want %q", loaded.Status, domain.StatusRunning)
	}
	if loaded.CurrentStageID != "implement" {
		t.Errorf("CurrentStageID = %q, want %q", loaded.CurrentStageID, "implement")
	}
	if loaded.FixCycleCount != 2 {
		t.Errorf("FixCycleCount = %d, want 2", loaded.FixCycleCount)
	}
	if loaded.PRIndex != 3 {
		t.Errorf("PRIndex = %d, want 3", loaded.PRIndex)
	}
	if len(loaded.Artifacts["adr"]) != 2 {
		t.Errorf("Artifacts[adr] = %v, want 2 entries", loaded.Artifacts["adr"])
	}
}

func TestStageRunsRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	task := makeTask("task-003")
	exitCode := 0
	feedback := "needs fixes"
	task.Runs = []domain.StageRun{
		{
			StageID:   "spec",
			Attempt:   1,
			Trigger:   domain.TriggerInitial,
			StartedAt: time.Now().UTC().Add(-5 * time.Minute),
			AgentResult: &domain.AgentResult{
				Success:          true,
				Stdout:           "spec output",
				Stderr:           "",
				DurationSeconds:  30.5,
				ExitCode:         &exitCode,
				ArtifactsCreated: []string{"/tmp/spec.md"},
			},
		},
		{
			StageID:           "review",
			Attempt:           1,
			Trigger:           domain.TriggerInitial,
			StartedAt:         time.Now().UTC().Add(-3 * time.Minute),
			RejectionFeedback: &feedback,
			AgentResult: &domain.AgentResult{
				Success:         false,
				Stdout:          "review output",
				Stderr:          "some stderr",
				DurationSeconds: 15.0,
			},
		},
	}

	store.CreateTask(ctx, task)

	loaded, _ := store.LoadTask(ctx, "task-003")
	if len(loaded.Runs) != 2 {
		t.Fatalf("len(Runs) = %d, want 2", len(loaded.Runs))
	}

	specRun := loaded.Runs[0]
	if specRun.StageID != "spec" {
		t.Errorf("StageID = %q, want %q", specRun.StageID, "spec")
	}
	if specRun.Trigger != domain.TriggerInitial {
		t.Errorf("Trigger = %q, want %q", specRun.Trigger, domain.TriggerInitial)
	}
	if specRun.AgentResult == nil || !specRun.AgentResult.Success {
		t.Error("AgentResult.Success = false, want true")
	}
	if specRun.AgentResult.Stdout != "spec output" {
		t.Errorf("Stdout = %q, want %q", specRun.AgentResult.Stdout, "spec output")
	}
	if specRun.AgentResult.DurationSeconds != 30.5 {
		t.Errorf("DurationSeconds = %f, want 30.5", specRun.AgentResult.DurationSeconds)
	}
	if specRun.AgentResult.ExitCode == nil || *specRun.AgentResult.ExitCode != 0 {
		t.Error("ExitCode = nil or wrong value, want 0")
	}
	if len(specRun.AgentResult.ArtifactsCreated) != 1 {
		t.Errorf("ArtifactsCreated = %v, want 1 entry", specRun.AgentResult.ArtifactsCreated)
	}

	reviewRun := loaded.Runs[1]
	if reviewRun.RejectionFeedback == nil || *reviewRun.RejectionFeedback != "needs fixes" {
		t.Error("RejectionFeedback not preserved")
	}
	if reviewRun.AgentResult.Success {
		t.Error("AgentResult.Success = true, want false")
	}
	if reviewRun.AgentResult.Stderr != "some stderr" {
		t.Errorf("Stderr = %q, want %q", reviewRun.AgentResult.Stderr, "some stderr")
	}
}

func TestSaveTaskReplacesStageRuns(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	task := makeTask("task-004")
	task.Runs = []domain.StageRun{
		{StageID: "spec", Attempt: 1, Trigger: domain.TriggerInitial, StartedAt: time.Now().UTC()},
	}
	store.CreateTask(ctx, task)

	// Add a second run via SaveTask.
	task.Runs = append(task.Runs, domain.StageRun{
		StageID:   "adr",
		Attempt:   1,
		Trigger:   domain.TriggerInitial,
		StartedAt: time.Now().UTC(),
	})
	store.SaveTask(ctx, task)

	loaded, _ := store.LoadTask(ctx, "task-004")
	if len(loaded.Runs) != 2 {
		t.Fatalf("len(Runs) = %d, want 2", len(loaded.Runs))
	}
	if loaded.Runs[0].StageID != "spec" {
		t.Errorf("Runs[0].StageID = %q, want spec", loaded.Runs[0].StageID)
	}
	if loaded.Runs[1].StageID != "adr" {
		t.Errorf("Runs[1].StageID = %q, want adr", loaded.Runs[1].StageID)
	}
}

func TestListTasks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := range 3 {
		task := makeTask(fmt.Sprintf("task-%03d", i))
		task.Description = fmt.Sprintf("Task %d", i)
		store.CreateTask(ctx, task)
		time.Sleep(10 * time.Millisecond) // stagger created_at
	}

	tasks, err := store.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("len(tasks) = %d, want 3", len(tasks))
	}

	// Should be ordered by created_at DESC.
	if tasks[0].Description == tasks[1].Description {
		t.Error("tasks not ordered by created_at DESC")
	}
}

func TestAppendAndLoadEvents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	task := makeTask("task-ev")
	store.CreateTask(ctx, task)

	events := []*domain.AgentEvent{
		{
			TaskID: "task-ev", StageID: "spec", Timestamp: time.Now().UTC(),
			Kind: domain.EventStageStarted, Message: "starting spec",
		},
		{
			TaskID: "task-ev", StageID: "spec", Timestamp: time.Now().UTC(),
			Kind: domain.EventAgentOutput, Message: "agent output",
			Metadata: map[string]any{"lines": float64(42)},
		},
		{
			TaskID: "task-ev", StageID: "spec", Timestamp: time.Now().UTC(),
			Kind: domain.EventStageCompleted, Message: "done",
		},
	}

	for _, e := range events {
		if err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	loaded, err := store.LoadEvents(ctx, "task-ev")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(loaded))
	}

	if loaded[0].Kind != domain.EventStageStarted {
		t.Errorf("events[0].Kind = %q, want stage_started", loaded[0].Kind)
	}
	if loaded[1].Kind != domain.EventAgentOutput {
		t.Errorf("events[1].Kind = %q, want agent_output", loaded[1].Kind)
	}
	if loaded[1].Metadata["lines"] != float64(42) {
		t.Errorf("events[1].Metadata[lines] = %v, want 42", loaded[1].Metadata["lines"])
	}
	if loaded[2].Kind != domain.EventStageCompleted {
		t.Errorf("events[2].Kind = %q, want stage_completed", loaded[2].Kind)
	}
}

func TestStageWorkdir(t *testing.T) {
	store := newTestStore(t)
	dir := store.StageWorkdir("task-001", "spec", 1)

	if !filepath.IsAbs(dir) {
		t.Errorf("StageWorkdir not absolute: %q", dir)
	}
	expected := filepath.Join(store.taskDir, "task-001", "stages", "spec", "attempt-1")
	if dir != expected {
		t.Errorf("StageWorkdir = %q, want %q", dir, expected)
	}
}

func TestLoadTaskNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.LoadTask(ctx, "nonexistent")
	if err == nil {
		t.Error("LoadTask should error for nonexistent task")
	}
}

func TestConcurrentGoroutineWrites(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const numGoroutines = 15
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			taskID := fmt.Sprintf("concurrent-%03d", idx)
			task := makeTask(taskID)
			if err := store.CreateTask(ctx, task); err != nil {
				errCh <- fmt.Errorf("create %s: %w", taskID, err)
				return
			}
			task.Status = domain.StatusRunning
			if err := store.SaveTask(ctx, task); err != nil {
				errCh <- fmt.Errorf("save %s: %w", taskID, err)
				return
			}
			event := &domain.AgentEvent{
				TaskID: taskID, StageID: "spec", Timestamp: time.Now().UTC(),
				Kind: domain.EventStageStarted, Message: fmt.Sprintf("started %d", idx),
			}
			if err := store.AppendEvent(ctx, event); err != nil {
				errCh <- fmt.Errorf("event %s: %w", taskID, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent write error: %v", err)
	}

	tasks, err := store.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks after concurrent writes: %v", err)
	}
	if len(tasks) != numGoroutines {
		t.Errorf("len(tasks) = %d, want %d", len(tasks), numGoroutines)
	}

	for _, task := range tasks {
		if task.Status != domain.StatusRunning {
			t.Errorf("task %s: Status = %q, want running", task.ID, task.Status)
		}
		events, err := store.LoadEvents(ctx, task.ID)
		if err != nil {
			t.Errorf("LoadEvents %s: %v", task.ID, err)
		}
		if len(events) != 1 {
			t.Errorf("task %s: len(events) = %d, want 1", task.ID, len(events))
		}
	}
}

func TestMigrationIdempotency(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()

	// Open store twice — second time should apply no new migrations.
	store1, err := NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("first NewSQLiteStore: %v", err)
	}
	store1.Close()

	store2, err := NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("second NewSQLiteStore: %v", err)
	}
	store2.Close()

	// Verify the database still works after double migration.
	store3, err := NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("third NewSQLiteStore: %v", err)
	}
	defer store3.Close()

	task := makeTask("idempotency-test")
	if err := store3.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask after double migration: %v", err)
	}
}

func TestUpdatedAtChanges(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	task := makeTask("task-updated")
	store.CreateTask(ctx, task)
	firstUpdate := task.UpdatedAt

	time.Sleep(50 * time.Millisecond)

	task.Status = domain.StatusRunning
	store.SaveTask(ctx, task)

	loaded, _ := store.LoadTask(ctx, "task-updated")
	if !loaded.UpdatedAt.After(firstUpdate) {
		t.Errorf("UpdatedAt not advanced: before=%v after=%v", firstUpdate, loaded.UpdatedAt)
	}
}

func TestNilAgentResultRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	exitCode := 0
	task := makeTask("task-nil-ar")
	task.Runs = []domain.StageRun{
		{
			StageID:     "spec",
			Attempt:     1,
			Trigger:     domain.TriggerInitial,
			StartedAt:   time.Now().UTC().Add(-5 * time.Minute),
			AgentResult: nil,
		},
		{
			StageID:   "adr",
			Attempt:   1,
			Trigger:   domain.TriggerInitial,
			StartedAt: time.Now().UTC().Add(-3 * time.Minute),
			AgentResult: &domain.AgentResult{
				Success:         true,
				Stdout:          "adr output",
				Stderr:          "",
				DurationSeconds: 20.0,
				ExitCode:        &exitCode,
			},
		},
		{
			StageID:   "review",
			Attempt:   1,
			Trigger:   domain.TriggerInitial,
			StartedAt: time.Now().UTC().Add(-1 * time.Minute),
			AgentResult: &domain.AgentResult{
				Success:         false,
				Stdout:          "review output",
				Stderr:          "errors",
				DurationSeconds: 5.0,
			},
		},
	}

	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	loaded, err := store.LoadTask(ctx, "task-nil-ar")
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}

	if len(loaded.Runs) != 3 {
		t.Fatalf("len(Runs) = %d, want 3", len(loaded.Runs))
	}

	// Stage with nil AgentResult must load back as nil.
	if loaded.Runs[0].AgentResult != nil {
		t.Errorf("spec run AgentResult = %v, want nil (not yet executed)", loaded.Runs[0].AgentResult)
	}

	// Stage with successful result must load correctly.
	if loaded.Runs[1].AgentResult == nil {
		t.Fatal("adr run AgentResult = nil, want non-nil")
	}
	if !loaded.Runs[1].AgentResult.Success {
		t.Error("adr run AgentResult.Success = false, want true")
	}

	// Stage with failed result must load correctly.
	if loaded.Runs[2].AgentResult == nil {
		t.Fatal("review run AgentResult = nil, want non-nil")
	}
	if loaded.Runs[2].AgentResult.Success {
		t.Error("review run AgentResult.Success = true, want false")
	}
	if loaded.Runs[2].AgentResult.Stderr != "errors" {
		t.Errorf("review run Stderr = %q, want %q", loaded.Runs[2].AgentResult.Stderr, "errors")
	}

	// Verify round-trip through SaveTask preserves nil.
	task.Status = domain.StatusRunning
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	reloaded, _ := store.LoadTask(ctx, "task-nil-ar")
	if reloaded.Runs[0].AgentResult != nil {
		t.Errorf("after SaveTask: spec run AgentResult = %v, want nil", reloaded.Runs[0].AgentResult)
	}
}
