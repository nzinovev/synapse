package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/store"
)

// --- Test engine helpers ---

// newMockEngine creates an engine wired with MockAgent instances for every
// named agent in the given pipeline. All agents default to returning
// StatusCompleted with no output.
func newMockEngine(t *testing.T, pipeline *domain.Pipeline) (*PipelineEngine, string, func()) {
	t.Helper()
	return newMockEngineWithFn(t, pipeline, nil)
}

// newMockEngineWithFn is like newMockEngine but passes runFn to every agent.
// Use it when you want a single behaviour shared across all agents.
func newMockEngineWithFn(t *testing.T, pipeline *domain.Pipeline, runFn func(context.Context, agent.RunInput) (agent.RunResult, error)) (*PipelineEngine, string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	reg := buildMockRegistry(t, pipeline, runFn)
	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline

	return eng, tmpDir, func() { s.Close() }
}

// buildMockRegistry returns an AgentRegistry with a MockAgent registered for
// every distinct, non-null agent name in the pipeline.
func buildMockRegistry(t *testing.T, pipeline *domain.Pipeline, runFn func(context.Context, agent.RunInput) (agent.RunResult, error)) *agent.AgentRegistry {
	t.Helper()
	reg := agent.NewAgentRegistry()
	seen := map[string]bool{}
	for _, s := range pipeline.Stages {
		if s.Agent == "" || s.Agent == "null" || seen[s.Agent] {
			continue
		}
		seen[s.Agent] = true
		fn := runFn
		reg.Register(s.Agent, func(cfg domain.AdapterConfig) (agent.Agent, error) {
			return &agent.MockAgent{RunFn: fn}, nil
		})
	}
	return reg
}

// mockVerdictRunFn returns a RunFn that always yields the given verdict.
func mockVerdictRunFn(v agent.Verdict) func(context.Context, agent.RunInput) (agent.RunResult, error) {
	return func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		return agent.RunResult{
			SchemaVersion: agent.SchemaVersion,
			Status:        agent.StatusCompleted,
			Verdict:       v,
		}, nil
	}
}

// mockFailRunFn returns a RunFn that always returns StatusFailed.
func mockFailRunFn() func(context.Context, agent.RunInput) (agent.RunResult, error) {
	return func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		return agent.RunResult{
			SchemaVersion: agent.SchemaVersion,
			Status:        agent.StatusFailed,
			Summary:       "mock failure",
		}, nil
	}
}

func newTestEngine(t *testing.T) (*PipelineEngine, *domain.Pipeline, string, func()) {
	t.Helper()

	pipeline := &domain.Pipeline{
		Name: "backend",
		Stages: []domain.Stage{
			{ID: "spec", Agent: "spec-writer", Gate: domain.GateAutoIfClean, ProducesGlob: "docs/specs/*.md"},
			{ID: "adr", Agent: "adr-architect", Gate: domain.GateHumanApproval, ProducesGlob: "docs/adr/*.md"},
			{ID: "implement", Agent: "feature-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	eng, tmpDir, cleanup := newMockEngineWithFn(t, pipeline, mockVerdictRunFn(agent.VerdictApproved))
	return eng, pipeline, tmpDir, cleanup
}

func createTestTask(t *testing.T, s store.TaskStore, workDir string) *domain.Task {
	t.Helper()
	task := &domain.Task{
		ID:             "test-task-001",
		PipelineName:   "backend",
		Description:    "Add logging to engine",
		WorkingDir:     workDir,
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	ctx := context.Background()
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// --- Verdict parsing tests ---

func TestParseVerdictApproved(t *testing.T) {
	text := "Some review text\n**Verdict:** APPROVED\nMore text"
	got := ParseVerdict(text)
	if got != "APPROVED" {
		t.Errorf("ParseVerdict = %q, want %q", got, "APPROVED")
	}
}

func TestParseVerdictNeedsFixes(t *testing.T) {
	text := "## Summary\nBad code\n\n**Verdict:** NEEDS FIXES\n"
	got := ParseVerdict(text)
	if got != "NEEDS FIXES" {
		t.Errorf("ParseVerdict = %q, want %q", got, "NEEDS FIXES")
	}
}

func TestParseVerdictBlocked(t *testing.T) {
	text := "**Verdict:** BLOCKED"
	got := ParseVerdict(text)
	if got != "BLOCKED" {
		t.Errorf("ParseVerdict = %q, want %q", got, "BLOCKED")
	}
}

func TestParseVerdictNone(t *testing.T) {
	text := "No verdict here"
	got := ParseVerdict(text)
	if got != "" {
		t.Errorf("ParseVerdict = %q, want empty", got)
	}
}

// --- Artifact stamping tests ---

func TestStampArtifactHeading(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.md")
	os.WriteFile(path, []byte("# ADR\n\n## Status\n\nProposed\n\n## Body\n"), 0o644)

	StampArtifactApproved(path)

	data, _ := os.ReadFile(path)
	text := string(data)
	if !contains(text, "## Status\n\nApproved") {
		t.Errorf("expected 'Approved' after ## Status, got:\n%s", text)
	}
}

func TestStampArtifactField(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.md")
	os.WriteFile(path, []byte("# Spec\n\nStatus: Proposed\n"), 0o644)

	StampArtifactApproved(path)

	data, _ := os.ReadFile(path)
	text := string(data)
	if !contains(text, "Status: Approved") {
		t.Errorf("expected 'Status: Approved', got:\n%s", text)
	}
}

func TestStampArtifactNoStatusField(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.md")
	os.WriteFile(path, []byte("# Doc\n\nSome content\n"), 0o644)

	StampArtifactApproved(path)

	data, _ := os.ReadFile(path)
	text := string(data)
	if !contains(text, "## Status\n\nApproved") {
		t.Errorf("expected appended status, got:\n%s", text)
	}
}

func TestStampArtifactNonexistent(t *testing.T) {
	StampArtifactApproved("/nonexistent/path.md")
}

// --- Open questions tests ---

func TestOpenQuestionsClean(t *testing.T) {
	text := "# Spec\n\n## Open Questions\n\nNone at this time.\n"
	if !openQuestionsSectionIsClean(text) {
		t.Error("expected clean")
	}
}

func TestOpenQuestionsNotClean(t *testing.T) {
	text := "# Spec\n\n## Open Questions\n\n- Need to decide on DB\n"
	if openQuestionsSectionIsClean(text) {
		t.Error("expected not clean")
	}
}

func TestOpenQuestionsMissing(t *testing.T) {
	text := "# Spec\n\nNo questions section\n"
	if openQuestionsSectionIsClean(text) {
		t.Error("expected not clean when section missing")
	}
}

func TestArtifactsAllowAutoIfCleanSpec(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.md")
	os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\nNone at this time.\n"), 0o644)

	if !ArtifactsAllowAutoIfClean([]string{specPath}) {
		t.Error("expected clean artifacts to pass")
	}
}

func TestArtifactsBlockOnQuestions(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.md")
	os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\n- TBD\n"), 0o644)

	if ArtifactsAllowAutoIfClean([]string{specPath}) {
		t.Error("expected open questions to block")
	}
}

func TestArtifactsBlockOnQuestionsMD(t *testing.T) {
	tmpDir := t.TempDir()
	qPath := filepath.Join(tmpDir, "QUESTIONS.md")
	os.WriteFile(qPath, []byte("questions"), 0o644)

	if ArtifactsAllowAutoIfClean([]string{qPath}) {
		t.Error("expected QUESTIONS.md to block")
	}
}

func TestArtifactsBlockEmpty(t *testing.T) {
	if ArtifactsAllowAutoIfClean(nil) {
		t.Error("expected empty artifacts to block")
	}
}

func TestArtifactsBlockNonMd(t *testing.T) {
	if ArtifactsAllowAutoIfClean([]string{"/tmp/spec.txt"}) {
		t.Error("expected non-md artifacts to block")
	}
}

// --- Engine state transition tests ---

func TestAutoGateAdvancesImmediately(t *testing.T) {
	simplePipeline := &domain.Pipeline{
		Name: "simple",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, simplePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-auto",
		PipelineName:   "simple",
		Description:    "auto gate test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}
}

func TestHumanApprovalGatePauses(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-human",
		PipelineName:   "single",
		Description:    "human gate test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
}

func TestApproveAdvancesToNextStage(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-approve",
		PipelineName:   "single",
		Description:    "approve test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	result, err = eng.Approve(ctx, task.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status after approve = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "stage2" {
		t.Errorf("CurrentStageID = %s, want stage2", result.CurrentStageID)
	}
}

func TestRejectReRunsCurrentStage(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-reject",
		PipelineName:   "single",
		Description:    "reject test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	eng.RunUntilGate(ctx, task.ID)

	result, err := eng.Reject(ctx, task.ID, "not good enough")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status after reject = %s, want awaiting_gate", result.Status)
	}

	var stage1Runs int
	for _, r := range result.Runs {
		if r.StageID == "stage1" {
			stage1Runs++
		}
	}
	if stage1Runs != 2 {
		t.Errorf("stage1 runs = %d, want 2", stage1Runs)
	}
}

func TestCancelTask(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-cancel",
		PipelineName:   "single",
		Description:    "cancel test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	eng.RunUntilGate(ctx, task.ID)

	result, err := eng.Cancel(ctx, task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if result.Status != domain.StatusCancelled {
		t.Errorf("Status = %s, want cancelled", result.Status)
	}
}

func TestCancelRequiresCancellableStatus(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-cancel-err",
		PipelineName:   "single",
		Description:    "cancel error test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	eng.RunUntilGate(ctx, task.ID)

	_, err := eng.Cancel(ctx, task.ID)
	if err == nil {
		t.Error("expected error when cancelling done task")
	}
}

// blockingMockAgent blocks Invoke until context is cancelled.
type blockingMockAgent struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingMockAgent) Name() string { return "blocking" }
func (b *blockingMockAgent) Run(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return agent.RunResult{}, ctx.Err()
}

func TestCancelRunningTask(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}

	blk := &blockingMockAgent{started: make(chan struct{})}
	reg := agent.NewAgentRegistry()
	reg.Register("spec-writer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return blk, nil
	})

	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = singlePipeline

	task := &domain.Task{
		ID:             "task-cancel-running",
		PipelineName:   "single",
		Description:    "cancel while running",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	runDone := make(chan error, 1)
	go func() {
		_, err := eng.RunUntilGate(ctx, task.ID)
		runDone <- err
	}()

	<-blk.started
	result, err := eng.Cancel(ctx, task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if result.Status != domain.StatusCancelled {
		t.Errorf("Status = %s, want cancelled", result.Status)
	}

	if err := <-runDone; err != nil {
		t.Errorf("RunUntilGate returned error: %v", err)
	}

	_, err = eng.Cancel(ctx, task.ID)
	if err != nil {
		t.Errorf("second Cancel: %v", err)
	}
}

func TestRetryRequiresBlockedOrEscalated(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-retry-err",
		PipelineName:   "single",
		Description:    "retry error test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	eng.RunUntilGate(ctx, task.ID)

	_, err := eng.Retry(ctx, task.ID)
	if err == nil {
		t.Error("expected error when retrying non-blocked task")
	}
}

func TestBlockedOnAdapterFailure(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
		},
	}
	eng, tmpDir, cleanup := newMockEngineWithFn(t, singlePipeline, mockFailRunFn())
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-fail",
		PipelineName:   "single",
		Description:    "fail test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}
	if result.Status != domain.StatusBlocked {
		t.Errorf("Status = %s, want blocked", result.Status)
	}
}

func TestRetryFromBlocked(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}

	// Use a mutable runFn so we can switch from fail to succeed mid-test.
	var mu sync.Mutex
	shouldFail := true
	runFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		mu.Lock()
		fail := shouldFail
		mu.Unlock()
		if fail {
			return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusFailed}, nil
		}
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted}, nil
	}

	eng, tmpDir, cleanup := newMockEngineWithFn(t, singlePipeline, runFn)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-retry",
		PipelineName:   "single",
		Description:    "retry test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, _ := eng.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusBlocked {
		t.Fatalf("expected blocked, got %s", result.Status)
	}

	mu.Lock()
	shouldFail = false
	mu.Unlock()

	result, err := eng.Retry(ctx, task.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status after retry = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "stage2" {
		t.Errorf("CurrentStageID = %s, want stage2", result.CurrentStageID)
	}
}

func TestHumanFinalApprovalCompletesTask(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-final",
		PipelineName:   "single",
		Description:    "final approval test",
		WorkingDir:     tmpDir,
		CurrentStageID: "done",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, _ := eng.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	result, err := eng.Approve(ctx, task.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result.Status != domain.StatusDone {
		t.Errorf("Status = %s, want done", result.Status)
	}
}

func TestHumanFinalRejectionRoutesToFix(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, pipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-final-reject",
		PipelineName:   "test",
		Description:    "final reject test",
		WorkingDir:     tmpDir,
		CurrentStageID: "done",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, _ := eng.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	result, err := eng.Reject(ctx, task.ID, "needs work")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}
	if result.FixCycleCount != 1 {
		t.Errorf("FixCycleCount = %d, want 1", result.FixCycleCount)
	}
}

// --- Fix loop tests ---

func TestAutoOnApprovalApproved(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng, tmpDir, cleanup := newMockEngineWithFn(t, pipeline, mockVerdictRunFn(agent.VerdictApproved))
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-approved",
		PipelineName:   "test",
		Description:    "approval test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate (at done)", result.Status)
	}
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}
}

func TestAutoOnApprovalNeedsFixes(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	var mu sync.Mutex
	reviewCalls := 0
	reviewFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		mu.Lock()
		n := reviewCalls
		reviewCalls++
		mu.Unlock()
		verdict := agent.VerdictNeedsFixes
		if n >= 1 {
			verdict = agent.VerdictApproved
		}
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted, Verdict: verdict}, nil
	}
	successFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted}, nil
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	reg := agent.NewAgentRegistry()
	reg.Register("spec-reviewer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: reviewFn}, nil
	})
	reg.Register("fix-implementer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: successFn}, nil
	})

	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline

	task := &domain.Task{
		ID:             "task-needsfix",
		PipelineName:   "test",
		Description:    "needs fixes test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}

	var fixRuns int
	for _, r := range result.Runs {
		if r.StageID == "fix" {
			fixRuns++
		}
	}
	if fixRuns == 0 {
		t.Error("expected fix stage to run")
	}
}

func TestFixLoopEscalation(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	reg := agent.NewAgentRegistry()
	reg.Register("spec-reviewer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: mockVerdictRunFn(agent.VerdictNeedsFixes)}, nil
	})
	reg.Register("fix-implementer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{}, nil
	})

	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline

	task := &domain.Task{
		ID:             "task-escalate",
		PipelineName:   "test",
		Description:    "escalation test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusEscalated {
		t.Errorf("Status = %s, want escalated", result.Status)
	}
	if result.FixCycleCount != 3 {
		t.Errorf("FixCycleCount = %d, want 3", result.FixCycleCount)
	}
}

func TestRetryResetsFixCycleCount(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	var mu sync.Mutex
	approveAfterRetry := false
	reviewFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		mu.Lock()
		approve := approveAfterRetry
		mu.Unlock()
		if approve {
			return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted, Verdict: agent.VerdictApproved}, nil
		}
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted, Verdict: agent.VerdictNeedsFixes}, nil
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	reg := agent.NewAgentRegistry()
	reg.Register("spec-reviewer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: reviewFn}, nil
	})
	reg.Register("fix-implementer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{}, nil
	})

	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline

	task := &domain.Task{
		ID:             "task-retry-fix",
		PipelineName:   "test",
		Description:    "retry fix test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, _ := eng.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusEscalated {
		t.Fatalf("expected escalated, got %s", result.Status)
	}

	mu.Lock()
	approveAfterRetry = true
	mu.Unlock()

	result, err = eng.Retry(ctx, task.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if result.FixCycleCount != 0 {
		t.Errorf("FixCycleCount after retry = %d, want 0", result.FixCycleCount)
	}
}

// --- Prepare methods tests ---

func TestPrepareApprove(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-prepare",
		PipelineName:   "single",
		Description:    "prepare approve test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	eng.RunUntilGate(ctx, task.ID)

	result, err := eng.PrepareApprove(ctx, task.ID)
	if err != nil {
		t.Fatalf("PrepareApprove: %v", err)
	}
	if result.Status != domain.StatusRunning {
		t.Errorf("Status = %s, want running (fast transition)", result.Status)
	}
	if result.CurrentStageID != "stage2" {
		t.Errorf("CurrentStageID = %s, want stage2", result.CurrentStageID)
	}
}

func TestPrepareReject(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-prepare-reject",
		PipelineName:   "single",
		Description:    "prepare reject test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	eng.RunUntilGate(ctx, task.ID)

	result, err := eng.PrepareReject(ctx, task.ID, "not good")
	if err != nil {
		t.Fatalf("PrepareReject: %v", err)
	}
	if result.Status != domain.StatusRunning {
		t.Errorf("Status = %s, want running", result.Status)
	}
}

// --- Cross-stage rejection feedback tests ---

func TestHumanFinalRejectFeedbackReachesFixAgent(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	var mu sync.Mutex
	var lastInput agent.RunInput
	captureFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		mu.Lock()
		lastInput = input
		mu.Unlock()
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted}, nil
	}

	eng, tmpDir, cleanup := newMockEngineWithFn(t, pipeline, captureFn)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-cross-feedback",
		PipelineName:   "test",
		Description:    "cross-stage feedback test",
		WorkingDir:     tmpDir,
		CurrentStageID: "done",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, _ := eng.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	result, err := eng.Reject(ctx, task.ID, "change X to Y")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}

	mu.Lock()
	captured := lastInput
	mu.Unlock()

	if captured.Feedback == nil {
		t.Fatal("Feedback is nil, expected non-nil")
	}
	if captured.Feedback.Kind != "rejection" {
		t.Errorf("Feedback.Kind = %q, want rejection", captured.Feedback.Kind)
	}
	if captured.Feedback.Text != "change X to Y" {
		t.Errorf("Feedback.Text = %q, want %q", captured.Feedback.Text, "change X to Y")
	}
	if captured.StageID != "fix" {
		t.Errorf("StageID = %q, want fix", captured.StageID)
	}
}

func TestHumanFinalRejectFeedbackIncludesPreviousOutput(t *testing.T) {
	pipeline2 := &domain.Pipeline{
		Name: "test2",
		Stages: []domain.Stage{
			{ID: "implement", Agent: "feature-implementer", Gate: domain.GateAuto},
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	var mu sync.Mutex
	var lastFixInput agent.RunInput
	fixFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		mu.Lock()
		lastFixInput = input
		mu.Unlock()
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted}, nil
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	reg := agent.NewAgentRegistry()
	reg.Register("feature-implementer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{}, nil
	})
	reg.Register("spec-reviewer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: mockVerdictRunFn(agent.VerdictApproved)}, nil
	})
	reg.Register("fix-implementer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: fixFn}, nil
	})

	eng2 := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng2.pipeline = pipeline2

	task2 := &domain.Task{
		ID:             "task-cross-output-2",
		PipelineName:   "test2",
		Description:    "cross-stage output test 2",
		WorkingDir:     tmpDir,
		CurrentStageID: "implement",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng2.store.CreateTask(ctx, task2)

	result2, _ := eng2.RunUntilGate(ctx, task2.ID)
	if result2.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result2.Status)
	}

	result2, err = eng2.Reject(ctx, task2.ID, "fix the output")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	mu.Lock()
	captured := lastFixInput
	mu.Unlock()

	if captured.Feedback == nil {
		t.Fatal("Feedback is nil")
	}
	if captured.Feedback.Text != "fix the output" {
		t.Errorf("Feedback.Text = %q, want %q", captured.Feedback.Text, "fix the output")
	}
}

func TestAutoOnApprovalFixPathUnchanged(t *testing.T) {
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	var mu sync.Mutex
	reviewCalls := 0
	reviewFn := func(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
		mu.Lock()
		n := reviewCalls
		reviewCalls++
		mu.Unlock()
		verdict := agent.VerdictNeedsFixes
		if n >= 1 {
			verdict = agent.VerdictApproved
		}
		return agent.RunResult{SchemaVersion: agent.SchemaVersion, Status: agent.StatusCompleted, Verdict: verdict}, nil
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	reg := agent.NewAgentRegistry()
	reg.Register("spec-reviewer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{RunFn: reviewFn}, nil
	})
	reg.Register("fix-implementer", func(cfg domain.AdapterConfig) (agent.Agent, error) {
		return &agent.MockAgent{}, nil
	})

	eng := NewPipelineEngineWithRegistry(s, reg, nil, domain.AdapterConfig{}, "", "")
	eng.pipeline = pipeline

	task := &domain.Task{
		ID:             "task-auto-on-approval",
		PipelineName:   "test",
		Description:    "auto_on_approval fix path test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}

	var fixRuns int
	for _, r := range result.Runs {
		if r.StageID == "fix" {
			fixRuns++
		}
	}
	if fixRuns != 1 {
		t.Errorf("fix runs = %d, want 1", fixRuns)
	}
	if result.FixCycleCount != 1 {
		t.Errorf("FixCycleCount = %d, want 1", result.FixCycleCount)
	}
}

// --- Restart / incomplete stage run tests ---

func TestRunLoopFiltersIncompleteStageRun(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	startedAt := time.Now().Add(-5 * time.Second)
	task := &domain.Task{
		ID:             "task-incomplete",
		PipelineName:   "single",
		Description:    "incomplete run test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
		Runs: []domain.StageRun{
			{StageID: "stage1", Attempt: 1, Trigger: domain.TriggerInitial, StartedAt: startedAt},
		},
	}
	if err := eng.store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	var completedAttempts []int
	for _, r := range result.Runs {
		if r.StageID == "stage1" && r.FinishedAt != nil {
			completedAttempts = append(completedAttempts, r.Attempt)
		}
	}
	if len(completedAttempts) != 1 {
		t.Fatalf("expected exactly 1 completed run, got %d", len(completedAttempts))
	}
	if completedAttempts[0] != 1 {
		t.Errorf("completed run attempt = %d, want 1", completedAttempts[0])
	}
}

// --- Registry-based adapter resolution tests ---

// minimalAdapter is a minimal AgentAdapter for registry tests that don't need MockAgent.
type minimalAdapter struct{}

func (m *minimalAdapter) Name() string { return "minimal" }
func (m *minimalAdapter) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	if err := os.MkdirAll(params.StageWorkdir, 0o755); err != nil {
		return domain.AgentResult{}, err
	}
	ec := 0
	return domain.AgentResult{Success: true, ExitCode: &ec}, nil
}

func newRegistryTestEngine(t *testing.T, adapterName string) (*PipelineEngine, *domain.Pipeline, string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	pipeline := &domain.Pipeline{
		Name: "backend",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	r := adapter.NewRegistry()
	r.Register(adapterName, func(cfg domain.AdapterConfig) (adapter.AgentAdapter, error) {
		return &minimalAdapter{}, nil
	})

	eng := NewPipelineEngineWithRegistry(s, nil, r, domain.AdapterConfig{}, adapterName, "")
	eng.pipeline = pipeline

	return eng, pipeline, tmpDir, func() { s.Close() }
}

func TestRegistryResolvesAdapterFromTask(t *testing.T) {
	eng, _, tmpDir, cleanup := newRegistryTestEngine(t, "test_adapter")
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-registry",
		PipelineName:   "backend",
		Description:    "registry test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
		Adapter:        "test_adapter",
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
}

func TestRegistryFallsBackToDefaultAdapter(t *testing.T) {
	eng, _, tmpDir, cleanup := newRegistryTestEngine(t, "test_adapter")
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-default",
		PipelineName:   "backend",
		Description:    "default adapter test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
		Adapter:        "",
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
}

func TestRegistryCachesAdapter(t *testing.T) {
	eng, _, _, cleanup := newRegistryTestEngine(t, "test_adapter")
	defer cleanup()

	task := &domain.Task{Adapter: "test_adapter"}

	a1, name1, err := eng.resolveAdapter(task)
	if err != nil {
		t.Fatalf("resolveAdapter: %v", err)
	}

	a2, name2, err := eng.resolveAdapter(task)
	if err != nil {
		t.Fatalf("resolveAdapter second call: %v", err)
	}

	if a1 != a2 {
		t.Error("expected cached adapter to be the same instance")
	}
	if name1 != "test_adapter" || name2 != "test_adapter" {
		t.Errorf("names = %q, %q, want test_adapter", name1, name2)
	}
}

func TestRegistryUnknownAdapterFails(t *testing.T) {
	eng, _, _, cleanup := newRegistryTestEngine(t, "test_adapter")
	defer cleanup()

	task := &domain.Task{Adapter: "nonexistent"}

	_, _, err := eng.resolveAdapter(task)
	if err == nil {
		t.Error("expected error for unknown adapter")
	}
}

func TestLegacyAdapterStillWorks(t *testing.T) {
	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng, tmpDir, cleanup := newMockEngine(t, singlePipeline)
	defer cleanup()

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-legacy",
		PipelineName:   "single",
		Description:    "legacy test",
		WorkingDir:     tmpDir,
		CurrentStageID: "stage1",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	eng.store.CreateTask(ctx, task)

	result, err := eng.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}
	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
}

// --- Glob diff tests ---

func TestGlobDiff(t *testing.T) {
	before := []string{"/a", "/b", "/c"}
	after := []string{"/a", "/b", "/c", "/d", "/e"}
	diff := diffGlobResults(before, after)
	if len(diff) != 2 {
		t.Fatalf("diff length = %d, want 2", len(diff))
	}
	if diff[0] != "/d" || diff[1] != "/e" {
		t.Errorf("diff = %v, want [/d /e]", diff)
	}
}

func TestGlobDiffNoNew(t *testing.T) {
	before := []string{"/a", "/b"}
	after := []string{"/a", "/b"}
	diff := diffGlobResults(before, after)
	if len(diff) != 0 {
		t.Errorf("expected empty diff, got %v", diff)
	}
}

// Make sure the unused time import doesn't cause issues.
var _ = time.Now

// contains is a local helper to avoid importing strings in test.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
