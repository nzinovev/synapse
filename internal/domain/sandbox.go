package domain

type SandboxMode string

const (
	SandboxDocker SandboxMode = "docker"
	SandboxHost   SandboxMode = "host"
)

type NetworkPolicy string

const (
	NetworkRestricted NetworkPolicy = "restricted"
	NetworkFull       NetworkPolicy = "full"
	NetworkNone       NetworkPolicy = "none"
)

type SourceMode string

const (
	SourceMount SourceMode = "mount"
)

type DockerConfig struct {
	NetworkPolicy      NetworkPolicy `json:"network_policy"`
	AllowedEndpoints   []string      `json:"allowed_endpoints"`
	CPULimit           float64       `json:"cpu_limit"`
	MemoryLimitMB      int           `json:"memory_limit_mb"`
	TimeoutSeconds     int           `json:"timeout_seconds"`
	SourceMode         SourceMode     `json:"source_mode"`
	ContainerMountPath string        `json:"container_mount_path"`
}

func DefaultDockerConfig() DockerConfig {
	return DockerConfig{
		NetworkPolicy:      NetworkRestricted,
		AllowedEndpoints:   []string{"api.anthropic.com", "api.openai.com", "api.z.ai"},
		CPULimit:           2.0,
		MemoryLimitMB:      2048,
		TimeoutSeconds:     1200,
		SourceMode:         SourceMount,
		ContainerMountPath: "/mount",
	}
}

type ContainerInfo struct {
	ContainerID   string        `json:"container_id"`
	Image         string        `json:"image"`
	NetworkPolicy NetworkPolicy `json:"network_policy"`
	CPULimit      float64       `json:"cpu_limit"`
	MemoryLimitMB int           `json:"memory_limit_mb"`
}
