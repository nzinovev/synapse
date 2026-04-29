package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type QueueItemStatus string

const (
	QueuePending   QueueItemStatus = "pending"
	QueueRunning   QueueItemStatus = "running"
	QueueCompleted QueueItemStatus = "completed"
)

type QueueItem struct {
	ID          int64
	TaskID      string
	Action      string
	Feedback    *string
	EnqueuedAt  time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	WorkerID    *string
	Status      QueueItemStatus
}

type QueueStore interface {
	Enqueue(ctx context.Context, taskID, action string, feedback *string) error
	Dequeue(ctx context.Context, workerID string) (*QueueItem, error)
	CompleteItem(ctx context.Context, itemID int64) error
	ResetOrphanedItems(ctx context.Context) error
	ReenqueueRunningTasks(ctx context.Context) error
}

type SQLiteQueueStore struct {
	db *sql.DB
}

func NewSQLiteQueueStore(db *sql.DB) *SQLiteQueueStore {
	return &SQLiteQueueStore{db: db}
}

func (s *SQLiteQueueStore) Enqueue(ctx context.Context, taskID, action string, feedback *string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_queue (task_id, action, feedback, enqueued_at, status)
		VALUES (?, ?, ?, ?, 'pending')`,
		taskID, action, feedback, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}

func (s *SQLiteQueueStore) Dequeue(ctx context.Context, workerID string) (*QueueItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var item QueueItem
	var status string
	var feedback sql.Null[string]
	var startedAt, completedAt sql.Null[time.Time]
	var workerIDVal sql.Null[string]

	err = tx.QueryRowContext(ctx, `
		SELECT id, task_id, action, feedback, enqueued_at, started_at, completed_at, worker_id, status
		FROM task_queue
		WHERE status = 'pending'
		ORDER BY enqueued_at ASC
		LIMIT 1`,
	).Scan(&item.ID, &item.TaskID, &item.Action, &feedback, &item.EnqueuedAt,
		&startedAt, &completedAt, &workerIDVal, &status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query pending: %w", err)
	}

	item.Status = QueueItemStatus(status)
	if feedback.Valid {
		item.Feedback = &feedback.V
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.V
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.V
	}
	if workerIDVal.Valid {
		item.WorkerID = &workerIDVal.V
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		UPDATE task_queue SET status = 'running', started_at = ?, worker_id = ?
		WHERE id = ?`,
		now, workerID, item.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("claim item: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	item.Status = QueueRunning
	item.StartedAt = &now
	wid := workerID
	item.WorkerID = &wid
	return &item, nil
}

func (s *SQLiteQueueStore) CompleteItem(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_queue SET status = 'completed', completed_at = ?
		WHERE id = ?`,
		time.Now().UTC(), itemID,
	)
	if err != nil {
		return fmt.Errorf("complete item: %w", err)
	}
	return nil
}

func (s *SQLiteQueueStore) ResetOrphanedItems(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_queue SET status = 'pending', started_at = NULL, worker_id = NULL
		WHERE status = 'running'`,
	)
	if err != nil {
		return fmt.Errorf("reset orphaned: %w", err)
	}
	return nil
}

func (s *SQLiteQueueStore) ReenqueueRunningTasks(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_queue (task_id, action, feedback, enqueued_at, status)
		SELECT id, 'run', NULL, ?, 'pending'
		FROM tasks
		WHERE status = 'running'
		  AND NOT EXISTS (
		    SELECT 1 FROM task_queue
		    WHERE task_queue.task_id = tasks.id
		      AND task_queue.action = 'run'
		      AND task_queue.status IN ('pending', 'running')
		  )`,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("reenqueue running tasks: %w", err)
	}
	return nil
}
