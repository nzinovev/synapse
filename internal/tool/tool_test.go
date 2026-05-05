package tool_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nzinovev/synapse/internal/tool"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tool-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func permFor(ws string) tool.ToolPermission {
	return tool.ToolPermission{
		AllowWrites:  true,
		WorkspaceDir: ws,
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry
// ---------------------------------------------------------------------------

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := tool.NewToolRegistry()
	ws := mustTempDir(t)
	perm := permFor(ws)

	if err := r.Register(tool.NewReadFileTool(perm)); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Get("read_file")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name() != "read_file" {
		t.Errorf("name: got %q, want %q", got.Name(), "read_file")
	}
}

func TestRegistry_DuplicateReturnsError(t *testing.T) {
	r := tool.NewToolRegistry()
	ws := mustTempDir(t)
	perm := permFor(ws)

	_ = r.Register(tool.NewReadFileTool(perm))
	if err := r.Register(tool.NewReadFileTool(perm)); err == nil {
		t.Fatal("expected error on duplicate registration, got nil")
	}
}

func TestRegistry_GetUnknownReturnsError(t *testing.T) {
	r := tool.NewToolRegistry()
	if _, err := r.Get("no_such_tool"); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistry_ListSortedByName(t *testing.T) {
	r := tool.NewToolRegistry()
	ws := mustTempDir(t)
	perm := permFor(ws)

	_ = r.Register(tool.NewWriteFileTool(perm))
	_ = r.Register(tool.NewReadFileTool(perm))
	_ = r.Register(tool.NewListFilesTool(perm))

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(list))
	}
	names := []string{list[0].Name(), list[1].Name(), list[2].Name()}
	want := []string{"list_files", "read_file", "write_file"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("position %d: got %q, want %q", i, n, want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// CheckPath — workspace escape
// ---------------------------------------------------------------------------

func TestCheckPath_WorkspaceEscape(t *testing.T) {
	ws := mustTempDir(t)
	perm := tool.ToolPermission{WorkspaceDir: ws, AllowWrites: true}

	outsidePath := filepath.Join(ws, "..", "outside")
	if err := tool.CheckPath(outsidePath, perm); err == nil {
		t.Error("expected error for path escaping workspace, got nil")
	}
}

func TestCheckPath_InsideWorkspace(t *testing.T) {
	ws := mustTempDir(t)
	perm := tool.ToolPermission{WorkspaceDir: ws, AllowWrites: true}

	inside := filepath.Join(ws, "subdir", "file.txt")
	if err := tool.CheckPath(inside, perm); err != nil {
		t.Errorf("unexpected error for valid path: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CheckPath — blocked paths
// ---------------------------------------------------------------------------

func TestCheckPath_BlockedPath(t *testing.T) {
	ws := mustTempDir(t)
	blocked := filepath.Join(ws, "secrets")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}

	perm := tool.ToolPermission{
		WorkspaceDir: ws,
		BlockedPaths: []string{blocked},
	}

	target := filepath.Join(blocked, "passwords.txt")
	if err := tool.CheckPath(target, perm); err == nil {
		t.Error("expected error for blocked path, got nil")
	}
}

func TestCheckPath_BlockedGlobPattern(t *testing.T) {
	ws := mustTempDir(t)
	// Write the file so EvalSymlinks can resolve it.
	envFile := filepath.Join(ws, ".env")
	if err := os.WriteFile(envFile, []byte("SECRET=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	perm := tool.ToolPermission{
		WorkspaceDir: ws,
		BlockedPaths: []string{".env"},
	}

	if err := tool.CheckPath(envFile, perm); err == nil {
		t.Error("expected error for .env glob block, got nil")
	}
}

// ---------------------------------------------------------------------------
// CheckPath — symlink escape
// ---------------------------------------------------------------------------

func TestCheckPath_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}

	ws := mustTempDir(t)
	outside := mustTempDir(t)

	// Create a symlink inside the workspace that points outside.
	link := filepath.Join(ws, "escape_link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	perm := tool.ToolPermission{WorkspaceDir: ws}
	if err := tool.CheckPath(link, perm); err == nil {
		t.Error("expected error for symlink escaping workspace, got nil")
	}
}

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

func TestReadFile_Success(t *testing.T) {
	ws := mustTempDir(t)
	path := filepath.Join(ws, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tl := tool.NewReadFileTool(permFor(ws))
	out, err := tl.Execute(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out["content"] != "hello world" {
		t.Errorf("content: got %q, want %q", out["content"], "hello world")
	}
}

func TestReadFile_RejectsDirectory(t *testing.T) {
	ws := mustTempDir(t)
	sub := filepath.Join(ws, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	tl := tool.NewReadFileTool(permFor(ws))
	if _, err := tl.Execute(context.Background(), map[string]any{"path": sub}); err == nil {
		t.Error("expected error when reading a directory, got nil")
	}
}

func TestReadFile_WorkspaceEscapeRejected(t *testing.T) {
	ws := mustTempDir(t)
	outside := mustTempDir(t)
	outsideFile := filepath.Join(outside, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0o644)

	tl := tool.NewReadFileTool(permFor(ws))
	if _, err := tl.Execute(context.Background(), map[string]any{"path": outsideFile}); err == nil {
		t.Error("expected error when reading outside workspace, got nil")
	}
}

// ---------------------------------------------------------------------------
// write_file
// ---------------------------------------------------------------------------

func TestWriteFile_Success(t *testing.T) {
	ws := mustTempDir(t)
	path := filepath.Join(ws, "out.txt")

	tl := tool.NewWriteFileTool(permFor(ws))
	out, err := tl.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "written content",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out["written"] != true {
		t.Errorf("written flag: got %v", out["written"])
	}

	data, _ := os.ReadFile(path)
	if string(data) != "written content" {
		t.Errorf("file content: got %q", string(data))
	}
}

func TestWriteFile_AllowWritesFalseBlocks(t *testing.T) {
	ws := mustTempDir(t)
	perm := tool.ToolPermission{AllowWrites: false, WorkspaceDir: ws}
	tl := tool.NewWriteFileTool(perm)

	_, err := tl.Execute(context.Background(), map[string]any{
		"path":    filepath.Join(ws, "file.txt"),
		"content": "x",
	})
	if err == nil {
		t.Fatal("expected error when allow_writes=false, got nil")
	}
	if !strings.Contains(err.Error(), "allow_writes=false") {
		t.Errorf("error message should mention allow_writes=false, got: %v", err)
	}
}

func TestWriteFile_WorkspaceEscapeRejected(t *testing.T) {
	ws := mustTempDir(t)
	outside := mustTempDir(t)

	perm := tool.ToolPermission{AllowWrites: true, WorkspaceDir: ws}
	tl := tool.NewWriteFileTool(perm)

	_, err := tl.Execute(context.Background(), map[string]any{
		"path":    filepath.Join(outside, "file.txt"),
		"content": "x",
	})
	if err == nil {
		t.Error("expected error when writing outside workspace, got nil")
	}
}

// ---------------------------------------------------------------------------
// list_files
// ---------------------------------------------------------------------------

func TestListFiles_Success(t *testing.T) {
	ws := mustTempDir(t)
	_ = os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "b.txt"), []byte("b"), 0o644)
	_ = os.MkdirAll(filepath.Join(ws, "subdir"), 0o755)

	tl := tool.NewListFilesTool(permFor(ws))
	out, err := tl.Execute(context.Background(), map[string]any{"path": ws})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	files, ok := out["files"].([]string)
	if !ok {
		t.Fatalf("files key is not []string: %T", out["files"])
	}

	// Should have 3 entries; subdir ends with "/".
	if len(files) != 3 {
		t.Errorf("expected 3 entries, got %d: %v", len(files), files)
	}

	var foundDir bool
	for _, f := range files {
		if f == "subdir/" {
			foundDir = true
		}
	}
	if !foundDir {
		t.Errorf("expected 'subdir/' in listing, got: %v", files)
	}
}

// ---------------------------------------------------------------------------
// search_text
// ---------------------------------------------------------------------------

func TestSearchText_BasicMatch(t *testing.T) {
	ws := mustTempDir(t)
	_ = os.WriteFile(filepath.Join(ws, "file1.txt"), []byte("hello world\nfoo bar\n"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "file2.txt"), []byte("no match here\n"), 0o644)

	tl := tool.NewSearchTextTool(permFor(ws))
	out, err := tl.Execute(context.Background(), map[string]any{
		"path":      ws,
		"pattern":   "hello",
		"recursive": false,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	matches, ok := out["matches"].([]map[string]any)
	if !ok {
		t.Fatalf("matches is not []map[string]any: %T", out["matches"])
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
	if matches[0]["line"] != 1 {
		t.Errorf("line: got %v, want 1", matches[0]["line"])
	}
}

func TestSearchText_Recursive(t *testing.T) {
	ws := mustTempDir(t)
	sub := filepath.Join(ws, "sub")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(ws, "top.txt"), []byte("needle top\n"), 0o644)
	_ = os.WriteFile(filepath.Join(sub, "deep.txt"), []byte("needle deep\n"), 0o644)

	tl := tool.NewSearchTextTool(permFor(ws))
	out, err := tl.Execute(context.Background(), map[string]any{
		"path":      ws,
		"pattern":   "needle",
		"recursive": true,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	matches := out["matches"].([]map[string]any)
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}
}

func TestSearchText_InvalidPatternError(t *testing.T) {
	ws := mustTempDir(t)
	tl := tool.NewSearchTextTool(permFor(ws))
	_, err := tl.Execute(context.Background(), map[string]any{
		"path":    ws,
		"pattern": "[invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestSearchText_NoMatchesReturnsEmptySlice(t *testing.T) {
	ws := mustTempDir(t)
	_ = os.WriteFile(filepath.Join(ws, "file.txt"), []byte("nothing here\n"), 0o644)

	tl := tool.NewSearchTextTool(permFor(ws))
	out, err := tl.Execute(context.Background(), map[string]any{
		"path":    ws,
		"pattern": "xyz_no_match",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	matches := out["matches"].([]map[string]any)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

// ---------------------------------------------------------------------------
// ask_user
// ---------------------------------------------------------------------------

func TestAskUser_ReturnsQuestion(t *testing.T) {
	tl := tool.NewAskUserTool()
	out, err := tl.Execute(context.Background(), map[string]any{
		"question_id": "q1",
		"text":        "What is the answer?",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out["question_id"] != "q1" {
		t.Errorf("question_id: got %v", out["question_id"])
	}
	if out["text"] != "What is the answer?" {
		t.Errorf("text: got %v", out["text"])
	}
}

func TestAskUser_MissingFieldReturnsError(t *testing.T) {
	tl := tool.NewAskUserTool()
	if _, err := tl.Execute(context.Background(), map[string]any{
		"question_id": "q1",
		// "text" missing
	}); err == nil {
		t.Fatal("expected error for missing text field")
	}
}

// ---------------------------------------------------------------------------
// Tool interface compliance — all built-in tools satisfy the interface
// ---------------------------------------------------------------------------

func TestToolInterface_BuiltIns(t *testing.T) {
	ws := mustTempDir(t)
	perm := permFor(ws)

	tools := []tool.Tool{
		tool.NewReadFileTool(perm),
		tool.NewWriteFileTool(perm),
		tool.NewListFilesTool(perm),
		tool.NewSearchTextTool(perm),
		tool.NewAskUserTool(),
	}

	for _, tl := range tools {
		if tl.Name() == "" {
			t.Errorf("tool Name() is empty")
		}
		if tl.Description() == "" {
			t.Errorf("tool %q Description() is empty", tl.Name())
		}
		schema := tl.InputSchema()
		if schema == nil {
			t.Errorf("tool %q InputSchema() returned nil", tl.Name())
		}
	}
}
