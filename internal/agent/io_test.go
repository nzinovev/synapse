package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStageInput_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	input := RunInput{
		SchemaVersion: SchemaVersion,
		TaskID:        "task-123",
		Goal:          "do something",
		StageID:       "spec",
	}
	if err := WriteStageInput(dir, input); err != nil {
		t.Fatalf("WriteStageInput: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "input.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got RunInput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.TaskID != input.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, input.TaskID)
	}
	if got.Goal != input.Goal {
		t.Errorf("Goal = %q, want %q", got.Goal, input.Goal)
	}
}

func TestWriteStageResult_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	result := RunResult{
		SchemaVersion: SchemaVersion,
		Status:        StatusCompleted,
		Summary:       "done",
	}
	if err := WriteStageResult(dir, result); err != nil {
		t.Fatalf("WriteStageResult: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got RunResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Status != result.Status {
		t.Errorf("Status = %q, want %q", got.Status, result.Status)
	}
}

func TestWriteStageResult_SkipsIfExists(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"schema_version":"synapse.result.v1","status":"completed","summary":"original"}`)
	path := filepath.Join(dir, "result.json")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	newResult := RunResult{
		SchemaVersion: SchemaVersion,
		Status:        StatusFailed,
		Summary:       "overwritten",
	}
	if err := WriteStageResult(dir, newResult); err != nil {
		t.Fatalf("WriteStageResult: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "original") {
		t.Error("expected original content to be preserved")
	}
}

func TestWriteStageArtifacts_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	refs := []ArtifactRef{
		{Path: "/a/b/c.md", StageID: "spec"},
		{Path: "/a/b/d.md", StageID: "spec"},
	}
	if err := WriteStageArtifacts(dir, refs); err != nil {
		t.Fatalf("WriteStageArtifacts: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "artifacts.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got []ArtifactRef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != len(refs) {
		t.Fatalf("len = %d, want %d", len(got), len(refs))
	}
	if got[0].Path != refs[0].Path {
		t.Errorf("Path = %q, want %q", got[0].Path, refs[0].Path)
	}
}

func TestWriteStageLogs_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WriteStageLogs(dir, "hello stdout", "hello stderr"); err != nil {
		t.Fatalf("WriteStageLogs: %v", err)
	}
	stdout, _ := os.ReadFile(filepath.Join(dir, "stdout.log"))
	stderr, _ := os.ReadFile(filepath.Join(dir, "stderr.log"))
	if string(stdout) != "hello stdout" {
		t.Errorf("stdout = %q, want %q", stdout, "hello stdout")
	}
	if string(stderr) != "hello stderr" {
		t.Errorf("stderr = %q, want %q", stderr, "hello stderr")
	}
}

func TestWriteStageLogs_Truncation(t *testing.T) {
	dir := t.TempDir()
	large := strings.Repeat("x", maxLogSize+100)
	if err := WriteStageLogs(dir, large, ""); err != nil {
		t.Fatalf("WriteStageLogs: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "stdout.log"))
	if !strings.HasSuffix(string(data), "[TRUNCATED]") {
		t.Error("expected [TRUNCATED] suffix for oversized log")
	}
	if len(data) <= maxLogSize {
		t.Errorf("expected data length > maxLogSize, got %d", len(data))
	}
}
