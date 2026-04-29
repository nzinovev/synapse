package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nzinovev/synapse/internal/domain"
)

// ImportFromPython reads Python .synapse/tasks/<id>/ directories from pythonTasksDir
// and imports each task's state.json and events.jsonl into SQLite.
func ImportFromPython(ctx context.Context, s *SQLiteStore, pythonTasksDir string) (imported int, err error) {
	entries, err := os.ReadDir(pythonTasksDir)
	if err != nil {
		return 0, fmt.Errorf("read python tasks dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		taskDir := filepath.Join(pythonTasksDir, e.Name())
		statePath := filepath.Join(taskDir, "state.json")
		if _, err := os.Stat(statePath); err != nil {
			continue
		}

		task, err := parsePythonState(statePath)
		if err != nil {
			return imported, fmt.Errorf("parse %s: %w", statePath, err)
		}

		// Skip if task already exists.
		if existing, _ := s.LoadTask(ctx, task.ID); existing != nil {
			fmt.Printf("  skipping %s (already exists)\n", task.ID)
			continue
		}

		if err := s.ImportTask(ctx, task); err != nil {
			return imported, fmt.Errorf("import task %s: %w", task.ID, err)
		}

		eventsPath := filepath.Join(taskDir, "events.jsonl")
		if cnt, err := importEvents(ctx, s, eventsPath); err != nil {
			fmt.Printf("  warning: events import for %s: %v\n", task.ID, err)
		} else {
			fmt.Printf("  imported %s (%d events)\n", task.ID, cnt)
		}

		imported++
	}
	return imported, nil
}

func parsePythonState(path string) (*domain.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if task.Artifacts == nil {
		task.Artifacts = map[string][]string{}
	}
	return &task, nil
}

func importEvents(ctx context.Context, s *SQLiteStore, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event domain.AgentEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Metadata == nil {
			event.Metadata = map[string]any{}
		}
		if err := s.AppendEvent(ctx, &event); err != nil {
			return count, fmt.Errorf("insert event: %w", err)
		}
		count++
	}
	return count, scanner.Err()
}
