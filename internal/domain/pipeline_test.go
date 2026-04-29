package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBackendPipeline(t *testing.T) {
	p, err := LoadBundledPipelineByName("backend")
	if err != nil {
		t.Fatalf("load backend pipeline: %v", err)
	}
	if p.Name != "backend" {
		t.Errorf("name = %q, want %q", p.Name, "backend")
	}
	if len(p.Stages) != 6 {
		t.Fatalf("stages = %d, want 6", len(p.Stages))
	}
	wantIDs := []string{"spec", "adr", "implement", "review", "fix", "done"}
	for i, s := range p.Stages {
		if s.ID != wantIDs[i] {
			t.Errorf("stage[%d].ID = %q, want %q", i, s.ID, wantIDs[i])
		}
	}
}

func TestLoadFrontendPipeline(t *testing.T) {
	p, err := LoadBundledPipelineByName("frontend")
	if err != nil {
		t.Fatalf("load frontend pipeline: %v", err)
	}
	if p.Name != "frontend" {
		t.Errorf("name = %q, want %q", p.Name, "frontend")
	}
	if len(p.Stages) != 7 {
		t.Fatalf("stages = %d, want 7", len(p.Stages))
	}
	wantIDs := []string{"spec", "ui", "adr", "implement", "review", "fix", "done"}
	for i, s := range p.Stages {
		if s.ID != wantIDs[i] {
			t.Errorf("stage[%d].ID = %q, want %q", i, s.ID, wantIDs[i])
		}
	}
}

func TestListBundledPipelines(t *testing.T) {
	pipelines, err := ListBundledPipelines()
	if err != nil {
		t.Fatalf("list bundled pipelines: %v", err)
	}
	names := map[string]bool{}
	for _, p := range pipelines {
		names[p.Name] = true
	}
	if !names["backend"] {
		t.Error("backend pipeline not found")
	}
	if !names["frontend"] {
		t.Error("frontend pipeline not found")
	}
}

func TestStageIndex(t *testing.T) {
	p, _ := LoadBundledPipelineByName("backend")
	tests := []struct {
		stageID string
		want    int
	}{
		{"spec", 0}, {"adr", 1}, {"implement", 2}, {"review", 3}, {"fix", 4}, {"done", 5},
	}
	for _, tt := range tests {
		got, err := p.StageIndex(tt.stageID)
		if err != nil {
			t.Errorf("StageIndex(%q) error: %v", tt.stageID, err)
		}
		if got != tt.want {
			t.Errorf("StageIndex(%q) = %d, want %d", tt.stageID, got, tt.want)
		}
	}
}

func TestStageIndexMissing(t *testing.T) {
	p, _ := LoadBundledPipelineByName("backend")
	_, err := p.StageIndex("nonexistent")
	if err == nil {
		t.Error("expected error for missing stage")
	}
}

func TestNextStage(t *testing.T) {
	p, _ := LoadBundledPipelineByName("backend")
	next, err := p.NextStage("spec")
	if err != nil {
		t.Fatalf("NextStage(spec): %v", err)
	}
	if next == nil || next.ID != "adr" {
		t.Errorf("NextStage(spec) = %v, want adr", next)
	}
}

func TestNextStageLast(t *testing.T) {
	p, _ := LoadBundledPipelineByName("backend")
	next, err := p.NextStage("done")
	if err != nil {
		t.Fatalf("NextStage(done): %v", err)
	}
	if next != nil {
		t.Errorf("NextStage(done) = %v, want nil", next)
	}
}

func TestGateValues(t *testing.T) {
	p, _ := LoadBundledPipelineByName("backend")
	wantGates := map[string]Gate{
		"spec":      GateAutoIfClean,
		"adr":       GateHumanApproval,
		"implement": GateAuto,
		"review":    GateAutoOnApproval,
		"fix":       GateAuto,
		"done":      GateHumanFinal,
	}
	for _, s := range p.Stages {
		if s.Gate != wantGates[s.ID] {
			t.Errorf("stage %q gate = %q, want %q", s.ID, s.Gate, wantGates[s.ID])
		}
	}
}

func TestDuplicateStageIDsRejected(t *testing.T) {
	dir := t.TempDir()
	bad := []byte("name: bad\nstages:\n  - { id: spec, agent: spec-writer, gate: auto }\n  - { id: spec, agent: adr-architect, gate: auto }\n")
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, bad, 0o644)
	_, err := LoadPipeline(path)
	if err == nil {
		t.Error("expected error for duplicate stage IDs")
	}
}

func TestUnknownGateRejected(t *testing.T) {
	dir := t.TempDir()
	bad := []byte("name: bad\nstages:\n  - { id: spec, agent: spec-writer, gate: magic }\n")
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, bad, 0o644)
	_, err := LoadPipeline(path)
	if err == nil {
		t.Error("expected error for unknown gate")
	}
}

func TestPipelineNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPipelineByName("nonexistent", dir)
	if err == nil {
		t.Error("expected error for nonexistent pipeline")
	}
}

func TestLoadPipelineByNameFromDir(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte("name: test\nstages:\n  - { id: build, agent: builder, gate: auto }\n")
	os.WriteFile(filepath.Join(dir, "test.yaml"), yaml, 0o644)
	p, err := LoadPipelineByName("test", dir)
	if err != nil {
		t.Fatalf("LoadPipelineByName: %v", err)
	}
	if p.Name != "test" {
		t.Errorf("name = %q, want %q", p.Name, "test")
	}
}

func TestListPipelinesFromDir(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte("name: test\nstages:\n  - { id: build, agent: builder, gate: auto }\n")
	os.WriteFile(filepath.Join(dir, "test.yaml"), yaml, 0o644)
	pipelines, err := ListPipelines(dir)
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipelines) != 1 || pipelines[0].Name != "test" {
		t.Errorf("pipelines = %v, want [test]", pipelines)
	}
}

func TestGetStage(t *testing.T) {
	p, _ := LoadBundledPipelineByName("backend")
	s, err := p.GetStage("review")
	if err != nil {
		t.Fatalf("GetStage(review): %v", err)
	}
	if s.Agent != "spec-reviewer" {
		t.Errorf("agent = %q, want spec-reviewer", s.Agent)
	}
}

func TestResolvePipeline(t *testing.T) {
	p, err := ResolvePipeline("backend", "")
	if err != nil {
		t.Fatalf("ResolvePipeline: %v", err)
	}
	if p.Name != "backend" {
		t.Errorf("name = %q, want backend", p.Name)
	}
}

func TestResolvePipelineFallback(t *testing.T) {
	dir := t.TempDir()
	p, err := ResolvePipeline("backend", dir)
	if err != nil {
		t.Fatalf("ResolvePipeline with empty dir: %v", err)
	}
	if p.Name != "backend" {
		t.Errorf("name = %q, want backend", p.Name)
	}
}
