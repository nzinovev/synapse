package store

import (
	"context"
	"database/sql"
	"fmt"
)

type migration struct {
	Version int
	Up      string
}

var migrations = []migration{
	{
		Version: 1,
		Up: `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    pipeline_name TEXT NOT NULL,
    description TEXT NOT NULL,
    working_dir TEXT NOT NULL,
    current_stage_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    fix_cycle_count INTEGER NOT NULL DEFAULT 0,
    pr_index INTEGER NOT NULL DEFAULT 1,
    artifacts_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS stage_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    stage_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    trigger TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    finished_at DATETIME,
    stdout TEXT DEFAULT '',
    stderr TEXT DEFAULT '',
    exit_code INTEGER,
    success INTEGER NOT NULL DEFAULT 0,
    duration_seconds REAL DEFAULT 0,
    rejection_feedback TEXT,
    artifacts_json TEXT DEFAULT '[]',
    UNIQUE(task_id, stage_id, attempt)
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    stage_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    kind TEXT NOT NULL,
    message TEXT NOT NULL,
    metadata TEXT DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS task_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    action TEXT NOT NULL,
    feedback TEXT,
    enqueued_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME,
    worker_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending'
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_events_task_id ON events(task_id);
CREATE INDEX IF NOT EXISTS idx_stage_runs_task ON stage_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_queue_pending ON task_queue(status) WHERE status = 'pending';
`,
	},
	{
		Version: 2,
		Up:      `ALTER TABLE stage_runs ADD COLUMN has_result INTEGER NOT NULL DEFAULT 0;`,
	},
	{
		Version: 3,
		Up:      `ALTER TABLE tasks ADD COLUMN adapter TEXT NOT NULL DEFAULT '';`,
	},
	{
		Version: 4,
		Up:      `ALTER TABLE stage_runs ADD COLUMN adapter TEXT NOT NULL DEFAULT '';`,
	},
	{
		Version: 5,
		Up:      `ALTER TABLE tasks ADD COLUMN sandbox_mode TEXT NOT NULL DEFAULT 'docker';`,
	},
	{
		Version: 6,
		Up:      `ALTER TABLE stage_runs ADD COLUMN container_info_json TEXT DEFAULT NULL;`,
	},
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	current, err := currentVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("apply migration V%d: %w", m.Version, err)
		}
	}
	return nil
}

func currentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		// Table might not exist yet — return 0 so V1 runs.
		return 0, nil
	}
	return version, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.Up); err != nil {
		return fmt.Errorf("exec migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", m.Version); err != nil {
		// V1 creates schema_version — if it already exists from the Up block, use UPDATE instead.
		if _, err2 := tx.ExecContext(ctx, "UPDATE schema_version SET version = ? WHERE version < ?", m.Version, m.Version); err2 != nil {
			return fmt.Errorf("record version: %w (original: %v)", err2, err)
		}
	}

	return tx.Commit()
}
