package engine

import (
	"os"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

type GateOutcome string

const (
	GateAdvance GateOutcome = "advance"
	GatePause   GateOutcome = "pause"
	GateRoute   GateOutcome = "route"
)

type GateResult struct {
	Outcome    GateOutcome
	RouteTo    string
	VerdictRaw string
}

func evaluateGate(
	gate domain.Gate,
	stageID string,
	result *agent.RunResult,
	stdout string,
	artifactPaths []string,
) GateResult {
	switch gate {
	case domain.GateAuto:
		return GateResult{Outcome: GateAdvance}

	case domain.GateAutoIfClean:
		if result != nil && result.FromFile {
			if len(result.OpenQuestions) == 0 {
				return GateResult{Outcome: GateAdvance}
			}
			return GateResult{Outcome: GatePause}
		}
		if ArtifactsAllowAutoIfClean(artifactPaths) {
			return GateResult{Outcome: GateAdvance}
		}
		return GateResult{Outcome: GatePause}

	case domain.GateAutoOnApproval:
		verdict := ""
		if result != nil && result.Verdict != "" {
			verdict = string(result.Verdict)
		} else {
			verdict = ParseVerdict(stdout)
		}
		if verdict == "" {
			verdict = parseVerdictFromArtifacts(artifactPaths)
		}
		return evaluateAutoOnApprovalGate(verdict)

	default:
		return GateResult{Outcome: GatePause}
	}
}

func parseVerdictFromArtifacts(artifactPaths []string) string {
	if len(artifactPaths) == 0 {
		return ""
	}
	reviewPath := artifactPaths[len(artifactPaths)-1]
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		return ""
	}
	return ParseVerdict(string(data))
}

func evaluateAutoOnApprovalGate(verdict string) GateResult {
	switch verdict {
	case "APPROVED":
		return GateResult{Outcome: GateRoute, RouteTo: "done", VerdictRaw: verdict}
	case "NEEDS FIXES":
		return GateResult{Outcome: GateRoute, RouteTo: "fix", VerdictRaw: verdict}
	default:
		return GateResult{Outcome: GatePause, VerdictRaw: verdict}
	}
}
