package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventKindValues(t *testing.T) {
	kinds := map[string]EventKind{
		"stage_started":   EventStageStarted,
		"agent_output":    EventAgentOutput,
		"stage_completed": EventStageCompleted,
		"stage_failed":    EventStageFailed,
		"gate_awaiting":   EventGateAwaiting,
		"gate_approved":   EventGateApproved,
		"gate_rejected":   EventGateRejected,
		"task_done":       EventTaskDone,
		"task_cancelled":  EventTaskCancelled,
	}
	for want, got := range kinds {
		if string(got) != want {
			t.Errorf("EventKind = %q, want %q", got, want)
		}
	}
}

func TestAgentEventJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	evt := AgentEvent{
		TaskID:    "task-001",
		StageID:   "spec",
		Timestamp: now,
		Kind:      EventStageStarted,
		Message:   "Starting spec stage",
		Metadata:  map[string]any{"key": "value"},
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskID != evt.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, evt.TaskID)
	}
	if got.Kind != evt.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, evt.Kind)
	}
	if got.Message != evt.Message {
		t.Errorf("Message = %q, want %q", got.Message, evt.Message)
	}
}

func TestAgentEventNilMetadata(t *testing.T) {
	evt := AgentEvent{
		TaskID:    "t1",
		StageID:   "s1",
		Timestamp: time.Now().UTC(),
		Kind:      EventStageStarted,
		Message:   "test",
		Metadata:  nil,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if s == "" {
		t.Error("expected non-empty JSON")
	}
	// nil metadata should serialize as "metadata":{}
	if !containsStr(s, `"metadata":{}`) {
		t.Errorf("nil metadata should serialize as {}, got %s", s)
	}
}
