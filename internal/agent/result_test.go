package agent_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
)

func TestReadStageResultRoundTrip(t *testing.T) {
	stageDir := t.TempDir()
	want := agent.RunResult{
		SchemaVersion: agent.SchemaVersion,
		Status:        agent.StatusNeedsInput,
		Summary:       "implementation is blocked on a product decision",
		Artifacts: []agent.ArtifactRef{
			{Path: "docs/specs/foo.md", StageID: "spec", Description: "spec file"},
		},
		OpenQuestions: []agent.Question{
			{ID: "q1", Text: "Which API shape should we use?", Answer: "Use option B"},
		},
		Verdict:  agent.VerdictBlocked,
		Metadata: map[string]string{"source": "test"},
		FromFile: true,
	}
	writeResultJSON(t, stageDir, want)

	got, err := agent.ReadStageResult(stageDir)
	if err != nil {
		t.Fatalf("ReadStageResult: %v", err)
	}
	if got == nil {
		t.Fatal("ReadStageResult returned nil result")
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("result mismatch\ngot:  %+v\nwant: %+v", *got, want)
	}
}

func TestReadStageResultMissingFile(t *testing.T) {
	got, err := agent.ReadStageResult(t.TempDir())
	if err != nil {
		t.Fatalf("ReadStageResult: %v", err)
	}
	if got != nil {
		t.Fatalf("ReadStageResult = %+v, want nil", got)
	}
}

func TestReadStageResultMalformedJSON(t *testing.T) {
	stageDir := t.TempDir()
	path := filepath.Join(stageDir, "result.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":`), 0o644); err != nil {
		t.Fatalf("write result.json: %v", err)
	}

	_, err := agent.ReadStageResult(stageDir)
	if err == nil {
		t.Fatal("ReadStageResult returned nil error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error %q does not include path %q", err, path)
	}
}

func TestReadStageResultUnknownSchemaVersion(t *testing.T) {
	stageDir := t.TempDir()
	writeResultJSON(t, stageDir, agent.RunResult{
		SchemaVersion: "synapse.result.v2",
		Status:        agent.StatusCompleted,
	})

	_, err := agent.ReadStageResult(stageDir)
	if err == nil {
		t.Fatal("ReadStageResult returned nil error")
	}
	if !strings.Contains(err.Error(), "unknown schema_version") {
		t.Fatalf("error %q does not mention unknown schema version", err)
	}
}

func writeResultJSON(t *testing.T, stageDir string, result agent.RunResult) {
	t.Helper()

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "result.json"), data, 0o644); err != nil {
		t.Fatalf("write result.json: %v", err)
	}
}
