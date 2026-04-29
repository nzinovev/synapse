package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzinovev/synapse/internal/domain"
)

func TestAdapterRegistry(t *testing.T) {
	registry := NewRegistry()
	RegisterFake(registry)

	adapter, err := registry.Create("fake", domain.AdapterConfig{})
	if err != nil {
		t.Fatalf("Create fake adapter: %v", err)
	}
	if adapter.Name() != "fake" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "fake")
	}

	_, err = registry.Create("nonexistent", domain.AdapterConfig{})
	if err == nil {
		t.Error("expected error for unknown adapter")
	}
}

func TestRegistryNames(t *testing.T) {
	registry := NewRegistry()
	RegisterFake(registry)
	RegisterClaudeCLI(registry)

	names := registry.Names()
	if len(names) != 2 {
		t.Errorf("Names() = %v, want 2 entries", names)
	}
}

func TestFakeAdapterSuccess(t *testing.T) {
	fake := &FakeAdapter{}
	result, err := fake.Invoke(t.Context(), domain.InvokeParams{
		AgentName:       "spec-writer",
		TaskDescription: "Add logging to engine",
		WorkingDir:      t.TempDir(),
		StageWorkdir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if !result.Success {
		t.Error("result.Success = false, want true")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", result.ExitCode)
	}
	if len(result.ArtifactsCreated) != 1 {
		t.Fatalf("len(ArtifactsCreated) = %d, want 1", len(result.ArtifactsCreated))
	}
	if !strings.Contains(result.ArtifactsCreated[0], "docs/specs/") {
		t.Errorf("artifact path = %q, want docs/specs/...", result.ArtifactsCreated[0])
	}
	if !strings.Contains(result.Stdout, "SYNAPSE_AGENT_DONE:") {
		t.Error("stdout missing SYNAPSE_AGENT_DONE sentinel")
	}
}

func TestFakeAdapterFailure(t *testing.T) {
	fake := &FakeAdapter{ShouldFail: true}
	result, err := fake.Invoke(t.Context(), domain.InvokeParams{
		AgentName:       "spec-writer",
		TaskDescription: "test task",
		WorkingDir:      t.TempDir(),
		StageWorkdir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Success {
		t.Error("result.Success = true, want false")
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", result.ExitCode)
	}
}

func TestFakeAdapterAllAgents(t *testing.T) {
	agents := []string{"spec-writer", "adr-architect", "ui-designer", "feature-implementer", "fix-implementer", "spec-reviewer"}
	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			fake := &FakeAdapter{}
			result, err := fake.Invoke(t.Context(), domain.InvokeParams{
				AgentName:       agent,
				TaskDescription: "test " + agent,
				WorkingDir:      t.TempDir(),
				StageWorkdir:    t.TempDir(),
			})
			if err != nil {
				t.Fatalf("Invoke(%s): %v", agent, err)
			}
			if !result.Success {
				t.Errorf("%s: Success = false", agent)
			}
		})
	}
}

func TestFakeAdapterUnknownAgent(t *testing.T) {
	fake := &FakeAdapter{}
	result, err := fake.Invoke(t.Context(), domain.InvokeParams{
		AgentName:       "custom-agent",
		TaskDescription: "test task",
		WorkingDir:      t.TempDir(),
		StageWorkdir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !result.Success {
		t.Error("expected success for unknown agent")
	}
	if len(result.ArtifactsCreated) != 0 {
		t.Errorf("expected no artifacts for unknown agent, got %d", len(result.ArtifactsCreated))
	}
}

func TestFakeAdapterPromptsDirCheck(t *testing.T) {
	promptsDir := t.TempDir()
	fake := &FakeAdapter{AgentPromptsDir: promptsDir}
	_, err := fake.Invoke(t.Context(), domain.InvokeParams{
		AgentName:       "spec-writer",
		TaskDescription: "test task",
		WorkingDir:      t.TempDir(),
		StageWorkdir:    t.TempDir(),
	})
	if err == nil {
		t.Error("expected error when prompt file is missing")
	}
}

func TestFakeAdapterPromptsDirExists(t *testing.T) {
	promptsDir := t.TempDir()
	fake := &FakeAdapter{AgentPromptsDir: promptsDir}

	// Create the prompt file.
	writeFile(t, promptsDir+"/spec-writer.md", "# Spec writer prompt")

	result, err := fake.Invoke(t.Context(), domain.InvokeParams{
		AgentName:       "spec-writer",
		TaskDescription: "test task",
		WorkingDir:      t.TempDir(),
		StageWorkdir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Add logging to engine", "add-logging-to-engine"},
		{"Hello World! @#$%", "hello-world"},
		{"a", "a"},
		{strings.Repeat("x", 100), strings.Repeat("x", 50)},
		{"  spaces  ", "spaces"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if got := truncateOutput(short, 100); got != short {
		t.Errorf("truncate(short) = %q, want %q", got, short)
	}

	long := strings.Repeat("x", 1000)
	got := truncateOutput(long, 100)
	if len(got) > 150 {
		t.Errorf("truncated output too long: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "[... truncated ...]") {
		t.Error("truncated output missing marker")
	}
}

func TestBuildTaskPrompt(t *testing.T) {
	prompt := BuildTaskPrompt(
		"Add logging to engine",
		"/tmp/workdir",
		map[string][]string{"spec": {"/tmp/spec.md"}},
		nil,
		nil,
		"implement",
		domain.GateAuto,
		"backend",
		"feature-implementer",
		0,
		1,
		nil,
		nil,
	)

	if !strings.Contains(prompt, "# Task\nAdd logging to engine") {
		t.Error("prompt missing task description")
	}
	if !strings.Contains(prompt, "# Context from prior stages") {
		t.Error("prompt missing context section")
	}
	if !strings.Contains(prompt, "See: /tmp/spec.md  (from stage: spec)") {
		t.Error("prompt missing artifact reference")
	}
	if !strings.Contains(prompt, "SYNAPSE_AGENT_DONE:") {
		t.Error("prompt missing SYNAPSE_AGENT_DONE instruction")
	}
	if !strings.Contains(prompt, "/tmp/workdir") {
		t.Error("prompt missing working directory")
	}
}

func TestBuildTaskPromptWithFeedback(t *testing.T) {
	feedback := "Fix the tests"
	prompt := BuildTaskPrompt(
		"Add logging",
		"/tmp/workdir",
		nil,
		&feedback,
		nil,
		"fix",
		domain.GateAuto,
		"backend",
		"fix-implementer",
		1,
		1,
		nil,
		nil,
	)
	if !strings.Contains(prompt, "Fix the tests") {
		t.Error("prompt missing rejection feedback")
	}
}

func TestBuildTaskPromptWithPreviousOutput(t *testing.T) {
	stdout := "previous output"
	stderr := "previous errors"
	prompt := BuildTaskPrompt(
		"Add logging",
		"/tmp/workdir",
		nil,
		nil,
		nil,
		"fix",
		domain.GateAuto,
		"backend",
		"fix-implementer",
		1,
		1,
		&stdout,
		&stderr,
	)
	if !strings.Contains(prompt, "## stdout\nprevious output") {
		t.Error("prompt missing previous stdout")
	}
	if !strings.Contains(prompt, "## stderr\nprevious errors") {
		t.Error("prompt missing previous stderr")
	}
}

func TestFormatRoutingBlock(t *testing.T) {
	block := FormatRoutingBlock("implement", domain.GateAuto, "feature-implementer", "backend", 0, 1)
	if !strings.Contains(block, "**Pipeline:** `backend`") {
		t.Error("missing pipeline line")
	}
	if !strings.Contains(block, "**Stage:** `implement`") {
		t.Error("missing stage line")
	}
	if !strings.Contains(block, "**Agent:** `feature-implementer`") {
		t.Error("missing agent line")
	}
	if !strings.Contains(block, "**Gate:** `auto`") {
		t.Error("missing gate line")
	}
	if strings.Contains(block, "fix_cycle_count") {
		t.Error("fix_cycle_count line should not appear when fixCycleCount=0")
	}
}

func TestFormatRoutingBlockWithFixLoop(t *testing.T) {
	block := FormatRoutingBlock("fix", domain.GateAuto, "fix-implementer", "backend", 2, 1)
	if !strings.Contains(block, "fix_cycle_count") {
		t.Error("missing fix loop line")
	}
	if !strings.Contains(block, "= 2") {
		t.Error("missing fix cycle count value")
	}
}

func TestFormatRoutingBlockWithPRIndex(t *testing.T) {
	block := FormatRoutingBlock("implement", domain.GateAuto, "feature-implementer", "backend", 0, 3)
	if !strings.Contains(block, "This is PR 3") {
		t.Error("missing PR index info")
	}
	if !strings.Contains(block, "PR 2") {
		t.Error("missing previous PR reference")
	}
}

func TestFormatRoutingBlockUnknownStage(t *testing.T) {
	block := FormatRoutingBlock("custom", domain.GateAuto, "custom-agent", "backend", 0, 1)
	if !strings.Contains(block, "Custom stage `custom`") {
		t.Error("missing custom stage text")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func TestSelectableNamesExcludesFake(t *testing.T) {
	r := NewRegistry()
	RegisterFake(r)
	r.Register("claude_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) {
		return &FakeAdapter{}, nil
	})

	names := r.SelectableNames()
	for _, n := range names {
		if n == "fake" {
			t.Error("SelectableNames should not include fake")
		}
	}
	if len(names) != 1 || names[0] != "claude_cli" {
		t.Errorf("SelectableNames = %v, want [claude_cli]", names)
	}
}

func TestHas(t *testing.T) {
	r := NewRegistry()
	RegisterFake(r)

	if !r.Has("fake") {
		t.Error("Has(fake) should be true")
	}
	if r.Has("nonexistent") {
		t.Error("Has(nonexistent) should be false")
	}
}
