package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TaskStatus string

const (
	StatusPending      TaskStatus = "pending"
	StatusRunning      TaskStatus = "running"
	StatusAwaitingGate TaskStatus = "awaiting_gate"
	StatusDone         TaskStatus = "done"
	StatusBlocked      TaskStatus = "blocked"
	StatusEscalated    TaskStatus = "escalated"
	StatusCancelled    TaskStatus = "cancelled"
)

type RunTrigger string

const (
	TriggerInitial   RunTrigger = "initial"
	TriggerRejection RunTrigger = "rejection"
	TriggerRetry     RunTrigger = "retry"
	TriggerAnswers   RunTrigger = "answers"
)

type AgentResult struct {
	Success          bool     `json:"success"`
	Stdout           string   `json:"stdout"`
	Stderr           string   `json:"stderr"`
	ArtifactsCreated []string `json:"artifacts_created"`
	DurationSeconds  float64  `json:"duration_seconds"`
	ExitCode         *int     `json:"exit_code"`
}

type StageRun struct {
	StageID           string       `json:"stage_id"`
	Attempt           int          `json:"attempt"`
	Trigger           RunTrigger   `json:"trigger"`
	StartedAt         time.Time    `json:"started_at"`
	FinishedAt        *time.Time   `json:"finished_at"`
	AgentResult       *AgentResult `json:"agent_result"`
	RejectionFeedback *string      `json:"rejection_feedback"`
	AnswersFeedback   *string      `json:"answers_feedback"`
	Adapter           string       `json:"adapter"`
}

type Task struct {
	ID             string              `json:"id"`
	PipelineName   string              `json:"pipeline_name"`
	Description    string              `json:"description"`
	WorkingDir     string              `json:"working_dir"`
	CurrentStageID string              `json:"current_stage_id"`
	Status         TaskStatus          `json:"status"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	Runs           []StageRun          `json:"runs"`
	Artifacts      map[string][]string `json:"artifacts"`
	FixCycleCount  int                 `json:"fix_cycle_count"`
	PRIndex        int                 `json:"pr_index"`
	Adapter        string              `json:"adapter"`
}

func GenerateTaskID(taskNumber string, now time.Time, collisionSuffix int) string {
	padded := taskNumber
	for len(padded) < 3 {
		padded = "0" + padded
	}
	ts := now.UTC().Format("20060102-150405")
	id := fmt.Sprintf("task-%s-%s", padded, ts)
	if collisionSuffix > 0 {
		id = fmt.Sprintf("%s-%d", id, collisionSuffix)
	}
	return id
}

func IsDuplicateIDError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint")
}

func (t Task) MarshalJSON() ([]byte, error) {
	type Alias Task
	artifacts := t.Artifacts
	if artifacts == nil {
		artifacts = map[string][]string{}
	}
	return json.Marshal(&struct {
		Alias
		Artifacts map[string][]string `json:"artifacts"`
	}{
		Alias:     Alias(t),
		Artifacts: artifacts,
	})
}
