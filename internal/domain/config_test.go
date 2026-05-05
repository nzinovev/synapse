package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSynapseConfig(t *testing.T) {
	cfg := DefaultSynapseConfig()
	if cfg.Adapter != "claude_cli" {
		t.Errorf("Adapter = %q, want claude_cli", cfg.Adapter)
	}
	if cfg.WorkerCount != 2 {
		t.Errorf("WorkerCount = %d, want 2", cfg.WorkerCount)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 8765 {
		t.Errorf("Port = %d, want 8765", cfg.Port)
	}
	if cfg.DBPath == "" {
		t.Error("DBPath should have a default")
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	synapseDir := filepath.Join(dir, ".synapse")
	os.MkdirAll(synapseDir, 0o755)

	configData := map[string]any{
		"adapter": "claude_cli",
		"adapter_config": map[string]any{
			"agent_prompts_dir": "/tmp/agents",
		},
		"pipelines_dir": "/tmp/pipelines",
	}
	data, _ := json.MarshalIndent(configData, "", "  ")
	os.WriteFile(filepath.Join(synapseDir, "config.json"), data, 0o644)

	cfg, err := LoadConfig(synapseDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Adapter != "claude_cli" {
		t.Errorf("Adapter = %q, want claude_cli", cfg.Adapter)
	}
	if cfg.AdapterConfig.AgentPromptsDir != "/tmp/agents" {
		t.Errorf("AgentPromptsDir = %q, want /tmp/agents", cfg.AdapterConfig.AgentPromptsDir)
	}
	// Defaults should be applied for missing fields
	if cfg.WorkerCount != 2 {
		t.Errorf("WorkerCount = %d, want 2", cfg.WorkerCount)
	}
	if cfg.Port != 8765 {
		t.Errorf("Port = %d, want 8765", cfg.Port)
	}
	if cfg.AdapterConfig.ClaudeBinary != "claude" {
		t.Errorf("ClaudeBinary = %q, want claude", cfg.AdapterConfig.ClaudeBinary)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	synapseDir := filepath.Join(dir, ".synapse")

	cfg := DefaultSynapseConfig()
	cfg.Adapter = "claude_cli"
	cfg.PipelinesDir = "/tmp/pipes"

	if err := cfg.Save(synapseDir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadConfig(synapseDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Adapter != "claude_cli" {
		t.Errorf("Adapter = %q, want claude_cli", loaded.Adapter)
	}
	if loaded.PipelinesDir != "/tmp/pipes" {
		t.Errorf("PipelinesDir = %q, want /tmp/pipes", loaded.PipelinesDir)
	}
}

func TestFindSynapseDir(t *testing.T) {
	dir := t.TempDir()
	synapseDir := filepath.Join(dir, ".synapse")
	os.MkdirAll(synapseDir, 0o755)

	found, err := FindSynapseDir(dir)
	if err != nil {
		t.Fatalf("FindSynapseDir: %v", err)
	}
	if found != synapseDir {
		t.Errorf("FindSynapseDir = %q, want %q", found, synapseDir)
	}
}

func TestFindSynapseDirWalksUp(t *testing.T) {
	dir := t.TempDir()
	synapseDir := filepath.Join(dir, ".synapse")
	os.MkdirAll(synapseDir, 0o755)
	subDir := filepath.Join(dir, "sub", "deep")
	os.MkdirAll(subDir, 0o755)

	found, err := FindSynapseDir(subDir)
	if err != nil {
		t.Fatalf("FindSynapseDir: %v", err)
	}
	if found != synapseDir {
		t.Errorf("FindSynapseDir = %q, want %q", found, synapseDir)
	}
}

func TestFindSynapseDirNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindSynapseDir(dir)
	if err == nil {
		t.Error("expected error when no .synapse dir found")
	}
}

func TestAdapterConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	synapseDir := filepath.Join(dir, ".synapse")
	os.MkdirAll(synapseDir, 0o755)

	// Config with only required fields
	configData := map[string]any{
		"adapter":        "claude_cli",
		"adapter_config": map[string]any{},
		"pipelines_dir":  "/tmp/p",
	}
	data, _ := json.MarshalIndent(configData, "", "  ")
	os.WriteFile(filepath.Join(synapseDir, "config.json"), data, 0o644)

	cfg, err := LoadConfig(synapseDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AdapterConfig.ClaudeBinary != "claude" {
		t.Errorf("ClaudeBinary = %q, want claude", cfg.AdapterConfig.ClaudeBinary)
	}
	if cfg.AdapterConfig.AgentBinary != "agent" {
		t.Errorf("AgentBinary = %q, want agent", cfg.AdapterConfig.AgentBinary)
	}
	if cfg.AdapterConfig.CLIProfile != "claude" {
		t.Errorf("CLIProfile = %q, want claude", cfg.AdapterConfig.CLIProfile)
	}
}

func TestGetHubDir(t *testing.T) {
	hub := GetHubDir()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".synapse")
	if hub != want {
		t.Errorf("GetHubDir = %q, want %q", hub, want)
	}
}
