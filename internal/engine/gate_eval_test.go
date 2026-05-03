package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

func TestEvaluateGateAutoOnApprovalStructured(t *testing.T) {
	outcome, err := evaluateGate(
		domain.Stage{ID: "review", Gate: domain.GateAutoOnApproval},
		agent.RunResult{SchemaVersion: agent.SchemaVersion, FromFile: true, Verdict: agent.VerdictApproved},
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if outcome.Decision != gateApproved {
		t.Fatalf("Decision = %q, want %q", outcome.Decision, gateApproved)
	}
}

func TestEvaluateGateAutoOnApprovalStructuredBlocked(t *testing.T) {
	outcome, err := evaluateGate(
		domain.Stage{ID: "review", Gate: domain.GateAutoOnApproval},
		agent.RunResult{SchemaVersion: agent.SchemaVersion, FromFile: true, Verdict: agent.VerdictBlocked},
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if outcome.Decision != gateAwait {
		t.Fatalf("Decision = %q, want %q", outcome.Decision, gateAwait)
	}
	if outcome.Verdict != agent.VerdictBlocked {
		t.Fatalf("Verdict = %q, want %q", outcome.Verdict, agent.VerdictBlocked)
	}
}

func TestEvaluateGateAutoOnApprovalFallsBackToStdout(t *testing.T) {
	outcome, err := evaluateGate(
		domain.Stage{ID: "review", Gate: domain.GateAutoOnApproval},
		agent.RunResult{SchemaVersion: agent.SchemaVersion, Stdout: "**Verdict:** NEEDS FIXES\n"},
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if outcome.Decision != gateNeedsFixes {
		t.Fatalf("Decision = %q, want %q", outcome.Decision, gateNeedsFixes)
	}
}

func TestEvaluateGateAutoIfCleanStructured(t *testing.T) {
	outcome, err := evaluateGate(
		domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean},
		agent.RunResult{SchemaVersion: agent.SchemaVersion, FromFile: true, OpenQuestions: []agent.Question{}},
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if outcome.Decision != gateProceed {
		t.Fatalf("Decision = %q, want %q", outcome.Decision, gateProceed)
	}

	outcome, err = evaluateGate(
		domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean},
		agent.RunResult{
			SchemaVersion: agent.SchemaVersion,
			FromFile:      true,
			OpenQuestions: []agent.Question{
				{ID: "q1", Text: "Which database?"},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("evaluateGate with open questions: %v", err)
	}
	if outcome.Decision != gateAwaitAutoIfClean {
		t.Fatalf("Decision = %q, want %q", outcome.Decision, gateAwaitAutoIfClean)
	}
}

func TestEvaluateGateAutoIfCleanFallsBackToArtifactsWhenResultJSONAbsent(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# Spec\n\n## Open Questions\n\nNone at this time.\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	result := agent.RunResult{SchemaVersion: agent.SchemaVersion}
	outcome, err := evaluateGate(
		domain.Stage{ID: "spec", Gate: domain.GateAutoIfClean},
		result,
		[]string{specPath},
	)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if outcome.Decision != gateProceed {
		t.Fatalf("Decision = %q, want %q", outcome.Decision, gateProceed)
	}
}

func TestEvaluateGateHumanGatesAwait(t *testing.T) {
	for _, gate := range []domain.Gate{domain.GateHumanApproval, domain.GateHumanFinal} {
		outcome, err := evaluateGate(
			domain.Stage{ID: "review", Gate: gate},
			agent.RunResult{SchemaVersion: agent.SchemaVersion},
			nil,
		)
		if err != nil {
			t.Fatalf("evaluateGate(%s): %v", gate, err)
		}
		if outcome.Decision != gateAwait {
			t.Fatalf("evaluateGate(%s) Decision = %q, want %q", gate, outcome.Decision, gateAwait)
		}
	}
}
