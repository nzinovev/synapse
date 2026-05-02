package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const SchemaVersion = "synapse.result.v1"

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

func ReadStageResult(stageDir string) (*RunResult, error) {
	path := filepath.Join(stageDir, "result.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read stage result %s: %w", path, err)
	}

	var result RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse stage result %s: %w", path, err)
	}
	if result.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("parse stage result %s: unknown schema_version %q", path, result.SchemaVersion)
	}

	return &result, nil
}
