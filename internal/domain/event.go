package domain

import (
	"encoding/json"
	"time"
)

type EventKind string

const (
	EventStageStarted   EventKind = "stage_started"
	EventAgentOutput    EventKind = "agent_output"
	EventStageCompleted EventKind = "stage_completed"
	EventStageFailed    EventKind = "stage_failed"
	EventGateAwaiting   EventKind = "gate_awaiting"
	EventGateApproved   EventKind = "gate_approved"
	EventGateRejected   EventKind = "gate_rejected"
	EventTaskDone       EventKind = "task_done"
	EventTaskCancelled  EventKind = "task_cancelled"
	EventGateAnswered   EventKind = "gate_answered"
)

type AgentEvent struct {
	TaskID    string         `json:"task_id"`
	StageID   string         `json:"stage_id"`
	Timestamp time.Time      `json:"timestamp"`
	Kind      EventKind      `json:"kind"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata"`
}

func (e AgentEvent) MarshalJSON() ([]byte, error) {
	type Alias AgentEvent
	md := e.Metadata
	if md == nil {
		md = map[string]any{}
	}
	return json.Marshal(&struct {
		Alias
		Metadata map[string]any `json:"metadata"`
	}{
		Alias:    Alias(e),
		Metadata: md,
	})
}
