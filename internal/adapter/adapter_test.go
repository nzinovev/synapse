package adapter

import (
	"strings"
	"testing"

	"github.com/nzinovev/synapse/internal/domain"
)

func TestAdapterRegistry_CreateUnknown(t *testing.T) {
	registry := NewRegistry()
	_, err := registry.Create("nonexistent", domain.AdapterConfig{})
	if err == nil {
		t.Error("expected error for unknown adapter")
	}
}

func TestRegistryNames(t *testing.T) {
	registry := NewRegistry()
	registry.Register("alpha", func(cfg domain.AdapterConfig) (AgentAdapter, error) { return nil, nil })
	registry.Register("beta", func(cfg domain.AdapterConfig) (AgentAdapter, error) { return nil, nil })

	names := registry.Names()
	if len(names) != 2 {
		t.Errorf("Names() = %v, want 2 entries", names)
	}
}

func TestSelectableNames(t *testing.T) {
	r := NewRegistry()
	r.Register("claude_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) { return nil, nil })
	r.Register("cursor_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) { return nil, nil })

	names := r.SelectableNames()
	if len(names) != 2 {
		t.Errorf("SelectableNames() = %v, want 2 entries", names)
	}
}

func TestHas(t *testing.T) {
	r := NewRegistry()
	r.Register("claude_cli", func(cfg domain.AdapterConfig) (AgentAdapter, error) { return nil, nil })

	if !r.Has("claude_cli") {
		t.Error("Has(claude_cli) should be true")
	}
	if r.Has("nonexistent") {
		t.Error("Has(nonexistent) should be false")
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
