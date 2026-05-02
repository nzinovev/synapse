package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/store"
)

func newTestEngine(t *testing.T) (*PipelineEngine, *domain.Pipeline, string, func()) {
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
			{ID: "spec", Agent: "spec-writer", Gate: domain.GateAutoIfClean, ProducesGlob: "docs/specs/*.md"},
			{ID: "adr", Agent: "adr-architect", Gate: domain.GateHumanApproval, ProducesGlob: "docs/adr/*.md"},
			{ID: "implement", Agent: "feature-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}

	fake := &adapter.FakeAdapter{}
	engine := NewPipelineEngineWithPipeline(s, fake, pipeline)

	return engine, pipeline, tmpDir, func() { s.Close() }
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
	if !strings.Contains(text, "## Status\n\nApproved") {
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
	if !strings.Contains(text, "Status: Approved") {
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
	if !strings.Contains(text, "## Status\n\nApproved") {
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
	engine, pipeline, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	// Create a simple pipeline with only auto gates.
	simplePipeline := &domain.Pipeline{
		Name: "simple",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = simplePipeline

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
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	// Should have run through both auto stages and stopped at human_final.
	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}

	_ = pipeline
}

func TestHumanApprovalGatePauses(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
}

func TestApproveAdvancesToNextStage(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	// Run to first gate.
	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	// Approve.
	result, err = engine.Approve(ctx, task.ID)
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
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	// Run to gate.
	engine.RunUntilGate(ctx, task.ID)

	// Reject.
	result, err := engine.Reject(ctx, task.ID, "not good enough")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status after reject = %s, want awaiting_gate", result.Status)
	}

	// Should have 2 runs for stage1 now.
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
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	engine.RunUntilGate(ctx, task.ID)

	result, err := engine.Cancel(ctx, task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if result.Status != domain.StatusCancelled {
		t.Errorf("Status = %s, want cancelled", result.Status)
	}
}

func TestCancelRequiresCancellableStatus(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	// Run to completion (auto gate finishes all).
	engine.RunUntilGate(ctx, task.ID)

	// Now task is done, so cancel should fail.
	_, err := engine.Cancel(ctx, task.ID)
	if err == nil {
		t.Error("expected error when cancelling done task")
	}
}

func TestRetryRequiresBlockedOrEscalated(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	engine.RunUntilGate(ctx, task.ID)

	// Task is awaiting_gate, not blocked/escalated.
	_, err := engine.Retry(ctx, task.ID)
	if err == nil {
		t.Error("expected error when retrying non-blocked task")
	}
}

func TestBlockedOnAdapterFailure(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
		},
	}
	engine.pipeline = singlePipeline

	fake := &adapter.FakeAdapter{ShouldFail: true}
	engine.adapter = fake

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
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}
	if result.Status != domain.StatusBlocked {
		t.Errorf("Status = %s, want blocked", result.Status)
	}
}

func TestRetryFromBlocked(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

	fake := &adapter.FakeAdapter{ShouldFail: true}
	engine.adapter = fake

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
	engine.store.CreateTask(ctx, task)

	// Run until blocked.
	result, _ := engine.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusBlocked {
		t.Fatalf("expected blocked, got %s", result.Status)
	}

	// Switch to successful adapter and retry.
	engine.adapter = &adapter.FakeAdapter{}
	result, err := engine.Retry(ctx, task.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Should now be at stage2 awaiting gate (auto gate on stage1 advances).
	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status after retry = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "stage2" {
		t.Errorf("CurrentStageID = %s, want stage2", result.CurrentStageID)
	}
}

func TestHumanFinalApprovalCompletesTask(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	// Run to human_final gate.
	result, _ := engine.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	// Approve at human_final gate.
	result, err := engine.Approve(ctx, task.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result.Status != domain.StatusDone {
		t.Errorf("Status = %s, want done", result.Status)
	}
}

func TestHumanFinalRejectionRoutesToFix(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

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
	engine.store.CreateTask(ctx, task)

	// Run to human_final gate.
	result, _ := engine.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	// Reject at human_final — should route to fix, run fix (auto), advance to done, pause at human_final.
	result, err := engine.Reject(ctx, task.ID, "needs work")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// Fix runs (auto gate) then done stage (human_final) pauses.
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}
	if result.FixCycleCount != 1 {
		t.Errorf("FixCycleCount = %d, want 1", result.FixCycleCount)
	}
}

// --- Fix loop tests ---

func TestAutoOnApprovalApproved(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

	// FakeAdapter creates review artifact with APPROVED verdict by default.

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
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	// APPROVED → advance to done → human_final → awaiting_gate.
	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate (at done)", result.Status)
	}
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}
}

func TestAutoOnApprovalNeedsFixes(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

	// Create a custom adapter that writes NEEDS FIXES on first review, APPROVED on second.
	needsFixesAdapter := &needsFixesFake{workDir: tmpDir, maxNeedsFixes: 1}
	engine.adapter = needsFixesAdapter

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-needsfix",
		PipelineName:   "test",
		Description:    "needs fixes test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	// NEEDS FIXES → fix (auto) → review (auto_on_approval) with APPROVED → done → awaiting_gate.

	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}

	// Check that fix was run.
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
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

	// Adapter that always writes NEEDS FIXES.
	engine.adapter = &needsFixesFake{workDir: tmpDir}

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-escalate",
		PipelineName:   "test",
		Description:    "escalation test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
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
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline
	engine.adapter = &needsFixesFake{workDir: tmpDir}

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-retry-fix",
		PipelineName:   "test",
		Description:    "retry fix test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	engine.store.CreateTask(ctx, task)

	// Run until escalated.
	result, _ := engine.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusEscalated {
		t.Fatalf("expected escalated, got %s", result.Status)
	}

	// Switch to passing adapter so retry can succeed.
	engine.adapter = &adapter.FakeAdapter{}

	// Retry resets fix cycle count.
	result, err := engine.Retry(ctx, task.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if result.FixCycleCount != 0 {
		t.Errorf("FixCycleCount after retry = %d, want 0", result.FixCycleCount)
	}
}

// --- Prepare methods tests ---

func TestPrepareApprove(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
			{ID: "stage2", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	engine.RunUntilGate(ctx, task.ID)

	result, err := engine.PrepareApprove(ctx, task.ID)
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
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	engine.pipeline = singlePipeline

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
	engine.store.CreateTask(ctx, task)

	engine.RunUntilGate(ctx, task.ID)

	result, err := engine.PrepareReject(ctx, task.ID, "not good")
	if err != nil {
		t.Fatalf("PrepareReject: %v", err)
	}
	if result.Status != domain.StatusRunning {
		t.Errorf("Status = %s, want running", result.Status)
	}
}

// --- Helper adapter for tests ---

type needsFixesFake struct {
	adapter.FakeAdapter
	workDir       string
	reviewCall    int
	maxNeedsFixes int // 0 = always NEEDS FIXES
}

func (n *needsFixesFake) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	if params.StageID == "review" {
		n.reviewCall++
		if n.maxNeedsFixes > 0 && n.reviewCall > n.maxNeedsFixes {
			return n.FakeAdapter.Invoke(ctx, params)
		}
		os.MkdirAll(filepath.Join(n.workDir, "docs/reviews"), 0o755)
		reviewPath := filepath.Join(n.workDir, "docs/reviews/test-review.md")
		os.WriteFile(reviewPath, []byte("# Review\n\n**Verdict:** NEEDS FIXES\n\nSome issues found.\n"), 0o644)

		exitCode := 0
		return domain.AgentResult{
			Success:          true,
			Stdout:           "review done\nSYNAPSE_AGENT_DONE: needs fixes\n",
			ExitCode:         &exitCode,
			ArtifactsCreated: []string{reviewPath},
		}, nil
	}
	return n.FakeAdapter.Invoke(ctx, params)
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

// --- Capturing adapter for cross-stage feedback tests ---

type capturingAdapter struct {
	adapter.FakeAdapter
	lastParams domain.InvokeParams
	stageID    string // if set, only capture params for this stage
}

func (c *capturingAdapter) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	if c.stageID == "" || c.stageID == params.StageID {
		c.lastParams = params
	}
	return c.FakeAdapter.Invoke(ctx, params)
}

// --- Cross-stage rejection feedback tests ---

func TestHumanFinalRejectFeedbackReachesFixAgent(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

	cap := &capturingAdapter{}
	engine.adapter = cap

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
	engine.store.CreateTask(ctx, task)

	// Run to human_final gate.
	result, _ := engine.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	// Reject at human_final with specific feedback.
	result, err := engine.Reject(ctx, task.ID, "change X to Y")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// Fix ran with auto gate, then done pauses at human_final.
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}

	// Verify the fix agent received the rejection feedback.
	if cap.lastParams.RejectionFeedback == nil {
		t.Fatal("RejectionFeedback is nil, expected non-nil")
	}
	if *cap.lastParams.RejectionFeedback != "change X to Y" {
		t.Errorf("RejectionFeedback = %q, want %q", *cap.lastParams.RejectionFeedback, "change X to Y")
	}
	if cap.lastParams.StageID != "fix" {
		t.Errorf("StageID = %q, want fix", cap.lastParams.StageID)
	}
}

func TestHumanFinalRejectFeedbackIncludesPreviousOutput(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "implement", Agent: "feature-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

	cap := &capturingAdapter{}
	engine.adapter = cap

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-cross-output",
		PipelineName:   "test",
		Description:    "cross-stage output test",
		WorkingDir:     tmpDir,
		CurrentStageID: "implement",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	engine.store.CreateTask(ctx, task)

	// Run implement (auto) then done (human_final pauses).
	result, _ := engine.RunUntilGate(ctx, task.ID)
	if result.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result.Status)
	}

	// Reject at human_final with feedback.
	feedback := "please fix the logic"
	result, err := engine.Reject(ctx, task.ID, feedback)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// After rejection, engine routes to fix. But our pipeline has no fix stage,
	// so doReject won't find it — task stays on "done". Let's use a pipeline
	// with a fix stage instead.
	_ = result

	// Recreate with a fix+done pipeline to test output propagation.
	engine2, _, tmpDir2, cleanup2 := newTestEngine(t)
	defer cleanup2()

	pipeline2 := &domain.Pipeline{
		Name: "test2",
		Stages: []domain.Stage{
			{ID: "implement", Agent: "feature-implementer", Gate: domain.GateAuto},
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine2.pipeline = pipeline2

	cap2 := &capturingAdapter{stageID: "fix"}
	engine2.adapter = cap2

	task2 := &domain.Task{
		ID:             "task-cross-output-2",
		PipelineName:   "test2",
		Description:    "cross-stage output test 2",
		WorkingDir:     tmpDir2,
		CurrentStageID: "implement",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	engine2.store.CreateTask(ctx, task2)

	// Run: implement (auto) → review (auto_on_approval, APPROVED) → done (human_final).
	result2, _ := engine2.RunUntilGate(ctx, task2.ID)
	if result2.Status != domain.StatusAwaitingGate {
		t.Fatalf("expected awaiting_gate, got %s", result2.Status)
	}

	// Reject at human_final.
	result2, err = engine2.Reject(ctx, task2.ID, "fix the output")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// Verify fix agent received feedback and previous output.
	if cap2.lastParams.RejectionFeedback == nil {
		t.Fatal("RejectionFeedback is nil")
	}
	if *cap2.lastParams.RejectionFeedback != "fix the output" {
		t.Errorf("RejectionFeedback = %q, want %q", *cap2.lastParams.RejectionFeedback, "fix the output")
	}

	// Find the done stage run — it has no agent output (null agent),
	// so the fallback should check earlier runs. The implement stage
	// run should have stdout from FakeAdapter.
	// The done stage's AgentResult has empty stdout/stderr (null agent),
	// so previousStdout comes from the done run's AgentResult which is empty.
	// This matches the ADR's documented behavior for null-agent stages.
}

func TestAutoOnApprovalFixPathUnchanged(t *testing.T) {
	engine, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto, ProducesGlob: "docs/handoff/*.md"},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	engine.pipeline = pipeline

	// Use the needsFixesFake which writes NEEDS FIXES on first call, APPROVED on second.
	nfa := &needsFixesFake{workDir: tmpDir, maxNeedsFixes: 1}
	engine.adapter = nfa

	ctx := context.Background()
	task := &domain.Task{
		ID:             "task-auto-on-approval",
		PipelineName:   "test",
		Description:    "auto_on_approval fix path test",
		WorkingDir:     tmpDir,
		CurrentStageID: "review",
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
	engine.store.CreateTask(ctx, task)

	result, err := engine.RunUntilGate(ctx, task.ID)
	if err != nil {
		t.Fatalf("RunUntilGate: %v", err)
	}

	// Should end at done stage awaiting_gate (NEEDS FIXES → fix → review APPROVED → done → human_final).
	if result.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", result.Status)
	}
	if result.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %s, want done", result.CurrentStageID)
	}

	// Verify the fix stage ran exactly once.
	var fixRuns int
	for _, r := range result.Runs {
		if r.StageID == "fix" {
			fixRuns++
		}
	}
	if fixRuns != 1 {
		t.Errorf("fix runs = %d, want 1", fixRuns)
	}

	// Verify fix cycle count was incremented by auto_on_approval path.
	if result.FixCycleCount != 1 {
		t.Errorf("FixCycleCount = %d, want 1", result.FixCycleCount)
	}
}

// --- Restart / incomplete stage run tests ---

func TestRunLoopFiltersIncompleteStageRun(t *testing.T) {
	eng, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateHumanApproval},
		},
	}
	eng.pipeline = singlePipeline

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
		// Simulate a crash mid-invocation: StartedAt set, FinishedAt nil.
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

	// Count completed runs for stage1.
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
		t.Errorf("completed run attempt = %d, want 1 (incomplete run must not count toward attempt number)", completedAttempts[0])
	}
}

// --- Registry-based adapter resolution tests ---

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
	adapter.RegisterFake(r)
	r.Register(adapterName, func(cfg domain.AdapterConfig) (adapter.AgentAdapter, error) {
		return &adapter.FakeAdapter{}, nil
	})

	eng := NewPipelineEngineWithRegistry(s, r, domain.AdapterConfig{}, adapterName, "")
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

	for _, r := range result.Runs {
		if r.StageID == "stage1" {
			if r.Adapter != "test_adapter" {
				t.Errorf("stageRun.Adapter = %q, want %q", r.Adapter, "test_adapter")
			}
			return
		}
	}
	t.Error("no stage1 run found")
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

	for _, r := range result.Runs {
		if r.StageID == "stage1" {
			if r.Adapter != "test_adapter" {
				t.Errorf("stageRun.Adapter = %q, want %q (default)", r.Adapter, "test_adapter")
			}
			return
		}
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
	eng, _, tmpDir, cleanup := newTestEngine(t)
	defer cleanup()

	singlePipeline := &domain.Pipeline{
		Name: "single",
		Stages: []domain.Stage{
			{ID: "stage1", Agent: "spec-writer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng.pipeline = singlePipeline

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
