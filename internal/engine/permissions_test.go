package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/store"
	"github.com/nzinovev/synapse/internal/tool"
)

// initGitRepo creates a git repository at dir. Skips the test if git is unavailable.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", dir)
	if err := cmd.Run(); err != nil {
		t.Skip("git not available:", err)
	}
}

// newNativeTestEngine builds an engine that routes native stages through factory.
func newNativeTestEngine(t *testing.T, pipeline *domain.Pipeline, factory NativeFactory) (*PipelineEngine, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	eng := NewPipelineEngineWithRegistry(s, nil, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline
	eng.SetNativeFactory(factory)
	return eng, func() { s.Close() }
}

// createNativeTask creates and saves a running task pointing at workDir.
func createNativeTask(t *testing.T, s store.TaskStore, workDir, stageID string) *domain.Task {
	t.Helper()
	task := &domain.Task{
		ID:             "perm-task-001",
		PipelineName:   "test",
		Description:    "permissions test",
		WorkingDir:     workDir,
		CurrentStageID: stageID,
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	if err := s.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// singleNativeStage returns a one-stage pipeline with the given stage config.
func singleNativeStage(perm *domain.StagePermissions) *domain.Pipeline {
	return &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{
				ID:          "work",
				Agent:       "spec-writer", // bundled agent — LoadAgent will succeed
				Gate:        domain.GateAuto,
				Runtime:     "native",
				Permissions: perm,
			},
		},
	}
}

// writeFileFactory returns a NativeFactory whose agent writes the listed paths
// (relative to the RunInput workspace) before returning StatusCompleted.
func writeFileFactory(relPaths []string) NativeFactory {
	return func(def agent.AgentDefinition, perm tool.ToolPermission) (agent.Agent, error) {
		return &agent.MockAgent{
			RunFn: func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
				for _, p := range relPaths {
					full := filepath.Join(input.WorkspacePath, p)
					if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
						return agent.RunResult{}, err
					}
					if err := os.WriteFile(full, []byte("content\n"), 0o644); err != nil {
						return agent.RunResult{}, err
					}
				}
				return agent.RunResult{
					SchemaVersion: agent.SchemaVersion,
					Status:        agent.StatusCompleted,
				}, nil
			},
		}, nil
	}
}

// noopFactory returns a NativeFactory whose agent writes nothing.
func noopFactory() NativeFactory {
	return func(def agent.AgentDefinition, perm tool.ToolPermission) (agent.Agent, error) {
		return &agent.MockAgent{}, nil
	}
}

func runEngineAndGetTask(t *testing.T, eng *PipelineEngine, workDir, stageID string) *domain.Task {
	t.Helper()
	// Access the store via the engine to create the task.
	task := createNativeTask(t, eng.store, workDir, stageID)
	result, err := eng.RunUntilGate(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}
	return result
}

// --- Tests ---

func TestPermissionsNativeAllowWritesFalse_PostStageBlocks(t *testing.T) {
	workDir := t.TempDir()
	initGitRepo(t, workDir)

	perm := &domain.StagePermissions{AllowWrites: boolPtr(false)}
	pipeline := singleNativeStage(perm)
	eng, cleanup := newNativeTestEngine(t, pipeline, writeFileFactory([]string{"newfile.txt"}))
	defer cleanup()

	task := runEngineAndGetTask(t, eng, workDir, "work")

	if task.Status != domain.StatusBlocked {
		t.Errorf("status = %s, want %s", task.Status, domain.StatusBlocked)
	}
	lastRun := task.Runs[len(task.Runs)-1]
	if lastRun.AgentResult == nil || !strings.Contains(lastRun.AgentResult.Stderr, "stage permissions violation") {
		t.Errorf("expected 'stage permissions violation' in stderr, got: %v", lastRun.AgentResult)
	}
}

func TestPermissionsNativeMaxChangedFilesExceeded(t *testing.T) {
	workDir := t.TempDir()
	initGitRepo(t, workDir)

	perm := &domain.StagePermissions{AllowWrites: boolPtr(true), MaxChangedFiles: 1}
	pipeline := singleNativeStage(perm)
	eng, cleanup := newNativeTestEngine(t, pipeline, writeFileFactory([]string{"file1.txt", "file2.txt"}))
	defer cleanup()

	task := runEngineAndGetTask(t, eng, workDir, "work")

	if task.Status != domain.StatusBlocked {
		t.Errorf("status = %s, want %s", task.Status, domain.StatusBlocked)
	}
	lastRun := task.Runs[len(task.Runs)-1]
	if lastRun.AgentResult == nil || !strings.Contains(lastRun.AgentResult.Stderr, "stage permissions violation") {
		t.Errorf("expected 'stage permissions violation' in stderr, got: %v", lastRun.AgentResult)
	}
}

func TestPermissionsNativeBlockedPath_PostStageCatch(t *testing.T) {
	workDir := t.TempDir()
	initGitRepo(t, workDir)

	perm := &domain.StagePermissions{AllowWrites: boolPtr(true), BlockedPaths: []string{".env"}}
	pipeline := singleNativeStage(perm)
	eng, cleanup := newNativeTestEngine(t, pipeline, writeFileFactory([]string{".env"}))
	defer cleanup()

	task := runEngineAndGetTask(t, eng, workDir, "work")

	if task.Status != domain.StatusBlocked {
		t.Errorf("status = %s, want %s", task.Status, domain.StatusBlocked)
	}
	lastRun := task.Runs[len(task.Runs)-1]
	if lastRun.AgentResult == nil || !strings.Contains(lastRun.AgentResult.Stderr, "stage permissions violation") {
		t.Errorf("expected 'stage permissions violation' in stderr, got: %v", lastRun.AgentResult)
	}
}

func TestPermissionsNativeDefaults_BlocksUnauthorizedWrite(t *testing.T) {
	workDir := t.TempDir()
	initGitRepo(t, workDir)

	// No permissions block — native defaults apply (allow_writes: false).
	pipeline := singleNativeStage(nil)
	eng, cleanup := newNativeTestEngine(t, pipeline, writeFileFactory([]string{"newfile.txt"}))
	defer cleanup()

	task := runEngineAndGetTask(t, eng, workDir, "work")

	if task.Status != domain.StatusBlocked {
		t.Errorf("status = %s, want %s", task.Status, domain.StatusBlocked)
	}
	lastRun := task.Runs[len(task.Runs)-1]
	if lastRun.AgentResult == nil || !strings.Contains(lastRun.AgentResult.Stderr, "stage permissions violation") {
		t.Errorf("expected 'stage permissions violation' in stderr, got: %v", lastRun.AgentResult)
	}
}

func TestPermissionsNativeExplicitAllowWrites_Passes(t *testing.T) {
	workDir := t.TempDir()
	initGitRepo(t, workDir)

	perm := &domain.StagePermissions{AllowWrites: boolPtr(true)}
	pipeline := singleNativeStage(perm)
	eng, cleanup := newNativeTestEngine(t, pipeline, writeFileFactory([]string{"newfile.txt"}))
	defer cleanup()

	task := runEngineAndGetTask(t, eng, workDir, "work")

	if task.Status == domain.StatusBlocked {
		var stderr string
		if lastRun := task.Runs[len(task.Runs)-1]; lastRun.AgentResult != nil {
			stderr = lastRun.AgentResult.Stderr
		}
		t.Errorf("status = %s (blocked unexpectedly); stderr: %s", task.Status, stderr)
	}
}

func TestPermissionsCliNotPostStagePoliced(t *testing.T) {
	workDir := t.TempDir()
	// No git init needed — CLI agents skip post-stage enforcement.

	allowWritesFalse := boolPtr(false)
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{
				ID:      "work",
				Agent:   "mock-cli",
				Gate:    domain.GateAuto,
				Runtime: "cli",
				Permissions: &domain.StagePermissions{
					AllowWrites: allowWritesFalse,
				},
			},
		},
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := store.NewSQLiteStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	reg := agent.NewAgentRegistry()
	reg.Register("mock-cli", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{}, nil
	})
	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline

	task := createNativeTask(t, s, workDir, "work")
	result, err := eng.RunUntilGate(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status == domain.StatusBlocked {
		t.Errorf("CLI agent was blocked by permissions enforcement; want not blocked")
	}
}

func TestPermissionsToolPermissionDerivedFromStage(t *testing.T) {
	workDir := t.TempDir()
	initGitRepo(t, workDir)

	perm := &domain.StagePermissions{
		AllowWrites: boolPtr(false),
		AllowShell:  boolPtr(false),
	}
	pipeline := singleNativeStage(perm)

	var capturedPerm tool.ToolPermission
	capturingFactory := func(def agent.AgentDefinition, p tool.ToolPermission) (agent.Agent, error) {
		capturedPerm = p
		return &agent.MockAgent{}, nil // writes nothing, so permissions check won't fire
	}

	eng, cleanup := newNativeTestEngine(t, pipeline, capturingFactory)
	defer cleanup()

	task := createNativeTask(t, eng.store, workDir, "work")
	eng.RunUntilGate(context.Background(), task.ID) //nolint:errcheck

	if capturedPerm.AllowWrites != false {
		t.Errorf("AllowWrites = %v, want false", capturedPerm.AllowWrites)
	}
	if capturedPerm.AllowShell != false {
		t.Errorf("AllowShell = %v, want false", capturedPerm.AllowShell)
	}
}
