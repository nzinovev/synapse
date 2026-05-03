package engine

import (
	"context"
	"os"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

type adapterBackedAgent struct {
	name  string
	inner adapter.AgentAdapter
}

func (a adapterBackedAgent) Name() string {
	if a.name != "" {
		return a.name
	}
	return a.inner.Name()
}

func (a adapterBackedAgent) Run(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
	agentName := input.AgentName
	if agentName == "" {
		agentName = input.StageID
	}

	result, err := a.inner.Invoke(ctx, domain.InvokeParams{
		AgentName:           agentName,
		TaskDescription:     input.Goal,
		WorkingDir:          input.WorkspacePath,
		ContextArtifacts:    contextArtifacts(input.PriorOutputs),
		RejectionFeedback:   rejectionFeedback(input.Feedback),
		OpenQuestionAnswers: openQuestionAnswers(input.Feedback),
		StageWorkdir:        input.StageWorkdir,
		StageID:             input.StageID,
		Gate:                input.Gate,
		PipelineName:        input.PipelineName,
		FixCycleCount:       input.FixCycleCount,
		PRIndex:             input.PRIndex,
		PreviousStdout:      input.PreviousStdout,
		PreviousStderr:      input.PreviousStderr,
		Model:               input.Model,
	})
	if err != nil {
		return agent.RunResult{}, err
	}

	status := agent.StatusFailed
	if result.Success {
		status = agent.StatusCompleted
	}
	runResult := agent.RunResult{
		SchemaVersion:   agent.SchemaVersion,
		Status:          status,
		Artifacts:       artifactRefs(input.StageID, result.ArtifactsCreated),
		Stdout:          result.Stdout,
		Stderr:          result.Stderr,
		DurationSeconds: result.DurationSeconds,
	}
	if runResult.Verdict == "" {
		runResult.Verdict = verdictFromArtifacts(result.ArtifactsCreated)
	}
	return runResult, nil
}

func priorOutput(stageID string, paths []string) agent.PriorOutput {
	artifacts := make([]agent.ArtifactRef, 0, len(paths))
	for _, path := range paths {
		artifacts = append(artifacts, agent.ArtifactRef{Path: path, StageID: stageID})
	}
	return agent.PriorOutput{StageID: stageID, Artifacts: artifacts}
}

func contextArtifacts(outputs []agent.PriorOutput) map[string][]string {
	if len(outputs) == 0 {
		return nil
	}
	artifacts := make(map[string][]string)
	for _, output := range outputs {
		for _, artifact := range output.Artifacts {
			artifacts[output.StageID] = append(artifacts[output.StageID], artifact.Path)
		}
	}
	if len(artifacts) == 0 {
		return nil
	}
	return artifacts
}

func rejectionFeedback(feedback *agent.FeedbackDetail) *string {
	if feedback == nil || feedback.Kind != "rejection" {
		return nil
	}
	return &feedback.Text
}

func openQuestionAnswers(feedback *agent.FeedbackDetail) *string {
	if feedback == nil {
		return nil
	}
	if feedback.Kind != "answer" && feedback.Kind != "answers" {
		return nil
	}
	return &feedback.Text
}

func artifactRefs(stageID string, paths []string) []agent.ArtifactRef {
	if len(paths) == 0 {
		return nil
	}
	artifacts := make([]agent.ArtifactRef, 0, len(paths))
	for _, path := range paths {
		artifacts = append(artifacts, agent.ArtifactRef{Path: path, StageID: stageID})
	}
	return artifacts
}

func verdictFromArtifacts(paths []string) agent.Verdict {
	for i := len(paths) - 1; i >= 0; i-- {
		data, err := os.ReadFile(paths[i])
		if err != nil {
			continue
		}
		if verdict := ParseVerdict(string(data)); verdict != "" {
			return agent.Verdict(verdict)
		}
	}
	return ""
}

func runFeedback(rejection, answers *string) *agent.FeedbackDetail {
	if rejection != nil {
		return &agent.FeedbackDetail{Kind: "rejection", Text: *rejection}
	}
	if answers != nil {
		return &agent.FeedbackDetail{Kind: "answers", Text: *answers}
	}
	return nil
}

func mergeRunResult(memory, file agent.RunResult) agent.RunResult {
	merged := memory
	if file.SchemaVersion != "" {
		merged.SchemaVersion = file.SchemaVersion
	}
	if file.Status != "" {
		merged.Status = file.Status
	}
	if file.Summary != "" {
		merged.Summary = file.Summary
	}
	if file.Artifacts != nil {
		merged.Artifacts = file.Artifacts
	}
	if file.OpenQuestions != nil {
		merged.OpenQuestions = file.OpenQuestions
	}
	if file.Verdict != "" {
		merged.Verdict = file.Verdict
	}
	if file.Stdout != "" {
		merged.Stdout = file.Stdout
	}
	if file.Stderr != "" {
		merged.Stderr = file.Stderr
	}
	if file.DurationSeconds != 0 {
		merged.DurationSeconds = file.DurationSeconds
	}
	if file.Usage != nil {
		merged.Usage = file.Usage
	}
	if file.Metadata != nil {
		merged.Metadata = file.Metadata
	}
	if file.FromFile {
		merged.FromFile = true
	}
	return merged
}

func domainAgentResult(result agent.RunResult) *domain.AgentResult {
	success := result.Status != agent.StatusFailed
	exitCode := 0
	if !success {
		exitCode = 1
	}

	artifacts := make([]string, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		artifacts = append(artifacts, artifact.Path)
	}

	return &domain.AgentResult{
		Success:          success,
		Stdout:           result.Stdout,
		Stderr:           result.Stderr,
		ArtifactsCreated: artifacts,
		DurationSeconds:  result.DurationSeconds,
		ExitCode:         &exitCode,
	}
}
