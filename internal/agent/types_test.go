package agent_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
)

func TestRunResultRoundTrip(t *testing.T) {
	original := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusCompleted,
		Summary:       "all stages passed",
		Artifacts: []agent.ArtifactRef{
			{Path: "docs/specs/foo.md", StageID: "spec", Description: "spec file"},
		},
		OpenQuestions: []agent.Question{
			{ID: "q1", Text: "Is this right?", Answer: "Yes"},
		},
		Verdict: agent.VerdictApproved,
		Stdout:  "ok\n",
		Stderr:  "",
		DurationSeconds: 4.2,
		Usage: &agent.ModelUsage{
			Model:            "claude-sonnet-4-6",
			InputTokens:      1000,
			OutputTokens:     200,
			CacheReadTokens:  50,
			CacheWriteTokens: 10,
		},
		Metadata: map[string]string{"key": "value"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got agent.RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(original, got) {
		t.Errorf("round-trip mismatch\ngot:  %+v\nwant: %+v", got, original)
	}
}
