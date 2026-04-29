package queue

import (
	"context"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/store"
)

func setupQueueTest(t *testing.T) (*SQLiteQueueStore, *store.SQLiteStore) {
	t.Helper()
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	q := NewSQLiteQueueStore(s.DB())
	return q, s
}

func TestEnqueueDequeue(t *testing.T) {
	q, _ := setupQueueTest(t)
	ctx := context.Background()

	feedback := "needs work"
	if err := q.Enqueue(ctx, "task-1", "approve", &feedback); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	item, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if item == nil {
		t.Fatal("expected item, got nil")
	}
	if item.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", item.TaskID, "task-1")
	}
	if item.Action != "approve" {
		t.Errorf("Action = %q, want %q", item.Action, "approve")
	}
	if item.Feedback == nil || *item.Feedback != "needs work" {
		t.Errorf("Feedback = %v, want %q", item.Feedback, "needs work")
	}
	if item.Status != QueueRunning {
		t.Errorf("Status = %q, want %q", item.Status, QueueRunning)
	}
}

func TestDequeueEmpty(t *testing.T) {
	q, _ := setupQueueTest(t)
	ctx := context.Background()

	item, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if item != nil {
		t.Error("expected nil for empty queue")
	}
}

func TestCompleteItem(t *testing.T) {
	q, _ := setupQueueTest(t)
	ctx := context.Background()

	q.Enqueue(ctx, "task-1", "run", nil)
	item, _ := q.Dequeue(ctx, "worker-0")

	if err := q.CompleteItem(ctx, item.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestResetOrphanedItems(t *testing.T) {
	q, _ := setupQueueTest(t)
	ctx := context.Background()

	q.Enqueue(ctx, "task-1", "run", nil)
	q.Dequeue(ctx, "worker-0") // sets status to running

	if err := q.ResetOrphanedItems(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// Should be able to dequeue again
	item, err := q.Dequeue(ctx, "worker-1")
	if err != nil {
		t.Fatalf("dequeue after reset: %v", err)
	}
	if item == nil {
		t.Fatal("expected item after reset")
	}
	if item.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", item.TaskID, "task-1")
	}
}

func TestReenqueueRunningTasks(t *testing.T) {
	q, s := setupQueueTest(t)
	ctx := context.Background()

	// Create a task with running status.
	s.CreateTask(ctx, &domain.Task{
		ID:             "running-task",
		PipelineName:   "backend",
		Description:    "test task",
		WorkingDir:     "/tmp",
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Runs:           []domain.StageRun{},
		Artifacts:      map[string][]string{},
	})

	if err := q.ReenqueueRunningTasks(ctx); err != nil {
		t.Fatalf("reenqueue: %v", err)
	}

	item, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if item == nil {
		t.Fatal("expected item from reenqueued running task")
	}
	if item.TaskID != "running-task" {
		t.Errorf("TaskID = %q, want %q", item.TaskID, "running-task")
	}
	if item.Action != "run" {
		t.Errorf("Action = %q, want %q", item.Action, "run")
	}
}

func TestReenqueueRunningTasksIdempotent(t *testing.T) {
	q, s := setupQueueTest(t)
	ctx := context.Background()

	s.CreateTask(ctx, &domain.Task{
		ID:             "running-task",
		PipelineName:   "backend",
		Description:    "test task",
		WorkingDir:     "/tmp",
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Runs:           []domain.StageRun{},
		Artifacts:      map[string][]string{},
	})

	if err := q.ReenqueueRunningTasks(ctx); err != nil {
		t.Fatalf("first reenqueue: %v", err)
	}
	if err := q.ReenqueueRunningTasks(ctx); err != nil {
		t.Fatalf("second reenqueue: %v", err)
	}

	item, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if item == nil {
		t.Fatal("expected one queue item")
	}

	duplicate, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("second dequeue: %v", err)
	}
	if duplicate != nil {
		t.Errorf("expected no duplicate queue item, got task=%s action=%s", duplicate.TaskID, duplicate.Action)
	}
}

func TestReenqueueSkipsTaskWithActiveQueueItem(t *testing.T) {
	q, s := setupQueueTest(t)
	ctx := context.Background()

	s.CreateTask(ctx, &domain.Task{
		ID:             "running-task",
		PipelineName:   "backend",
		Description:    "test task",
		WorkingDir:     "/tmp",
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Runs:           []domain.StageRun{},
		Artifacts:      map[string][]string{},
	})

	// Enqueue and dequeue to simulate an in-progress queue item (status='running').
	q.Enqueue(ctx, "running-task", "run", nil)
	q.Dequeue(ctx, "worker-0")

	// ReenqueueRunningTasks must not insert a second item because one is already running.
	if err := q.ReenqueueRunningTasks(ctx); err != nil {
		t.Fatalf("reenqueue: %v", err)
	}

	// Reset the orphaned item back to pending to make it dequeueable.
	q.ResetOrphanedItems(ctx)

	first, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if first == nil {
		t.Fatal("expected one item")
	}

	extra, err := q.Dequeue(ctx, "worker-0")
	if err != nil {
		t.Fatalf("second dequeue: %v", err)
	}
	if extra != nil {
		t.Errorf("expected no duplicate queue item, got task=%s action=%s", extra.TaskID, extra.Action)
	}
}

func TestFIFO(t *testing.T) {
	q, _ := setupQueueTest(t)
	ctx := context.Background()

	q.Enqueue(ctx, "task-1", "run", nil)
	time.Sleep(10 * time.Millisecond)
	q.Enqueue(ctx, "task-2", "run", nil)

	item1, _ := q.Dequeue(ctx, "worker-0")
	if item1.TaskID != "task-1" {
		t.Errorf("first item = %q, want %q", item1.TaskID, "task-1")
	}

	item2, _ := q.Dequeue(ctx, "worker-0")
	if item2.TaskID != "task-2" {
		t.Errorf("second item = %q, want %q", item2.TaskID, "task-2")
	}
}
