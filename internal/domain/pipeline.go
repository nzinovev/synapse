package domain

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type Gate string

const (
	GateAuto           Gate = "auto"
	GateAutoIfClean    Gate = "auto_if_clean"
	GateHumanApproval  Gate = "human_approval"
	GateHumanFinal     Gate = "human_final"
	GateAutoOnApproval Gate = "auto_on_approval"
)

func (g Gate) Valid() bool {
	switch g {
	case GateAuto, GateAutoIfClean, GateHumanApproval, GateHumanFinal, GateAutoOnApproval:
		return true
	}
	return false
}

type Stage struct {
	ID           string `yaml:"id"`
	Agent        string `yaml:"agent"`
	Gate         Gate   `yaml:"gate"`
	ProducesGlob string `yaml:"produces_glob"`
	Model        string `yaml:"model"`
	Runtime      string `yaml:"runtime"` // "cli" | "native"; defaults to "cli" when empty
}

type Pipeline struct {
	Name   string  `yaml:"name"`
	Stages []Stage `yaml:"stages"`
}

func (p *Pipeline) Validate() error {
	seen := make(map[string]bool)
	for _, s := range p.Stages {
		if seen[s.ID] {
			return fmt.Errorf("duplicate stage id: %q", s.ID)
		}
		seen[s.ID] = true
		if !s.Gate.Valid() {
			return fmt.Errorf("invalid gate %q for stage %q", s.Gate, s.ID)
		}
	}
	return nil
}

func (p *Pipeline) StageIndex(stageID string) (int, error) {
	for i, s := range p.Stages {
		if s.ID == stageID {
			return i, nil
		}
	}
	return -1, fmt.Errorf("stage %q not found in pipeline %q", stageID, p.Name)
}

func (p *Pipeline) NextStage(current string) (*Stage, error) {
	idx, err := p.StageIndex(current)
	if err != nil {
		return nil, err
	}
	if idx+1 < len(p.Stages) {
		return &p.Stages[idx+1], nil
	}
	return nil, nil
}

func (p *Pipeline) GetStage(stageID string) (*Stage, error) {
	for i := range p.Stages {
		if p.Stages[i].ID == stageID {
			return &p.Stages[i], nil
		}
	}
	return nil, fmt.Errorf("stage %q not found in pipeline %q", stageID, p.Name)
}

func LoadPipeline(path string) (*Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pipeline: %w", err)
	}
	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse pipeline yaml: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func LoadPipelineFromBytes(data []byte) (*Pipeline, error) {
	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse pipeline yaml: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func LoadPipelineByName(name, pipelinesDir string) (*Pipeline, error) {
	for _, ext := range []string{".yaml", ".yml"} {
		path := filepath.Join(pipelinesDir, name+ext)
		if _, err := os.Stat(path); err == nil {
			return LoadPipeline(path)
		}
	}

	entries, err := os.ReadDir(pipelinesDir)
	if err != nil {
		return nil, fmt.Errorf("no pipeline named %q found in %s", name, pipelinesDir)
	}

	var yamlFiles []string
	for _, e := range entries {
		if !e.IsDir() && (filepath.Ext(e.Name()) == ".yaml" || filepath.Ext(e.Name()) == ".yml") {
			yamlFiles = append(yamlFiles, filepath.Join(pipelinesDir, e.Name()))
		}
	}
	sort.Strings(yamlFiles)

	for _, path := range yamlFiles {
		p, err := LoadPipeline(path)
		if err != nil {
			continue
		}
		if p.Name == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no pipeline named %q found in %s", name, pipelinesDir)
}

func ListPipelines(pipelinesDir string) ([]*Pipeline, error) {
	entries, err := os.ReadDir(pipelinesDir)
	if err != nil {
		return nil, nil
	}

	var yamlFiles []string
	for _, e := range entries {
		if !e.IsDir() && (filepath.Ext(e.Name()) == ".yaml" || filepath.Ext(e.Name()) == ".yml") {
			yamlFiles = append(yamlFiles, filepath.Join(pipelinesDir, e.Name()))
		}
	}
	sort.Strings(yamlFiles)

	var pipelines []*Pipeline
	for _, path := range yamlFiles {
		p, err := LoadPipeline(path)
		if err != nil {
			continue
		}
		pipelines = append(pipelines, p)
	}
	return pipelines, nil
}

var PipelinesFS embed.FS

func LoadBundledPipelineByName(name string) (*Pipeline, error) {
	for _, ext := range []string{".yaml", ".yml"} {
		filename := "pipelines/" + name + ext
		data, err := fs.ReadFile(PipelinesFS, filename)
		if err != nil {
			continue
		}
		return LoadPipelineFromBytes(data)
	}

	entries, err := fs.ReadDir(PipelinesFS, "pipelines")
	if err != nil {
		return nil, fmt.Errorf("no bundled pipeline named %q", name)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		filename := "pipelines/" + e.Name()
		data, err := fs.ReadFile(PipelinesFS, filename)
		if err != nil {
			continue
		}
		p, err := LoadPipelineFromBytes(data)
		if err != nil {
			continue
		}
		if p.Name == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no bundled pipeline named %q", name)
}

func ListBundledPipelines() ([]*Pipeline, error) {
	entries, err := fs.ReadDir(PipelinesFS, "pipelines")
	if err != nil {
		return nil, err
	}

	var pipelines []*Pipeline
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		filename := "pipelines/" + e.Name()
		data, err := fs.ReadFile(PipelinesFS, filename)
		if err != nil {
			continue
		}
		p, err := LoadPipelineFromBytes(data)
		if err != nil {
			continue
		}
		pipelines = append(pipelines, p)
	}
	return pipelines, nil
}

func HasBundledPipeline(name string) bool {
	_, err := LoadBundledPipelineByName(name)
	return err == nil
}

func ResolvePipeline(name, pipelinesDir string) (*Pipeline, error) {
	if pipelinesDir != "" {
		p, err := LoadPipelineByName(name, pipelinesDir)
		if err == nil {
			return p, nil
		}
	}

	if HasBundledPipeline(name) {
		return LoadBundledPipelineByName(name)
	}

	if pipelinesDir != "" {
		return nil, fmt.Errorf("no pipeline named %q found in %s or bundled pipelines", name, pipelinesDir)
	}
	return nil, fmt.Errorf("no pipeline named %q found in bundled pipelines", name)
}

func init() {
	// Detect if we have embedded pipelines by trying to read the directory.
	// If PipelinesFS is not set via go:embed, this will be a no-op FS.
	// The actual embedding is done in a separate file that uses go:embed.
	data, err := fs.ReadFile(PipelinesFS, "pipelines/backend.yaml")
	if err == nil && len(data) > 0 {
		_ = data // pipelines are available
	}
}

// EqualBytes compares two pipelines by serializing to YAML and comparing bytes.
func PipelinesEqual(a, b *Pipeline) (bool, error) {
	ya, err := yaml.Marshal(a)
	if err != nil {
		return false, err
	}
	yb, err := yaml.Marshal(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ya, yb), nil
}
