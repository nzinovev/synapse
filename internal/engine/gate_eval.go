package engine

import (
	"context"
	"regexp"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

// evaluateGate decides what happens after a stage completes. It is the single
// decision point for all gate types. The result argument carries structured
// output from the agent (verdict, open questions); artifactPaths is the list
// of new artifact files produced in this run.
func (e *PipelineEngine) evaluateGate(
	ctx context.Context,
	task *domain.Task,
	pipeline *domain.Pipeline,
	stage *domain.Stage,
	result agent.RunResult,
	artifactPaths []string,
) (terminal bool, err error) {
	switch stage.Gate {
	case domain.GateAuto:
		return e.continueAfterAutoGate(ctx, task, pipeline, stage)

	case domain.GateAutoIfClean:
		clean := e.stageIsClean(result, artifactPaths)
		if clean {
			return e.continueAfterAutoGate(ctx, task, pipeline, stage)
		}
		task.Status = domain.StatusAwaitingGate
		e.emitEvent(ctx, task.ID, stage.ID, domain.EventGateAwaiting,
			"Waiting for human approval at stage "+stage.ID+" (auto_if_clean: open questions or QUESTIONS.md)",
			map[string]any{"gate": "auto_if_clean"})
		e.store.SaveTask(ctx, task)
		return true, nil

	case domain.GateAutoOnApproval:
		return e.evaluateAutoOnApproval(ctx, task, pipeline, stage, result)

	default:
		// human_approval or human_final: stop and await gate.
		task.Status = domain.StatusAwaitingGate
		e.emitEvent(ctx, task.ID, stage.ID, domain.EventGateAwaiting,
			"Waiting for "+string(stage.Gate)+" at stage "+stage.ID,
			map[string]any{"gate": string(stage.Gate)})
		e.store.SaveTask(ctx, task)
		return true, nil
	}
}

// stageIsClean returns true if the stage result has no open questions (structured
// path) or if the artifact files contain no open questions (markdown fallback).
func (e *PipelineEngine) stageIsClean(result agent.RunResult, artifactPaths []string) bool {
	if result.Metadata["result_json_present"] == "true" {
		return len(result.OpenQuestions) == 0
	}
	return ArtifactsAllowAutoIfClean(artifactPaths)
}

func (e *PipelineEngine) evaluateAutoOnApproval(
	ctx context.Context,
	task *domain.Task,
	pipeline *domain.Pipeline,
	stage *domain.Stage,
	result agent.RunResult,
) (bool, error) {
	verdict := e.resolveVerdict(result, stage.ID, task)

	switch verdict {
	case "APPROVED":
		handoffRe := regexp.MustCompile(`/handoff/.*-pr\d+\.md$`)
		hasHandoff := false
		for _, p := range task.Artifacts["implement"] {
			if handoffRe.MatchString(p) {
				hasHandoff = true
				break
			}
		}

		if hasHandoff {
			task.PRIndex++
			task.Artifacts["implement"] = nil
			implementStage, err := pipeline.GetStage("implement")
			if err != nil {
				return false, err
			}
			task.CurrentStageID = implementStage.ID
			task.Status = domain.StatusRunning
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageStarted,
				"PR approved — starting implement for next PR",
				map[string]any{"pr_index": task.PRIndex})
			e.store.SaveTask(ctx, task)
			return false, nil
		}

		doneStage, err := pipeline.GetStage("done")
		if err != nil {
			return false, err
		}
		task.CurrentStageID = doneStage.ID
		task.Status = domain.StatusRunning
		e.store.SaveTask(ctx, task)
		return false, nil

	case "NEEDS FIXES":
		if task.FixCycleCount >= MaxFixCycles {
			task.Status = domain.StatusEscalated
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageFailed,
				"Max fix cycles reached — escalated, human intervention required",
				map[string]any{"reason": "max_fix_cycles_exceeded"})
			e.store.SaveTask(ctx, task)
			return true, nil
		}
		task.FixCycleCount++
		fixStage, err := pipeline.GetStage("fix")
		if err != nil {
			return false, err
		}
		task.CurrentStageID = fixStage.ID
		task.Status = domain.StatusRunning
		e.store.SaveTask(ctx, task)
		return false, nil

	default:
		task.Status = domain.StatusAwaitingGate
		e.emitEvent(ctx, task.ID, stage.ID, domain.EventGateAwaiting,
			"Reviewer verdict BLOCKED (or unreadable) — awaiting human",
			map[string]any{"gate": "auto_on_approval", "verdict": verdict})
		e.store.SaveTask(ctx, task)
		return true, nil
	}
}

// resolveVerdict picks the verdict from the structured result first, then falls
// back to parsing the last artifact file as markdown.
func (e *PipelineEngine) resolveVerdict(result agent.RunResult, stageID string, task *domain.Task) string {
	if result.Verdict != "" {
		return string(result.Verdict)
	}
	return e.parseReviewerVerdict(stageID, task)
}

func (e *PipelineEngine) continueAfterAutoGate(ctx context.Context, task *domain.Task, pipeline *domain.Pipeline, stage *domain.Stage) (bool, error) {
	var reviewerStage *domain.Stage
	for i := range pipeline.Stages {
		if pipeline.Stages[i].Gate == domain.GateAutoOnApproval {
			reviewerStage = &pipeline.Stages[i]
			break
		}
	}

	var nextStage *domain.Stage
	if reviewerStage != nil && stage.ID == "fix" {
		nextStage = reviewerStage
	} else {
		var err error
		nextStage, err = pipeline.NextStage(stage.ID)
		if err != nil {
			return false, err
		}
	}

	if nextStage == nil {
		task.Status = domain.StatusDone
		e.emitEvent(ctx, task.ID, stage.ID, domain.EventTaskDone, "All stages complete.", nil)
		e.store.SaveTask(ctx, task)
		return true, nil
	}

	task.CurrentStageID = nextStage.ID
	task.Status = domain.StatusRunning
	e.store.SaveTask(ctx, task)
	return false, nil
}
