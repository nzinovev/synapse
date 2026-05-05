package agent

// AgentDefinition is the structured representation of an agent loaded from
// agent.yaml or synthesized from a legacy prompt file.
type AgentDefinition struct {
	Name        string       `yaml:"name"`
	Version     string       `yaml:"version"`
	Description string       `yaml:"description"`
	Runtime     string       `yaml:"runtime"`    // "cli" | "native"
	ModelTier   string       `yaml:"model_tier"` // "low" | "medium" | "high"
	Prompt      string       `yaml:"prompt"`     // relative path to prompt.md, or inline text
	Outputs     []OutputSpec `yaml:"outputs"`
	Permissions *AgentPerms  `yaml:"permissions"`
	Source      string       `yaml:"-"` // "user", "legacy", "bundled" — set by loader
}

// OutputSpec describes an expected output artifact produced by an agent.
type OutputSpec struct {
	Glob        string `yaml:"glob"`
	Description string `yaml:"description"`
}

// AgentPerms declares the permission boundary for an agent.
type AgentPerms struct {
	AllowWrites  bool     `yaml:"allow_writes"`
	AllowShell   bool     `yaml:"allow_shell"`
	BlockedPaths []string `yaml:"blocked_paths"`
}
