package agent

import (
	"context"

	"github.com/nzinovev/synapse/internal/domain"
)

const SchemaVersion = "synapse.result.v1"

type StageStatus string

const (
	StatusCompleted  StageStatus = "completed"
	StatusNeedsInput StageStatus = "needs_input"
	StatusFailed     StageStatus = "failed"
)

type Verdict string

const (
	VerdictApproved   Verdict = "APPROVED"
	VerdictNeedsFixes Verdict = "NEEDS FIXES"
	VerdictBlocked    Verdict = "BLOCKED"
)

type ArtifactRef struct {
	Path        string `json:"path"`
	StageID     string `json:"stage_id"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Answer string `json:"answer,omitempty"`
}

type ModelUsage struct {
	Model            string `json:"model"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
}

type FeedbackDetail struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type PriorOutput struct {
	StageID   string        `json:"stage_id"`
	Artifacts []ArtifactRef `json:"artifacts"`
	Summary   string        `json:"summary,omitempty"`
}

type RunInput struct {
	SchemaVersion  string            `json:"schema_version"`
	TaskID         string            `json:"task_id"`
	TaskNumber     string            `json:"task_number"`
	Goal           string            `json:"goal"`
	WorkspacePath  string            `json:"workspace_path"`
	StageWorkdir   string            `json:"stage_workdir,omitempty"`
	StageID        string            `json:"stage_id"`
	PipelineName   string            `json:"pipeline_name"`
	Gate           domain.Gate       `json:"gate"`
	PRIndex        int               `json:"pr_index"`
	FixCycleCount  int               `json:"fix_cycle_count"`
	PriorOutputs   []PriorOutput     `json:"prior_outputs"`
	Feedback       *FeedbackDetail   `json:"feedback,omitempty"`
	PreviousStdout *string           `json:"previous_stdout,omitempty"`
	PreviousStderr *string           `json:"previous_stderr,omitempty"`
	Model          string            `json:"model,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

func NewRunInput(taskID, taskNumber, goal, workspacePath, stageID, pipelineName string, gate domain.Gate) RunInput {
	return RunInput{
		SchemaVersion: SchemaVersion,
		TaskID:        taskID,
		TaskNumber:    taskNumber,
		Goal:          goal,
		WorkspacePath: workspacePath,
		StageID:       stageID,
		PipelineName:  pipelineName,
		Gate:          gate,
	}
}

type RunResult struct {
	SchemaVersion   string            `json:"schema_version"`
	Status          StageStatus       `json:"status"`
	Summary         string            `json:"summary,omitempty"`
	Artifacts       []ArtifactRef     `json:"artifacts,omitempty"`
	OpenQuestions   []Question        `json:"open_questions,omitempty"`
	Verdict         Verdict           `json:"verdict,omitempty"`
	Stdout          string            `json:"stdout,omitempty"`
	Stderr          string            `json:"stderr,omitempty"`
	DurationSeconds float64           `json:"duration_seconds"`
	Usage           *ModelUsage       `json:"usage,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type Agent interface {
	Name() string
	Run(ctx context.Context, input RunInput) (RunResult, error)
}
