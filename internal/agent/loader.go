package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nzinovev/synapse/internal/domain"
	"gopkg.in/yaml.v3"
)

// LoadAgent discovers and loads an agent definition by name.
// Discovery order:
//  1. <cfg.AgentDir>/<name>/agent.yaml (new layout, Source: "user")
//  2. <cfg.AdapterConfig.AgentPromptsDir>/<name>.md (legacy, Source: "legacy")
//  3. Bundled (deferred to T12 — returns error for now)
func LoadAgent(name string, cfg domain.SynapseConfig) (AgentDefinition, error) {
	if def, err := loadFromUserDir(name, cfg); err == nil {
		return def, nil
	}

	if def, err := loadFromLegacyDir(name, cfg); err == nil {
		return def, nil
	}

	if def, err := loadFromBundledFS(name); err == nil {
		return def, nil
	}
	return AgentDefinition{}, fmt.Errorf("agent %q not found", name)
}

// ListAgents scans all agent sources and returns the union, sorted by name.
// Priority: user > legacy > bundled.
func ListAgents(cfg domain.SynapseConfig) ([]AgentDefinition, error) {
	seen := make(map[string]bool)
	var result []AgentDefinition

	// Collect bundled agents first (lowest priority).
	for _, def := range listBundledAgents() {
		if !seen[def.Name] {
			seen[def.Name] = true
			result = append(result, def)
		}
	}

	// Collect legacy agents — replace bundled entry when names collide.
	if legacy, err := listLegacyAgents(cfg); err == nil {
		for _, def := range legacy {
			if seen[def.Name] {
				for i, existing := range result {
					if existing.Name == def.Name {
						result[i] = def
						break
					}
				}
			} else {
				seen[def.Name] = true
				result = append(result, def)
			}
		}
	}

	// Collect user-defined agents — replace bundled/legacy entry when names collide.
	if user, err := listUserAgents(cfg); err == nil {
		for _, def := range user {
			if seen[def.Name] {
				for i, existing := range result {
					if existing.Name == def.Name {
						result[i] = def
						break
					}
				}
			} else {
				seen[def.Name] = true
				result = append(result, def)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// loadFromBundledFS attempts to load an agent from the embedded agents FS.
// The prompt field in agent.yaml is resolved to inline content.
func loadFromBundledFS(name string) (AgentDefinition, error) {
	yamlPath := "agents/" + name + "/agent.yaml"
	data, err := BundledAgentsFS.ReadFile(yamlPath)
	if err != nil {
		return AgentDefinition{}, err
	}

	var def AgentDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return AgentDefinition{}, fmt.Errorf("parse bundled agent %s: %w", yamlPath, err)
	}

	def.Name = name
	def.Source = "bundled"

	if def.Prompt != "" {
		promptPath := "agents/" + name + "/" + def.Prompt
		promptData, err := BundledAgentsFS.ReadFile(promptPath)
		if err != nil {
			return AgentDefinition{}, fmt.Errorf("read bundled prompt %s: %w", promptPath, err)
		}
		def.Prompt = string(promptData)
	}

	return def, nil
}

// listBundledAgents returns all agents from the embedded agents FS.
// Errors on individual entries are silently skipped.
func listBundledAgents() []AgentDefinition {
	entries, err := BundledAgentsFS.ReadDir("agents")
	if err != nil {
		return nil
	}

	var result []AgentDefinition
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		def, err := loadFromBundledFS(entry.Name())
		if err != nil {
			continue
		}
		result = append(result, def)
	}
	return result
}

// loadFromUserDir attempts to load <cfg.AgentDir>/<name>/agent.yaml.
func loadFromUserDir(name string, cfg domain.SynapseConfig) (AgentDefinition, error) {
	if cfg.AgentDir == "" {
		return AgentDefinition{}, fmt.Errorf("no agent dir configured")
	}

	dir := filepath.Join(cfg.AgentDir, name)
	yamlPath := filepath.Join(dir, "agent.yaml")

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return AgentDefinition{}, err
	}

	var def AgentDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return AgentDefinition{}, fmt.Errorf("parse %s: %w", yamlPath, err)
	}

	def.Name = name
	def.Source = "user"

	// Resolve prompt path relative to the agent directory.
	if def.Prompt != "" && !filepath.IsAbs(def.Prompt) {
		def.Prompt = filepath.Join(dir, def.Prompt)
	}

	return def, nil
}

// loadFromLegacyDir attempts to load <cfg.AdapterConfig.AgentPromptsDir>/<name>.md
// and synthesizes a minimal AgentDefinition.
func loadFromLegacyDir(name string, cfg domain.SynapseConfig) (AgentDefinition, error) {
	promptsDir := cfg.AdapterConfig.AgentPromptsDir
	if promptsDir == "" {
		return AgentDefinition{}, fmt.Errorf("no agent prompts dir configured")
	}

	promptPath := filepath.Join(promptsDir, name+".md")

	info, err := os.Stat(promptPath)
	if err != nil {
		return AgentDefinition{}, err
	}

	if info.IsDir() {
		return AgentDefinition{}, fmt.Errorf("%q is a directory, not a file", promptPath)
	}

	return AgentDefinition{
		Name:    name,
		Runtime: "cli",
		Prompt:  promptPath,
		Source:  "legacy",
	}, nil
}

// listUserAgents scans cfg.AgentDir for subdirectories containing agent.yaml.
func listUserAgents(cfg domain.SynapseConfig) ([]AgentDefinition, error) {
	if cfg.AgentDir == "" {
		return nil, nil
	}

	dir, err := os.ReadDir(cfg.AgentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []AgentDefinition
	for _, entry := range dir {
		if !entry.IsDir() {
			continue
		}
		def, err := loadFromUserDir(entry.Name(), cfg)
		if err != nil {
			continue // skip unreadable definitions
		}
		result = append(result, def)
	}
	return result, nil
}

// listLegacyAgents scans cfg.AdapterConfig.AgentPromptsDir for .md files.
func listLegacyAgents(cfg domain.SynapseConfig) ([]AgentDefinition, error) {
	promptsDir := cfg.AdapterConfig.AgentPromptsDir
	if promptsDir == "" {
		return nil, nil
	}

	dir, err := os.ReadDir(promptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []AgentDefinition
	for _, entry := range dir {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))]
		def, err := loadFromLegacyDir(name, cfg)
		if err != nil {
			continue
		}
		result = append(result, def)
	}
	return result, nil
}

// ValidatePipelineAgents warns if any pipeline stage references an agent
// that cannot be found via the loader. It does not hard-fail — it logs a
// warning and returns nil.
func ValidatePipelineAgents(pipeline *domain.Pipeline, cfg domain.SynapseConfig) {
	agents, err := ListAgents(cfg)
	if err != nil {
		return
	}
	known := make(map[string]bool, len(agents))
	for _, a := range agents {
		known[a.Name] = true
	}
	for _, stage := range pipeline.Stages {
		if !known[stage.Agent] {
			fmt.Printf("WARNING: pipeline %q references unknown agent %q in stage %q\n",
				pipeline.Name, stage.Agent, stage.ID)
		}
	}
}
