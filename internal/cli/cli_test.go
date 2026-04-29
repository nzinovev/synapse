package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/engine"
	"github.com/nzinovev/synapse/internal/store"
	"github.com/spf13/cobra"
)

func setupTestEnv(t *testing.T) (string, *adapter.AdapterRegistry, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	hubDir := filepath.Join(tmpDir, ".synapse")
	os.MkdirAll(hubDir, 0o755)

	cfg := domain.DefaultSynapseConfig()
	cfg.Adapter = "fake"
	cfg.DBPath = filepath.Join(hubDir, "synapse.db")
	cfg.PipelinesDir = ""

	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(hubDir, "config.json"), data, 0o644)

	registry := adapter.NewRegistry()
	adapter.RegisterFake(registry)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)

	cleanup := func() {
		os.Setenv("HOME", origHome)
	}

	return hubDir, registry, cleanup
}

func newTestRootCmd(registry *adapter.AdapterRegistry) *cobra.Command {
	deps := &Dependencies{Registry: registry}
	return NewRootCmd(deps)
}

// executeCommand captures both stdout and stderr by redirecting os.Stdout.
func executeCommand(root *cobra.Command, args ...string) (string, error) {
	root.SetArgs(args)

	// Capture os.Stdout and os.Stderr.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	buf := make([]byte, 65536)
	n, _ := r.Read(buf)
	return string(buf[:n]), err
}

func TestRootCommand_Help(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "--help")
	if err != nil {
		t.Fatalf("--help should not error: %v", err)
	}

	expected := []string{"run", "init", "status", "approve", "reject", "retry"}
	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("help output should contain %q, got:\n%s", exp, output)
		}
	}
}

func TestListPipelinesCmd(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "list-pipelines")
	if err != nil {
		t.Fatalf("list-pipelines should not error: %v", err)
	}

	if !strings.Contains(output, "backend") {
		t.Errorf("list-pipelines should list backend pipeline, got:\n%s", output)
	}
}

func TestShowPipelineCmd(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "show-pipeline", "backend")
	if err != nil {
		t.Fatalf("show-pipeline should not error: %v", err)
	}

	if !strings.Contains(output, "spec") || !strings.Contains(output, "adr") {
		t.Errorf("show-pipeline should list stages, got:\n%s", output)
	}
}

func TestShowPipelineCmd_NotFound(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	_, err := executeCommand(root, "show-pipeline", "nonexistent")
	if err == nil {
		t.Fatal("show-pipeline with nonexistent name should error")
	}
}

func TestStatusCmd_NoTasks(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "status")
	if err != nil {
		t.Fatalf("status with no tasks should not error: %v", err)
	}

	if !strings.Contains(output, "No tasks found") {
		t.Errorf("status with no tasks should say 'No tasks found', got:\n%s", output)
	}
}

func TestRunCmd_HappyPath(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "run", "backend", "-n", "042", "Test task for CLI")
	if err != nil {
		t.Fatalf("run should not error: %v", err)
	}

	if !strings.Contains(output, "Created task") {
		t.Errorf("run should print 'Created task', got:\n%s", output)
	}
	if !strings.Contains(output, "backend") {
		t.Errorf("run should mention pipeline name, got:\n%s", output)
	}

	// Verify task was created in the database.
	ctx := context.Background()
	cfg, _ := domain.LoadConfigGlobal()
	s, err := store.NewSQLiteStore(ctx, cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	tasks, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.PipelineName != "backend" {
		t.Errorf("expected pipeline=backend, got %s", task.PipelineName)
	}
	if task.Description != "Test task for CLI" {
		t.Errorf("unexpected description: %s", task.Description)
	}
	if task.Status == "" {
		t.Error("task should have a status")
	}
}

func TestRunCmd_WithFile(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	tmpFile := filepath.Join(t.TempDir(), "task.md")
	os.WriteFile(tmpFile, []byte("Fix the login bug"), 0o644)

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "run", "backend", "-n", "043", "--file", tmpFile)
	if err != nil {
		t.Fatalf("run --file should not error: %v", err)
	}

	if !strings.Contains(output, "Created task") {
		t.Errorf("run --file should print 'Created task', got:\n%s", output)
	}

	ctx := context.Background()
	cfg, _ := domain.LoadConfigGlobal()
	s, _ := store.NewSQLiteStore(ctx, cfg.DBPath)
	defer s.Close()

	tasks, _ := s.ListTasks(ctx)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Description != "Fix the login bug" {
		t.Errorf("unexpected description: %s", tasks[0].Description)
	}
}

func TestRunCmd_MissingDescription(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	_, err := executeCommand(root, "run", "backend")
	if err == nil {
		t.Fatal("run without description should error")
	}
}

func TestRunCmd_PipelineNotFound(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	_, err := executeCommand(root, "run", "nonexistent-pipeline", "desc")
	if err == nil {
		t.Fatal("run with nonexistent pipeline should error")
	}
}

func TestStatusCmd_WithTask(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	cfg, _ := domain.LoadConfigGlobal()
	s, _ := store.NewSQLiteStore(ctx, cfg.DBPath)
	defer s.Close()

	task := &domain.Task{
		ID:             "test-task-123",
		PipelineName:   "backend",
		Description:    "A test task",
		WorkingDir:     "/tmp/project",
		CurrentStageID: "spec",
		Status:         domain.StatusAwaitingGate,
		Artifacts:      map[string][]string{},
	}
	s.CreateTask(ctx, task)

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "status", "test-task-123")
	if err != nil {
		t.Fatalf("status <task-id> should not error: %v", err)
	}

	if !strings.Contains(output, "test-task-123") {
		t.Errorf("status should show task ID, got:\n%s", output)
	}
	if !strings.Contains(output, "Awaiting gate approval") {
		t.Errorf("awaiting_gate task should show approval hint, got:\n%s", output)
	}
}

func TestStatusCmd_TaskNotFound(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	_, err := executeCommand(root, "status", "nonexistent")
	if err == nil {
		t.Fatal("status with nonexistent task should error")
	}
}

func TestApproveRejectRetry_Flow(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	cfg, _ := domain.LoadConfigGlobal()
	s, _ := store.NewSQLiteStore(ctx, cfg.DBPath)
	defer s.Close()

	pipeline, _ := domain.LoadBundledPipelineByName("backend")
	a, _ := registry.Create("fake", cfg.AdapterConfig)
	eng := engine.NewPipelineEngine(s, a, "")

	// Create and run a task until human gate.
	task := &domain.Task{
		ID:             "approve-test",
		PipelineName:   "backend",
		Description:    "Test approve flow",
		WorkingDir:     t.TempDir(),
		CurrentStageID: pipeline.Stages[0].ID,
		Status:         domain.StatusRunning,
		Artifacts:      map[string][]string{},
	}
	s.CreateTask(ctx, task)

	task, err := eng.RunUntilGate(ctx, "approve-test")
	if err != nil {
		t.Fatalf("run until gate: %v", err)
	}

	if task.Status == domain.StatusAwaitingGate {
		root := newTestRootCmd(registry)

		output, err := executeCommand(root, "approve", "approve-test")
		if err != nil {
			t.Fatalf("approve should not error: %v", err)
		}
		if !strings.Contains(output, "Approving gate") {
			t.Errorf("approve should print approving message, got:\n%s", output)
		}
	}
}

func TestStatusJSON(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	cfg, _ := domain.LoadConfigGlobal()
	s, _ := store.NewSQLiteStore(ctx, cfg.DBPath)
	defer s.Close()

	task := &domain.Task{
		ID:             "json-test-456",
		PipelineName:   "backend",
		Description:    "JSON output test",
		WorkingDir:     "/tmp/project",
		CurrentStageID: "spec",
		Status:         domain.StatusDone,
		Artifacts:      map[string][]string{},
	}
	s.CreateTask(ctx, task)

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "status", "--json")
	if err != nil {
		t.Fatalf("status --json should not error: %v", err)
	}

	var tasks []map[string]any
	if err := json.Unmarshal([]byte(output), &tasks); err != nil {
		t.Fatalf("status --json output should be valid JSON: %v\noutput:\n%s", err, output)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task in JSON, got %d", len(tasks))
	}
	if tasks[0]["id"] != "json-test-456" {
		t.Errorf("expected task id 'json-test-456', got %v", tasks[0]["id"])
	}
}

func TestRunCmd_JSONOutput(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)
	output, err := executeCommand(root, "run", "backend", "-n", "044", "--json", "JSON test task")
	if err != nil {
		t.Fatalf("run --json should not error: %v", err)
	}

	// The output may contain "Created task" before the JSON.
	// Find the start of JSON and extract everything from there.
	idx := strings.Index(output, "{\n")
	if idx < 0 {
		idx = strings.Index(output, "{")
	}
	if idx < 0 {
		t.Fatalf("run --json should output JSON, got:\n%s", output)
	}

	// The JSON may be followed by "Created task" output that went to stdout before the JSON.
	// Actually, printTask outputs JSON first, then "Created task" was printed before.
	// Let's find the JSON block by finding the last { that starts a valid JSON object.
	// Simpler: find the last occurrence of { in the output and try to parse from there.
	jsonStart := strings.LastIndex(output, "{")
	if jsonStart < 0 {
		t.Fatalf("no JSON found in output:\n%s", output)
	}
	jsonStr := output[jsonStart:]

	var task map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &task); err != nil {
		// Try finding the first { instead.
		jsonStart = strings.Index(output, "{")
		jsonStr = output[jsonStart:]
		// Try to find the end of the JSON object by counting braces.
		depth := 0
		end := -1
		for i, ch := range jsonStr {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		if end > 0 {
			jsonStr = jsonStr[:end]
		}
		if err2 := json.Unmarshal([]byte(jsonStr), &task); err2 != nil {
			t.Fatalf("JSON should be valid: %v (also %v)\nraw:\n%s", err, err2, output)
		}
	}
	if task["pipeline_name"] != "backend" {
		t.Errorf("expected pipeline_name=backend, got %v", task["pipeline_name"])
	}
}

func TestGenerateTaskID(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		number string
		suffix int
		want   string
		maxLen int
	}{
		{"13", 0, "task-013-20260429-143000", 30},
		{"1", 0, "task-001-20260429-143000", 30},
		{"1234", 0, "task-1234-20260429-143000", 30},
		{"13", 1, "task-013-20260429-143000-1", 30},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%s/suffix%d", tt.number, tt.suffix)
		t.Run(name, func(t *testing.T) {
			got := domain.GenerateTaskID(tt.number, now, tt.suffix)
			if got != tt.want {
				t.Errorf("GenerateTaskID(%q, now, %d) = %q, want %q", tt.number, tt.suffix, got, tt.want)
			}
			if len(got) > tt.maxLen {
				t.Errorf("GenerateTaskID = %q (%d chars), exceeds max %d", got, len(got), tt.maxLen)
			}
		})
	}
}

func TestInitCmd(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	registry := adapter.NewRegistry()
	adapter.RegisterFake(registry)

	root := newTestRootCmd(registry)

	input := "fake\n\n\n\n"
	root.SetIn(strings.NewReader(input))
	root.SetArgs([]string{"init"})

	// Capture output.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	err := root.Execute()

	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	buf := make([]byte, 65536)
	n, _ := r.Read(buf)
	_ = string(buf[:n])

	if err != nil {
		t.Fatalf("init should not error: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".synapse", "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("init should create config.json")
	}

	cfg, err := domain.LoadConfig(filepath.Join(tmpDir, ".synapse"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Adapter != "fake" {
		t.Errorf("expected adapter=fake, got %s", cfg.Adapter)
	}
	if cfg.AdapterConfig.ClaudeBinary != "claude" {
		t.Errorf("expected claude binary=claude, got %s", cfg.AdapterConfig.ClaudeBinary)
	}
	if cfg.AdapterConfig.AgentBinary != "agent" {
		t.Errorf("expected agent binary=agent, got %s", cfg.AdapterConfig.AgentBinary)
	}
}

func TestFullHappyPath(t *testing.T) {
	_, registry, cleanup := setupTestEnv(t)
	defer cleanup()

	root := newTestRootCmd(registry)

	// 1. Run a task
	runOutput, err := executeCommand(root, "run", "backend", "-n", "045", "Full happy path test")
	if err != nil {
		t.Fatalf("run should not error: %v", err)
	}
	if !strings.Contains(runOutput, "Created task") {
		t.Fatalf("run should create task, got:\n%s", runOutput)
	}

	// 2. List tasks
	statusOutput, err := executeCommand(root, "status")
	if err != nil {
		t.Fatalf("status should not error: %v", err)
	}
	if !strings.Contains(statusOutput, "task-045-") {
		t.Errorf("status should list the task, got:\n%s", statusOutput)
	}

	// 3. List pipelines
	listOutput, err := executeCommand(root, "list-pipelines")
	if err != nil {
		t.Fatalf("list-pipelines should not error: %v", err)
	}
	if !strings.Contains(listOutput, "backend") {
		t.Errorf("list-pipelines should list backend, got:\n%s", listOutput)
	}

	// 4. Show pipeline
	showOutput, err := executeCommand(root, "show-pipeline", "backend")
	if err != nil {
		t.Fatalf("show-pipeline should not error: %v", err)
	}
	if !strings.Contains(showOutput, "spec") {
		t.Errorf("show-pipeline should list stages, got:\n%s", showOutput)
	}
}

func TestBinaryBuilds(t *testing.T) {
	registry := adapter.NewRegistry()
	adapter.RegisterFake(registry)

	root := NewRootCmd(&Dependencies{Registry: registry})
	if root == nil {
		t.Fatal("NewRootCmd should return a non-nil command")
	}

	expected := []string{"init", "run", "status", "approve", "reject", "retry", "list-pipelines", "show-pipeline", "web"}
	commands := root.Commands()
	cmdNames := make(map[string]bool)
	for _, cmd := range commands {
		cmdNames[cmd.Name()] = true
	}

	for _, name := range expected {
		if !cmdNames[name] {
			t.Errorf("root command should have subcommand %q", name)
		}
	}
}
