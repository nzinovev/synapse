package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// --- read_file ---------------------------------------------------------------

type readFileTool struct {
	perm ToolPermission
}

// NewReadFileTool returns a Tool that reads the contents of a file.
func NewReadFileTool(perm ToolPermission) Tool {
	return &readFileTool{perm: perm}
}

func (t *readFileTool) Name() string { return "read_file" }

func (t *readFileTool) Description() string {
	return "Read the contents of a file at the given path within the workspace."
}

func (t *readFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to read (absolute or relative to the workspace).",
			},
		},
		"required": []string{"path"},
	}
}

func (t *readFileTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	path, err := stringField(input, "path")
	if err != nil {
		return nil, err
	}

	if err := CheckPath(path, t.perm); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("read_file: %q is a directory, not a file", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	return map[string]any{"content": string(data)}, nil
}

// --- write_file --------------------------------------------------------------

type writeFileTool struct {
	perm ToolPermission
}

// NewWriteFileTool returns a Tool that writes content to a file.
func NewWriteFileTool(perm ToolPermission) Tool {
	return &writeFileTool{perm: perm}
}

func (t *writeFileTool) Name() string { return "write_file" }

func (t *writeFileTool) Description() string {
	return "Write content to a file at the given path within the workspace."
}

func (t *writeFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to write (absolute or relative to the workspace).",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Text content to write to the file.",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *writeFileTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	if !t.perm.AllowWrites {
		return nil, fmt.Errorf("write_file: writes are not permitted (allow_writes=false)")
	}

	path, err := stringField(input, "path")
	if err != nil {
		return nil, err
	}
	content, err := stringField(input, "content")
	if err != nil {
		return nil, err
	}

	if err := CheckPath(path, t.perm); err != nil {
		return nil, err
	}

	// Ensure parent directory exists.
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return nil, fmt.Errorf("write_file: cannot create parent directory: %w", mkErr)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}

	return map[string]any{"written": true}, nil
}

// --- list_files --------------------------------------------------------------

type listFilesTool struct {
	perm ToolPermission
}

// NewListFilesTool returns a Tool that lists files in a directory.
func NewListFilesTool(perm ToolPermission) Tool {
	return &listFilesTool{perm: perm}
}

func (t *listFilesTool) Name() string { return "list_files" }

func (t *listFilesTool) Description() string {
	return "List the files and directories inside a directory within the workspace."
}

func (t *listFilesTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the directory to list (absolute or relative to the workspace).",
			},
		},
		"required": []string{"path"},
	}
}

func (t *listFilesTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	path, err := stringField(input, "path")
	if err != nil {
		return nil, err
	}

	if err := CheckPath(path, t.perm); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("list_files: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		files = append(files, name)
	}

	return map[string]any{"files": files}, nil
}

// --- helpers -----------------------------------------------------------------

func stringField(input map[string]any, key string) (string, error) {
	v, ok := input[key]
	if !ok {
		return "", fmt.Errorf("missing required field %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string, got %T", key, v)
	}
	return s, nil
}
