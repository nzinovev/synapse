package engine

import (
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

type gateDecision string

const (
	gateProceed          gateDecision = "proceed"
	gateAwait            gateDecision = "await"
	gateAwaitAutoIfClean gateDecision = "await_auto_if_clean"
	gateApproved         gateDecision = "approved"
	gateNeedsFixes       gateDecision = "needs_fixes"
)

type GateOutcome struct {
	Decision gateDecision
	Verdict  agent.Verdict
}

func evaluateGate(stage domain.Stage, result agent.RunResult, artifactPaths []string) (GateOutcome, error) {
	switch stage.Gate {
	case domain.GateAuto:
		return GateOutcome{Decision: gateProceed}, nil

	case domain.GateAutoIfClean:
		if !result.FromFile {
			if ArtifactsAllowAutoIfClean(artifactPaths) {
				return GateOutcome{Decision: gateProceed}, nil
			}
			return GateOutcome{Decision: gateAwaitAutoIfClean}, nil
		}
		if len(result.OpenQuestions) == 0 {
			return GateOutcome{Decision: gateProceed}, nil
		}
		return GateOutcome{Decision: gateAwaitAutoIfClean}, nil

	case domain.GateAutoOnApproval:
		verdict := result.Verdict
		if verdict == "" {
			verdict = agent.Verdict(ParseVerdict(result.Stdout))
		}

		switch verdict {
		case agent.VerdictApproved:
			return GateOutcome{Decision: gateApproved, Verdict: verdict}, nil
		case agent.VerdictNeedsFixes:
			return GateOutcome{Decision: gateNeedsFixes, Verdict: verdict}, nil
		default:
			return GateOutcome{Decision: gateAwait, Verdict: verdict}, nil
		}

	default:
		return GateOutcome{Decision: gateAwait}, nil
	}
}
