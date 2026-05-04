package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

func TestLoadAgent_UserDirTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create user agent definition.
	userAgentDir := filepath.Join(tmpDir, "agents", "spec-writer")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
name: spec-writer
version: "1.0"
description: "User-defined spec writer"
runtime: cli
model_tier: high
prompt: prompt.md
`
	if err := os.WriteFile(filepath.Join(userAgentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userAgentDir, "prompt.md"), []byte("user prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create legacy prompt file (should NOT be used).
	legacyDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "spec-writer.md"), []byte("legacy prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := domain.SynapseConfig{
		AgentDir: filepath.Join(tmpDir, "agents"),
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: legacyDir,
		},
	}

	def, err := agent.LoadAgent("spec-writer", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Source != "user" {
		t.Errorf("expected source %q, got %q", "user", def.Source)
	}
	if def.Description != "User-defined spec writer" {
		t.Errorf("unexpected description: %s", def.Description)
	}
}

func TestLoadAgent_LegacyFallback(t *testing.T) {
	tmpDir := t.TempDir()

	// Only create legacy prompt file, no user agent dir.
	legacyDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "adr-architect.md"), []byte("legacy prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := domain.SynapseConfig{
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: legacyDir,
		},
	}

	def, err := agent.LoadAgent("adr-architect", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Source != "legacy" {
		t.Errorf("expected source %q, got %q", "legacy", def.Source)
	}
	if def.Runtime != "cli" {
		t.Errorf("expected runtime %q, got %q", "cli", def.Runtime)
	}
	if def.Name != "adr-architect" {
		t.Errorf("expected name %q, got %q", "adr-architect", def.Name)
	}
}

func TestLoadAgent_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := domain.SynapseConfig{
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: filepath.Join(tmpDir, "prompts"),
		},
	}

	_, err := agent.LoadAgent("nonexistent", cfg)
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
}

func TestLoadAgent_MalformedYAML(t *testing.T) {
	tmpDir := t.TempDir()

	userAgentDir := filepath.Join(tmpDir, "agents", "bad-agent")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write invalid YAML.
	if err := os.WriteFile(filepath.Join(userAgentDir, "agent.yaml"), []byte("key: [unclosed list"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No legacy fallback for this agent.
	legacyDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := domain.SynapseConfig{
		AgentDir: filepath.Join(tmpDir, "agents"),
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: legacyDir,
		},
	}

	_, err := agent.LoadAgent("bad-agent", cfg)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoadAgent_MissingPromptFile(t *testing.T) {
	tmpDir := t.TempDir()

	// User agent dir with agent.yaml referencing a missing prompt.md.
	userAgentDir := filepath.Join(tmpDir, "agents", "no-prompt")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
name: no-prompt
version: "1.0"
description: "Agent with missing prompt"
runtime: cli
prompt: missing_prompt.md
`
	if err := os.WriteFile(filepath.Join(userAgentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// No legacy fallback for this agent.
	legacyDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := domain.SynapseConfig{
		AgentDir: filepath.Join(tmpDir, "agents"),
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: legacyDir,
		},
	}

	// Loading should succeed — the loader does not validate that the prompt
	// file exists at load time (that is the adapter's responsibility at
	// invocation time). The definition is loaded and the Prompt field is set
	// to the resolved path.
	def, err := agent.LoadAgent("no-prompt", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Source != "user" {
		t.Errorf("expected source %q, got %q", "user", def.Source)
	}
	// The prompt path should be resolved relative to the agent directory.
	expectedPrompt := filepath.Join(userAgentDir, "missing_prompt.md")
	if def.Prompt != expectedPrompt {
		t.Errorf("expected prompt path %q, got %q", expectedPrompt, def.Prompt)
	}
}

func TestListAgents_SortedAndMerged(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two legacy agents.
	legacyDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "beta.md"), []byte("beta prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "alpha.md"), []byte("alpha prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create one user agent that overrides "alpha".
	userAgentDir := filepath.Join(tmpDir, "agents", "alpha")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
description: "User alpha override"
runtime: native
`
	if err := os.WriteFile(filepath.Join(userAgentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create an additional user agent "gamma".
	gammaDir := filepath.Join(tmpDir, "agents", "gamma")
	if err := os.MkdirAll(gammaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gammaDir, "agent.yaml"), []byte("description: gamma\nruntime: cli\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := domain.SynapseConfig{
		AgentDir: filepath.Join(tmpDir, "agents"),
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: legacyDir,
		},
	}

	agents, err := agent.ListAgents(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// Should be sorted alphabetically: alpha, beta, gamma.
	if agents[0].Name != "alpha" || agents[1].Name != "beta" || agents[2].Name != "gamma" {
		t.Errorf("expected [alpha, beta, gamma], got %v", namesOf(agents))
	}

	// "alpha" should be the user override, not legacy.
	if agents[0].Source != "user" {
		t.Errorf("expected alpha source %q, got %q", "user", agents[0].Source)
	}
	if agents[0].Runtime != "native" {
		t.Errorf("expected alpha runtime %q, got %q", "native", agents[0].Runtime)
	}

	// "beta" should be legacy.
	if agents[1].Source != "legacy" {
		t.Errorf("expected beta source %q, got %q", "legacy", agents[1].Source)
	}

	// "gamma" should be user.
	if agents[2].Source != "user" {
		t.Errorf("expected gamma source %q, got %q", "user", agents[2].Source)
	}
}

func TestListAgents_EmptyDirs(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := domain.SynapseConfig{
		AgentDir: filepath.Join(tmpDir, "agents"),
		AdapterConfig: domain.AdapterConfig{
			AgentPromptsDir: filepath.Join(tmpDir, "prompts"),
		},
	}

	agents, err := agent.ListAgents(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestListAgents_NoDirsConfigured(t *testing.T) {
	cfg := domain.SynapseConfig{}

	agents, err := agent.ListAgents(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestLoadAgent_PromptPathResolved(t *testing.T) {
	tmpDir := t.TempDir()

	userAgentDir := filepath.Join(tmpDir, "agents", "path-test")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentYAML := `
description: "Path resolution test"
runtime: cli
prompt: subdir/prompt.md
`
	if err := os.WriteFile(filepath.Join(userAgentDir, "agent.yaml"), []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := domain.SynapseConfig{
		AgentDir: filepath.Join(tmpDir, "agents"),
	}

	def, err := agent.LoadAgent("path-test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(userAgentDir, "subdir", "prompt.md")
	if def.Prompt != expected {
		t.Errorf("expected prompt %q, got %q", expected, def.Prompt)
	}
}

func namesOf(agents []agent.AgentDefinition) []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return names
}
