package agent

import (
	"context"
	"testing"
)

func TestMockAgent_DefaultName(t *testing.T) {
	m := &MockAgent{}
	if m.Name() != "mock" {
		t.Errorf("Name() = %q, want %q", m.Name(), "mock")
	}
}

func TestMockAgent_CustomName(t *testing.T) {
	m := &MockAgent{NameFn: func() string { return "custom" }}
	if m.Name() != "custom" {
		t.Errorf("Name() = %q, want %q", m.Name(), "custom")
	}
}

func TestMockAgent_DefaultRun(t *testing.T) {
	m := &MockAgent{}
	result, err := m.Run(context.Background(), RunInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("Status = %q, want %q", result.Status, StatusCompleted)
	}
	if result.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", result.SchemaVersion, SchemaVersion)
	}
}

func TestMockAgent_CustomRun(t *testing.T) {
	m := &MockAgent{
		RunFn: func(ctx context.Context, input RunInput) (RunResult, error) {
			return RunResult{
				SchemaVersion: SchemaVersion,
				Status:        StatusFailed,
				Summary:       "custom failure",
			}, nil
		},
	}
	result, err := m.Run(context.Background(), RunInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", result.Status, StatusFailed)
	}
	if result.Summary != "custom failure" {
		t.Errorf("Summary = %q, want %q", result.Summary, "custom failure")
	}
}
