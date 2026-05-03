package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

// --- auto gate ---

func TestEvaluateGateAuto(t *testing.T) {
	gr := evaluateGate(domain.GateAuto, "stage1", nil, "", nil)
	if gr.Outcome != GateAdvance {
		t.Errorf("auto gate: outcome = %s, want advance", gr.Outcome)
	}
}

// --- human_approval gate ---

func TestEvaluateGateHumanApproval(t *testing.T) {
	gr := evaluateGate(domain.GateHumanApproval, "stage1", nil, "", nil)
	if gr.Outcome != GatePause {
		t.Errorf("human_approval gate: outcome = %s, want pause", gr.Outcome)
	}
}

// --- human_final gate ---

func TestEvaluateGateHumanFinal(t *testing.T) {
	gr := evaluateGate(domain.GateHumanFinal, "done", nil, "", nil)
	if gr.Outcome != GatePause {
		t.Errorf("human_final gate: outcome = %s, want pause", gr.Outcome)
	}
}

// --- auto_if_clean: structured path ---

func TestAutoIfCleanStructuredEmptyQuestions(t *testing.T) {
	result := &agent.RunResult{FromFile: true, OpenQuestions: nil}
	gr := evaluateGate(domain.GateAutoIfClean, "spec", result, "", nil)
	if gr.Outcome != GateAdvance {
		t.Errorf("structured empty questions: outcome = %s, want advance", gr.Outcome)
	}
}

func TestAutoIfCleanStructuredWithQuestions(t *testing.T) {
	result := &agent.RunResult{
		FromFile:      true,
		OpenQuestions: []agent.Question{{ID: "q1", Text: "What DB?"}},
	}
	gr := evaluateGate(domain.GateAutoIfClean, "spec", result, "", nil)
	if gr.Outcome != GatePause {
		t.Errorf("structured with questions: outcome = %s, want pause", gr.Outcome)
	}
}

func TestAutoIfCleanNotFromFile(t *testing.T) {
	result := &agent.RunResult{FromFile: false, OpenQuestions: nil}
	gr := evaluateGate(domain.GateAutoIfClean, "spec", result, "", nil)
	// Should fall through to fallback; with no artifact paths, should pause.
	if gr.Outcome != GatePause {
		t.Errorf("not from file, no artifacts: outcome = %s, want pause", gr.Outcome)
	}
}

func TestAutoIfCleanNilResult(t *testing.T) {
	gr := evaluateGate(domain.GateAutoIfClean, "spec", nil, "", nil)
	if gr.Outcome != GatePause {
		t.Errorf("nil result, no artifacts: outcome = %s, want pause", gr.Outcome)
	}
}

// --- auto_if_clean: fallback to markdown ---

func TestAutoIfCleanFallbackClean(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.md")
	os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\nNone at this time.\n"), 0o644)

	gr := evaluateGate(domain.GateAutoIfClean, "spec", nil, "", []string{specPath})
	if gr.Outcome != GateAdvance {
		t.Errorf("fallback clean: outcome = %s, want advance", gr.Outcome)
	}
}

func TestAutoIfCleanFallbackQuestions(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.md")
	os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\n- TBD\n"), 0o644)

	gr := evaluateGate(domain.GateAutoIfClean, "spec", nil, "", []string{specPath})
	if gr.Outcome != GatePause {
		t.Errorf("fallback with questions: outcome = %s, want pause", gr.Outcome)
	}
}

func TestAutoIfCleanFallbackQuestionsMD(t *testing.T) {
	tmpDir := t.TempDir()
	qPath := filepath.Join(tmpDir, "QUESTIONS.md")
	os.WriteFile(qPath, []byte("questions"), 0o644)

	gr := evaluateGate(domain.GateAutoIfClean, "spec", nil, "", []string{qPath})
	if gr.Outcome != GatePause {
		t.Errorf("fallback QUESTIONS.md: outcome = %s, want pause", gr.Outcome)
	}
}

// --- auto_on_approval: structured path ---

func TestAutoOnApprovalStructuredApproved(t *testing.T) {
	result := &agent.RunResult{Verdict: agent.VerdictApproved}
	gr := evaluateGate(domain.GateAutoOnApproval, "review", result, "", nil)
	if gr.Outcome != GateRoute || gr.RouteTo != "done" {
		t.Errorf("structured approved: outcome = %s routeTo = %s, want route/done", gr.Outcome, gr.RouteTo)
	}
	if gr.VerdictRaw != "APPROVED" {
		t.Errorf("verdict = %q, want APPROVED", gr.VerdictRaw)
	}
}

func TestAutoOnApprovalStructuredNeedsFixes(t *testing.T) {
	result := &agent.RunResult{Verdict: agent.VerdictNeedsFixes}
	gr := evaluateGate(domain.GateAutoOnApproval, "review", result, "", nil)
	if gr.Outcome != GateRoute {
		t.Errorf("structured needs fixes: outcome = %s, want route", gr.Outcome)
	}
	if gr.RouteTo != "fix" {
		t.Errorf("routeTo = %q, want fix", gr.RouteTo)
	}
}

func TestAutoOnApprovalStructuredBlocked(t *testing.T) {
	result := &agent.RunResult{Verdict: agent.VerdictBlocked}
	gr := evaluateGate(domain.GateAutoOnApproval, "review", result, "", nil)
	if gr.Outcome != GatePause {
		t.Errorf("structured blocked: outcome = %s, want pause", gr.Outcome)
	}
}

func TestAutoOnApprovalStructuredEmptyVerdict(t *testing.T) {
	result := &agent.RunResult{Verdict: ""}
	gr := evaluateGate(domain.GateAutoOnApproval, "review", result, "no verdict here", nil)
	// Falls back to ParseVerdict on stdout.
	if gr.Outcome != GatePause {
		t.Errorf("structured empty verdict, no verdict in stdout: outcome = %s, want pause", gr.Outcome)
	}
}

// --- auto_on_approval: fallback to stdout ---

func TestAutoOnApprovalFallbackApproved(t *testing.T) {
	gr := evaluateGate(domain.GateAutoOnApproval, "review", nil, "Review text\n**Verdict:** APPROVED\n", nil)
	if gr.Outcome != GateRoute || gr.RouteTo != "done" {
		t.Errorf("fallback approved: outcome = %s routeTo = %s, want route/done", gr.Outcome, gr.RouteTo)
	}
}

func TestAutoOnApprovalFallbackNeedsFixes(t *testing.T) {
	gr := evaluateGate(domain.GateAutoOnApproval, "review", nil, "**Verdict:** NEEDS FIXES\n", nil)
	if gr.Outcome != GateRoute {
		t.Errorf("fallback needs fixes: outcome = %s, want route", gr.Outcome)
	}
	if gr.RouteTo != "fix" {
		t.Errorf("routeTo = %q, want fix", gr.RouteTo)
	}
}

func TestAutoOnApprovalFallbackBlocked(t *testing.T) {
	gr := evaluateGate(domain.GateAutoOnApproval, "review", nil, "**Verdict:** BLOCKED\n", nil)
	if gr.Outcome != GatePause {
		t.Errorf("fallback blocked: outcome = %s, want pause", gr.Outcome)
	}
}

func TestAutoOnApprovalFallbackNoVerdict(t *testing.T) {
	gr := evaluateGate(domain.GateAutoOnApproval, "review", nil, "No verdict here", nil)
	if gr.Outcome != GatePause {
		t.Errorf("fallback no verdict: outcome = %s, want pause", gr.Outcome)
	}
}

// --- evaluateAutoOnApprovalGate helper ---

func TestEvaluateAutoOnApprovalGateApproved(t *testing.T) {
	gr := evaluateAutoOnApprovalGate("APPROVED")
	if gr.Outcome != GateRoute || gr.RouteTo != "done" {
		t.Errorf("approved: outcome = %s routeTo = %s, want route/done", gr.Outcome, gr.RouteTo)
	}
}

func TestEvaluateAutoOnApprovalGateNeedsFixes(t *testing.T) {
	gr := evaluateAutoOnApprovalGate("NEEDS FIXES")
	if gr.Outcome != GateRoute || gr.RouteTo != "fix" {
		t.Errorf("needs fixes: outcome = %s routeTo = %s, want route/fix", gr.Outcome, gr.RouteTo)
	}
}

func TestEvaluateAutoOnApprovalGateBlocked(t *testing.T) {
	gr := evaluateAutoOnApprovalGate("BLOCKED")
	if gr.Outcome != GatePause {
		t.Errorf("blocked: outcome = %s, want pause", gr.Outcome)
	}
}

func TestEvaluateAutoOnApprovalGateEmpty(t *testing.T) {
	gr := evaluateAutoOnApprovalGate("")
	if gr.Outcome != GatePause {
		t.Errorf("empty verdict: outcome = %s, want pause", gr.Outcome)
	}
}
