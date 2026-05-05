package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const maxLogSize = 10 * 1024 * 1024 // 10 MB

// WriteStageInput writes the RunInput snapshot to <stageDir>/input.json atomically.
func WriteStageInput(stageDir string, in RunInput) error {
	return writeJSONAtomic(filepath.Join(stageDir, "input.json"), in)
}

// WriteStageResult writes the RunResult to <stageDir>/result.json atomically.
// If result.json already exists (written by the agent itself), it is not overwritten.
func WriteStageResult(stageDir string, r RunResult) error {
	path := filepath.Join(stageDir, "result.json")
	if _, err := os.Stat(path); err == nil {
		return nil // agent already wrote it
	}
	return writeJSONAtomic(path, r)
}

// WriteStageArtifacts writes the artifact list to <stageDir>/artifacts.json atomically.
func WriteStageArtifacts(stageDir string, refs []ArtifactRef) error {
	return writeJSONAtomic(filepath.Join(stageDir, "artifacts.json"), refs)
}

// WriteStageLogs writes stdout.log and stderr.log to stageDir. Content is
// truncated to maxLogSize bytes with a [TRUNCATED] trailer.
func WriteStageLogs(stageDir string, stdout, stderr string) error {
	if err := writeLogAtomic(filepath.Join(stageDir, "stdout.log"), stdout); err != nil {
		return err
	}
	return writeLogAtomic(filepath.Join(stageDir, "stderr.log"), stderr)
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeLogAtomic(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data := []byte(content)
	if len(data) > maxLogSize {
		data = append(data[:maxLogSize], []byte("\n[TRUNCATED]")...)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
