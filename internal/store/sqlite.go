package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nzinovev/synapse/internal/domain"

	_ "modernc.org/sqlite"
)

type TaskStore interface {
	CreateTask(ctx context.Context, task *domain.Task) error
	LoadTask(ctx context.Context, taskID string) (*domain.Task, error)
	SaveTask(ctx context.Context, task *domain.Task) error
	ListTasks(ctx context.Context) ([]*domain.Task, error)
	AppendEvent(ctx context.Context, event *domain.AgentEvent) error
	LoadEvents(ctx context.Context, taskID string) ([]*domain.AgentEvent, error)
	StageWorkdir(taskID, stageID string, attempt int) string
	Close() error
}

type SQLiteStore struct {
	db      *sql.DB
	taskDir string
}

func NewSQLiteStore(ctx context.Context, dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=1", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Single connection serializes writes; WAL mode allows concurrent reads.
	db.SetMaxOpenConns(1)
	if err := RunMigrations(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	taskDir := filepath.Join(filepath.Dir(dbPath), "tasks")
	os.MkdirAll(taskDir, 0o755)

	return &SQLiteStore{db: db, taskDir: taskDir}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) CreateTask(ctx context.Context, task *domain.Task) error {
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	return s.insertTask(ctx, task)
}

// ImportTask inserts a task preserving its original timestamps (for migration).
func (s *SQLiteStore) ImportTask(ctx context.Context, task *domain.Task) error {
	return s.insertTask(ctx, task)
}

func (s *SQLiteStore) insertTask(ctx context.Context, task *domain.Task) error {
	artifactsJSON, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, pipeline_name, description, working_dir, current_stage_id,
		                   status, created_at, updated_at, fix_cycle_count, pr_index, artifacts_json, adapter, sandbox_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.PipelineName, task.Description, task.WorkingDir, task.CurrentStageID,
		string(task.Status), task.CreatedAt, task.UpdatedAt, task.FixCycleCount, task.PRIndex, string(artifactsJSON), task.Adapter, string(task.SandboxMode),
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}

	for _, run := range task.Runs {
		if err := s.insertStageRun(ctx, task.ID, &run); err != nil {
			return fmt.Errorf("insert stage run: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) LoadTask(ctx context.Context, taskID string) (*domain.Task, error) {
	var t domain.Task
	var status, artifactsJSON, sandboxMode string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, pipeline_name, description, working_dir, current_stage_id,
		       status, created_at, updated_at, fix_cycle_count, pr_index, artifacts_json, adapter, sandbox_mode
		FROM tasks WHERE id = ?`, taskID,
	).Scan(&t.ID, &t.PipelineName, &t.Description, &t.WorkingDir, &t.CurrentStageID,
		&status, &t.CreatedAt, &t.UpdatedAt, &t.FixCycleCount, &t.PRIndex, &artifactsJSON, &t.Adapter, &sandboxMode,
	)
	if err != nil {
		return nil, fmt.Errorf("query task: %w", err)
	}

	t.Status = domain.TaskStatus(status)
	t.SandboxMode = domain.SandboxMode(sandboxMode)
	t.Artifacts = make(map[string][]string)
	if err := json.Unmarshal([]byte(artifactsJSON), &t.Artifacts); err != nil {
		return nil, fmt.Errorf("unmarshal artifacts: %w", err)
	}

	runs, err := s.loadStageRuns(ctx, taskID)
	if err != nil {
		return nil, err
	}
	t.Runs = runs

	return &t, nil
}

func (s *SQLiteStore) SaveTask(ctx context.Context, task *domain.Task) error {
	artifactsJSON, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal artifacts: %w", err)
	}

	task.UpdatedAt = time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE tasks SET pipeline_name=?, description=?, working_dir=?, current_stage_id=?,
		                 status=?, updated_at=?, fix_cycle_count=?, pr_index=?, artifacts_json=?, adapter=?, sandbox_mode=?
		WHERE id=?`,
		task.PipelineName, task.Description, task.WorkingDir, task.CurrentStageID,
		string(task.Status), task.UpdatedAt, task.FixCycleCount, task.PRIndex, string(artifactsJSON), task.Adapter, string(task.SandboxMode),
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	// Reconcile stage runs: delete existing and re-insert.
	if _, err := tx.ExecContext(ctx, "DELETE FROM stage_runs WHERE task_id=?", task.ID); err != nil {
		return fmt.Errorf("delete stage runs: %w", err)
	}
	for _, run := range task.Runs {
		if err := s.insertStageRunInTx(tx, task.ID, &run); err != nil {
			return fmt.Errorf("insert stage run: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) ListTasks(ctx context.Context) ([]*domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pipeline_name, description, working_dir, current_stage_id,
		       status, created_at, updated_at, fix_cycle_count, pr_index, artifacts_json, adapter, sandbox_mode
		FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	// Collect task rows first so we release the connection before loading stage runs.
	type taskRow struct {
		task          domain.Task
		artifactsJSON string
	}
	var collected []taskRow
	for rows.Next() {
		var tr taskRow
		var status string
		if err := rows.Scan(&tr.task.ID, &tr.task.PipelineName, &tr.task.Description,
			&tr.task.WorkingDir, &tr.task.CurrentStageID,
			&status, &tr.task.CreatedAt, &tr.task.UpdatedAt,
			&tr.task.FixCycleCount, &tr.task.PRIndex, &tr.artifactsJSON, &tr.task.Adapter, &tr.task.SandboxMode,
		); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tr.task.Status = domain.TaskStatus(status)
		collected = append(collected, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tasks := make([]*domain.Task, 0, len(collected))
	for _, tr := range collected {
		tr.task.Artifacts = make(map[string][]string)
		_ = json.Unmarshal([]byte(tr.artifactsJSON), &tr.task.Artifacts)

		runs, err := s.loadStageRuns(ctx, tr.task.ID)
		if err != nil {
			return nil, err
		}
		tr.task.Runs = runs
		tasks = append(tasks, &tr.task)
	}
	return tasks, nil
}

func (s *SQLiteStore) AppendEvent(ctx context.Context, event *domain.AgentEvent) error {
	mdJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events (task_id, stage_id, timestamp, kind, message, metadata)
		VALUES (?, ?, ?, ?, ?, ?)`,
		event.TaskID, event.StageID, event.Timestamp, string(event.Kind), event.Message, string(mdJSON),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) LoadEvents(ctx context.Context, taskID string) ([]*domain.AgentEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, stage_id, timestamp, kind, message, metadata
		FROM events WHERE task_id = ? ORDER BY id ASC`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []*domain.AgentEvent
	for rows.Next() {
		var e domain.AgentEvent
		var kind, mdJSON string
		if err := rows.Scan(&e.TaskID, &e.StageID, &e.Timestamp, &kind, &e.Message, &mdJSON); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Kind = domain.EventKind(kind)
		e.Metadata = make(map[string]any)
		_ = json.Unmarshal([]byte(mdJSON), &e.Metadata)
		events = append(events, &e)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) StageWorkdir(taskID, stageID string, attempt int) string {
	return filepath.Join(s.taskDir, taskID, "stages", stageID, fmt.Sprintf("attempt-%d", attempt))
}

func (s *SQLiteStore) insertStageRun(ctx context.Context, taskID string, run *domain.StageRun) error {
	return s.insertStageRunInTx(s.db, taskID, run)
}

func (s *SQLiteStore) insertStageRunInTx(tx dbtx, taskID string, run *domain.StageRun) error {
	var stdout, stderr string
	var exitCode sql.Null[int]
	var success bool
	var durationSec float64
	var artifactsJSON string
	var feedback sql.Null[string]
	var hasResult bool
	var containerInfoJSON sql.Null[string]

	if run.AgentResult != nil {
		hasResult = true
		stdout = run.AgentResult.Stdout
		stderr = run.AgentResult.Stderr
		success = run.AgentResult.Success
		durationSec = run.AgentResult.DurationSeconds
		artifactsJSON = "[]"
		if len(run.AgentResult.ArtifactsCreated) > 0 {
			b, _ := json.Marshal(run.AgentResult.ArtifactsCreated)
			artifactsJSON = string(b)
		}
		if run.AgentResult.ExitCode != nil {
			exitCode = sql.Null[int]{V: *run.AgentResult.ExitCode, Valid: true}
		}
		if run.AgentResult.ContainerInfo != nil {
			b, _ := json.Marshal(run.AgentResult.ContainerInfo)
			containerInfoJSON = sql.Null[string]{V: string(b), Valid: true}
		}
	} else {
		artifactsJSON = "[]"
	}

	if run.RejectionFeedback != nil {
		feedback = sql.Null[string]{V: *run.RejectionFeedback, Valid: true}
	}

	var finishedAt sql.Null[time.Time]
	if run.FinishedAt != nil {
		finishedAt = sql.Null[time.Time]{V: *run.FinishedAt, Valid: true}
	}

	_, err := tx.ExecContext(context.Background(), `
		INSERT INTO stage_runs (task_id, stage_id, attempt, trigger, started_at, finished_at,
		                        stdout, stderr, exit_code, success, duration_seconds,
		                        rejection_feedback, artifacts_json, has_result, adapter, container_info_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, run.StageID, run.Attempt, string(run.Trigger), run.StartedAt, finishedAt,
		stdout, stderr, exitCode, success, durationSec, feedback, artifactsJSON, hasResult, run.Adapter, containerInfoJSON,
	)
	return err
}

func (s *SQLiteStore) loadStageRuns(ctx context.Context, taskID string) ([]domain.StageRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stage_id, attempt, trigger, started_at, finished_at,
		       stdout, stderr, exit_code, success, duration_seconds,
		       rejection_feedback, artifacts_json, has_result, adapter, container_info_json
		FROM stage_runs WHERE task_id = ? ORDER BY id ASC`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query stage runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.StageRun
	for rows.Next() {
		var r domain.StageRun
		var trigger string
		var finishedAt sql.Null[time.Time]
		var stdout, stderr, artifactsJSON string
		var exitCode sql.Null[int]
		var success bool
		var durationSec float64
		var feedback sql.Null[string]
		var hasResult bool
		var containerInfoJSON sql.Null[string]

		if err := rows.Scan(&r.StageID, &r.Attempt, &trigger, &r.StartedAt, &finishedAt,
			&stdout, &stderr, &exitCode, &success, &durationSec,
			&feedback, &artifactsJSON, &hasResult, &r.Adapter, &containerInfoJSON,
		); err != nil {
			return nil, fmt.Errorf("scan stage run: %w", err)
		}

		r.Trigger = domain.RunTrigger(trigger)
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.V
		}

		if hasResult {
			r.AgentResult = &domain.AgentResult{
				Success:         success,
				Stdout:          stdout,
				Stderr:          stderr,
				DurationSeconds: durationSec,
			}
			if exitCode.Valid {
				r.AgentResult.ExitCode = &exitCode.V
			}
			_ = json.Unmarshal([]byte(artifactsJSON), &r.AgentResult.ArtifactsCreated)
			if containerInfoJSON.Valid {
				var ci domain.ContainerInfo
				if json.Unmarshal([]byte(containerInfoJSON.V), &ci) == nil {
					r.AgentResult.ContainerInfo = &ci
				}
			}
		}

		if feedback.Valid {
			r.RejectionFeedback = &feedback.V
		}

		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// Ensure directory for stage workdir exists.
func EnsureStageWorkdir(store TaskStore, taskID, stageID string, attempt int) (string, error) {
	dir := store.StageWorkdir(taskID, stageID, attempt)
	// Find the synapse tasks base dir by walking up from the workdir.
	// StageWorkdir returns: <taskDir>/<taskID>/stages/<stageID>/attempt-<n>
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create stage workdir: %w", err)
	}
	return dir, nil
}

type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func SortTasksByCreated(tasks []*domain.Task) {
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
}
