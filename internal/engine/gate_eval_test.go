package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/store"
)

func newGateEvalEngine(t *testing.T) (*PipelineEngine, *domain.Pipeline, string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval, ProducesGlob: "docs/reviews/*.md"},
			{ID: "fix", Agent: "fix-implementer", Gate: domain.GateAuto},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng := &PipelineEngine{store: s, pipeline: pipeline}
	return eng, pipeline, tmpDir, func() { s.Close() }
}

func makeTask(id, stageID, workDir string) *domain.Task {
	return &domain.Task{
		ID:             id,
		PipelineName:   "test",
		Description:    "test task",
		WorkingDir:     workDir,
		CurrentStageID: stageID,
		Status:         domain.StatusRunning,
		Artifacts:      make(map[string][]string),
	}
}

func TestEvaluateGate_AutoOnApproval_StructuredVerdict(t *testing.T) {
	eng, _, tmpDir, cleanup := newGateEvalEngine(t)
	defer cleanup()

	ctx := context.Background()
	task := makeTask("task-structured-verdict", "review", tmpDir)
	eng.store.CreateTask(ctx, task)

	stage := &domain.Stage{ID: "review", Gate: domain.GateAutoOnApproval}
	result := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
		Verdict:       agent.VerdictApproved,
	}

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng.pipeline = pipeline

	terminal, err := eng.evaluateGate(ctx, task, pipeline, stage, result, nil)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	// APPROVED without handoff → routes to done, non-terminal
	if terminal {
		t.Error("expected non-terminal (advance to done stage)")
	}
	if task.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %q, want %q", task.CurrentStageID, "done")
	}
}

func TestEvaluateGate_AutoOnApproval_FallbackMarkdown(t *testing.T) {
	eng, _, tmpDir, cleanup := newGateEvalEngine(t)
	defer cleanup()

	ctx := context.Background()
	task := makeTask("task-fallback-verdict", "review", tmpDir)
	eng.store.CreateTask(ctx, task)

	// Write APPROVED review artifact for markdown fallback.
	reviewDir := filepath.Join(tmpDir, "docs", "reviews")
	os.MkdirAll(reviewDir, 0o755)
	reviewPath := filepath.Join(reviewDir, "review.md")
	os.WriteFile(reviewPath, []byte("# Review\n\n**Verdict:** APPROVED\n"), 0o644)
	task.Artifacts["review"] = []string{reviewPath}

	stage := &domain.Stage{ID: "review", Gate: domain.GateAutoOnApproval}
	result := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
		// Verdict is empty — should fall back to markdown
	}

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "review", Agent: "spec-reviewer", Gate: domain.GateAutoOnApproval},
			{ID: "done", Agent: "", Gate: domain.GateHumanFinal},
		},
	}
	eng.pipeline = pipeline

	terminal, err := eng.evaluateGate(ctx, task, pipeline, stage, result, nil)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if terminal {
		t.Error("expected non-terminal (advance to done)")
	}
	if task.CurrentStageID != "done" {
		t.Errorf("CurrentStageID = %q, want done", task.CurrentStageID)
	}
}

func TestEvaluateGate_AutoIfClean_StructuredClean(t *testing.T) {
	eng, _, tmpDir, cleanup := newGateEvalEngine(t)
	defer cleanup()

	ctx := context.Background()
	task := makeTask("task-structured-clean", "spec", tmpDir)
	eng.store.CreateTask(ctx, task)

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "spec", Agent: "spec-writer", Gate: domain.GateAutoIfClean},
			{ID: "adr", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	eng.pipeline = pipeline

	stage := &domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean}
	result := agent.RunResult{
		SchemaVersion:  agent.SchemaVersion,
		Status:         agent.StatusCompleted,
		OpenQuestions:  nil,
		Metadata:       map[string]string{"result_json_present": "true"},
	}

	terminal, err := eng.evaluateGate(ctx, task, pipeline, stage, result, nil)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if terminal {
		t.Error("expected non-terminal (auto advance)")
	}
	if task.CurrentStageID != "adr" {
		t.Errorf("CurrentStageID = %q, want adr", task.CurrentStageID)
	}
}

func TestEvaluateGate_AutoIfClean_StructuredDirty(t *testing.T) {
	eng, _, tmpDir, cleanup := newGateEvalEngine(t)
	defer cleanup()

	ctx := context.Background()
	task := makeTask("task-structured-dirty", "spec", tmpDir)
	eng.store.CreateTask(ctx, task)

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "spec", Agent: "spec-writer", Gate: domain.GateAutoIfClean},
			{ID: "adr", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	eng.pipeline = pipeline

	stage := &domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean}
	result := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
		OpenQuestions: []agent.Question{{ID: "q1", Text: "Unanswered question"}},
		Metadata:      map[string]string{"result_json_present": "true"},
	}

	terminal, err := eng.evaluateGate(ctx, task, pipeline, stage, result, nil)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if !terminal {
		t.Error("expected terminal (awaiting gate due to open questions)")
	}
	if task.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", task.Status)
	}
}

func TestEvaluateGate_AutoIfClean_FallbackClean(t *testing.T) {
	eng, _, tmpDir, cleanup := newGateEvalEngine(t)
	defer cleanup()

	ctx := context.Background()
	task := makeTask("task-fallback-clean", "spec", tmpDir)
	eng.store.CreateTask(ctx, task)

	// Create a clean spec artifact.
	specDir := filepath.Join(tmpDir, "docs", "specs")
	os.MkdirAll(specDir, 0o755)
	specPath := filepath.Join(specDir, "spec.md")
	os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\nNone at this time.\n"), 0o644)

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "spec", Agent: "spec-writer", Gate: domain.GateAutoIfClean},
			{ID: "adr", Agent: "adr-architect", Gate: domain.GateHumanApproval},
		},
	}
	eng.pipeline = pipeline

	stage := &domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean}
	result := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
		// No result_json_present in Metadata → fallback to markdown
	}

	terminal, err := eng.evaluateGate(ctx, task, pipeline, stage, result, []string{specPath})
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if terminal {
		t.Error("expected non-terminal (auto advance on clean markdown)")
	}
}

func TestEvaluateGate_AutoIfClean_FallbackDirty(t *testing.T) {
	eng, _, tmpDir, cleanup := newGateEvalEngine(t)
	defer cleanup()

	ctx := context.Background()
	task := makeTask("task-fallback-dirty", "spec", tmpDir)
	eng.store.CreateTask(ctx, task)

	// Create a spec with open questions.
	specDir := filepath.Join(tmpDir, "docs", "specs")
	os.MkdirAll(specDir, 0o755)
	specPath := filepath.Join(specDir, "spec.md")
	os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\n- TBD\n"), 0o644)

	pipeline := &domain.Pipeline{
		Name: "test",
		Stages: []domain.Stage{
			{ID: "spec", Agent: "spec-writer", Gate: domain.GateAutoIfClean},
		},
	}
	eng.pipeline = pipeline

	stage := &domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean}
	result := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
	}

	terminal, err := eng.evaluateGate(ctx, task, pipeline, stage, result, []string{specPath})
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if !terminal {
		t.Error("expected terminal (awaiting gate)")
	}
	if task.Status != domain.StatusAwaitingGate {
		t.Errorf("Status = %s, want awaiting_gate", task.Status)
	}
}
