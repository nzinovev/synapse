package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AdapterConfig struct {
	ClaudeBinary    string            `json:"claude_binary"`
	AgentBinary     string            `json:"agent_binary"`
	AgentPromptsDir string            `json:"agent_prompts_dir"`
	CLIProfile      string            `json:"cli_profile"`
	CursorExtraArgs []string          `json:"cursor_extra_args"`
	ModelTiers      map[string]string `json:"model_tiers"`
	SandboxMode     SandboxMode       `json:"sandbox_mode"`
	DockerConfig    DockerConfig      `json:"docker_config"`
}

type SynapseConfig struct {
	Adapter       string        `json:"adapter"`
	AdapterConfig AdapterConfig `json:"adapter_config"`
	PipelinesDir  string        `json:"pipelines_dir"`
	DBPath        string        `json:"db_path"`
	WorkerCount   int           `json:"worker_count"`
	Host          string        `json:"host"`
	Port          int           `json:"port"`
}

func defaultModelTiers() map[string]string {
	return map[string]string{
		"low":    "haiku",
		"medium": "sonnet",
		"high":   "opus",
	}
}

func defaultCursorModelTiers() map[string]string {
	return map[string]string{
		"low":    "Auto",
		"medium": "Auto",
		"high":   "Premium",
	}
}

func DefaultSynapseConfig() SynapseConfig {
	return SynapseConfig{
		Adapter: "fake",
		AdapterConfig: AdapterConfig{
			ClaudeBinary: "claude",
			AgentBinary:  "agent",
			CLIProfile:   "claude",
			ModelTiers:   defaultModelTiers(),
			SandboxMode:  SandboxDocker,
			DockerConfig: DefaultDockerConfig(),
		},
		DBPath:      defaultDBPath(),
		WorkerCount: 2,
		Host:        "127.0.0.1",
		Port:        8765,
	}
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".synapse/synapse.db"
	}
	return filepath.Join(home, ".synapse", "synapse.db")
}

func GetHubDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".synapse"
	}
	return filepath.Join(home, ".synapse")
}

func FindSynapseDir(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(current, ".synapse")
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no .synapse/ directory found. Run `synapse init` first")
		}
		current = parent
	}
}

func LoadConfig(synapseDir string) (*SynapseConfig, error) {
	configPath := filepath.Join(synapseDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultSynapseConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func LoadConfigGlobal() (*SynapseConfig, error) {
	return LoadConfig(GetHubDir())
}

func FindAndLoadConfig(start string) (*SynapseConfig, error) {
	synapseDir, err := FindSynapseDir(start)
	if err != nil {
		return nil, err
	}
	return LoadConfig(synapseDir)
}

func applyDefaults(cfg *SynapseConfig) {
	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath()
	}
	if cfg.WorkerCount == 0 {
		cfg.WorkerCount = 2
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 8765
	}
	if cfg.AdapterConfig.ClaudeBinary == "" {
		cfg.AdapterConfig.ClaudeBinary = "claude"
	}
	if cfg.AdapterConfig.AgentBinary == "" {
		cfg.AdapterConfig.AgentBinary = "agent"
	}
	if cfg.AdapterConfig.CLIProfile == "" {
		cfg.AdapterConfig.CLIProfile = "claude"
	}
	if cfg.AdapterConfig.ModelTiers == nil {
		if cfg.Adapter == "cursor_cli" {
			cfg.AdapterConfig.ModelTiers = defaultCursorModelTiers()
		} else {
			cfg.AdapterConfig.ModelTiers = defaultModelTiers()
		}
	}
	if cfg.AdapterConfig.SandboxMode == "" {
		cfg.AdapterConfig.SandboxMode = SandboxDocker
	}
	if cfg.AdapterConfig.DockerConfig.NetworkPolicy == "" {
		cfg.AdapterConfig.DockerConfig.NetworkPolicy = NetworkRestricted
	}
	if cfg.AdapterConfig.DockerConfig.CPULimit == 0 {
		cfg.AdapterConfig.DockerConfig.CPULimit = 2.0
	}
	if cfg.AdapterConfig.DockerConfig.MemoryLimitMB == 0 {
		cfg.AdapterConfig.DockerConfig.MemoryLimitMB = 2048
	}
	if cfg.AdapterConfig.DockerConfig.TimeoutSeconds == 0 {
		cfg.AdapterConfig.DockerConfig.TimeoutSeconds = 1200
	}
	if cfg.AdapterConfig.DockerConfig.SourceMode == "" {
		cfg.AdapterConfig.DockerConfig.SourceMode = SourceMount
	}
	if cfg.AdapterConfig.DockerConfig.ContainerMountPath == "" {
		cfg.AdapterConfig.DockerConfig.ContainerMountPath = "/mount"
	}
	if len(cfg.AdapterConfig.DockerConfig.AllowedEndpoints) == 0 {
		cfg.AdapterConfig.DockerConfig.AllowedEndpoints = []string{"api.anthropic.com", "api.openai.com", "api.z.ai"}
	}
}

func (c *SynapseConfig) Save(synapseDir string) error {
	dir := filepath.Join(synapseDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create synapse dir: %w", err)
	}
	configPath := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(configPath, data, 0o644)
}

func (c *SynapseConfig) SaveGlobal() error {
	return c.Save(GetHubDir())
}
