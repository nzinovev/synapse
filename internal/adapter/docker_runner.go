package adapter

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
)

type DockerRunner struct {
	config domain.DockerConfig
}

func NewDockerRunner(config domain.DockerConfig) *DockerRunner {
	return &DockerRunner{config: config}
}

func (d *DockerRunner) Run(ctx context.Context, params RunnerParams) (domain.AgentResult, error) {
	start := time.Now()

	if err := checkDockerAvailable(); err != nil {
		return domain.AgentResult{}, fmt.Errorf("docker not available: %w", err)
	}

	image := resolveAgentImage(params.AgentName)
	if image == "" {
		return domain.AgentResult{}, fmt.Errorf("no image available for agent: %s", params.AgentName)
	}

	containerName := fmt.Sprintf("synapse-%s-%s-%d", params.StageID, params.TaskID, time.Now().UnixMilli())

	// Build docker create arguments.
	createArgs := d.buildCreateArgs(containerName, image, params)

	log.Printf("docker create name=%s image=%s workdir=%s", containerName, image, params.WorkingDir)

	// Step 1: docker create
	var createOut bytes.Buffer
	createCmd := exec.CommandContext(ctx, "docker", createArgs...)
	createCmd.Stderr = &createOut
	createOutput, err := createCmd.Output()
	if err != nil {
		return domain.AgentResult{}, fmt.Errorf("docker create failed: %w\n%s", err, createOut.String())
	}

	containerID := strings.TrimSpace(string(createOutput))

	// Ensure cleanup regardless of outcome.
	defer func() {
		rmCmd := exec.Command("docker", "rm", "-f", containerID)
		rmCmd.Run()
	}()

	// Step 2: docker start -a (attach to capture output)
	timeout := time.Duration(d.config.TimeoutSeconds) * time.Second
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startCmd := exec.CommandContext(timeoutCtx, "docker", "start", "-a", containerID)
	var stdout, stderr bytes.Buffer
	startCmd.Stdout = &stdout
	startCmd.Stderr = &stderr

	log.Printf("docker start container=%s", containerID[:12])
	err = startCmd.Run()

	duration := time.Since(start).Seconds()

	stdoutStr := truncateOutput(stdout.String(), maxOutputBytes)
	stderrStr := truncateOutput(stderr.String(), maxOutputBytes)

	// Handle timeout.
	if timeoutCtx.Err() == context.DeadlineExceeded {
		killCmd := exec.Command("docker", "kill", containerID)
		killCmd.Run()
		stderrStr += "\n[SYNAPSE: container timed out — hard-killed]"
		os.WriteFile(filepath.Join(params.StageWorkdir, "stdout.log"), []byte(stdoutStr), 0o644)
		os.WriteFile(filepath.Join(params.StageWorkdir, "stderr.log"), []byte(stderrStr), 0o644)
		log.Printf("docker timeout container=%s duration_s=%.2f", containerID[:12], duration)
		return domain.AgentResult{
			Success:         false,
			Stdout:          stdoutStr,
			Stderr:          stderrStr,
			DurationSeconds: duration,
		}, nil
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return domain.AgentResult{}, fmt.Errorf("docker start failed: %w", err)
		}
	}

	doneMarkerFound := false
	for _, line := range strings.Split(stdoutStr, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "SYNAPSE_AGENT_DONE:") {
			doneMarkerFound = true
			break
		}
	}

	if exitCode == 0 && !doneMarkerFound {
		stderrStr += "\n[SYNAPSE WARNING: SYNAPSE_AGENT_DONE marker not found in agent output]"
	}

	os.WriteFile(filepath.Join(params.StageWorkdir, "stdout.log"), []byte(stdoutStr), 0o644)
	os.WriteFile(filepath.Join(params.StageWorkdir, "stderr.log"), []byte(stderrStr), 0o644)

	log.Printf("docker done container=%s exit_code=%d duration_s=%.2f done_marker=%v",
		containerID[:12], exitCode, duration, doneMarkerFound)

	containerInfo := &domain.ContainerInfo{
		ContainerID:   containerID,
		Image:         image,
		NetworkPolicy: d.config.NetworkPolicy,
		CPULimit:      d.config.CPULimit,
		MemoryLimitMB: d.config.MemoryLimitMB,
	}

	return domain.AgentResult{
		Success:          exitCode == 0,
		Stdout:           stdoutStr,
		Stderr:           stderrStr,
		DurationSeconds:  duration,
		ExitCode:         &exitCode,
		ContainerInfo:    containerInfo,
	}, nil
}

func (d *DockerRunner) buildCreateArgs(containerName, image string, params RunnerParams) []string {
	mountPath := d.config.ContainerMountPath
	if mountPath == "" {
		mountPath = "/mount"
	}

	var args []string
	args = append(args, "create", "--name", containerName)

	// Project mount — add :cached on macOS for VirtioFS performance.
	projectMount := fmt.Sprintf("%s:%s", params.WorkingDir, mountPath)
	if runtime.GOOS == "darwin" {
		projectMount += ":cached"
	}
	args = append(args, "-v", projectMount)

	// Stage workdir mount for logs.
	args = append(args, "-v", fmt.Sprintf("%s:/workdir", params.StageWorkdir))

	// Prompt files mount (read-only).
	if params.PromptDir != "" {
		args = append(args, "-v", fmt.Sprintf("%s:/prompts:ro", params.PromptDir))
	}

	args = append(args, "--workdir", mountPath)

	// Resource limits.
	if d.config.CPULimit > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", d.config.CPULimit))
	}
	if d.config.MemoryLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", d.config.MemoryLimitMB))
	}

	// Network policy.
	switch d.config.NetworkPolicy {
	case domain.NetworkNone:
		args = append(args, "--network", "none")
	// NetworkRestricted and NetworkFull both use default bridge in V1.
	// Endpoint allowlisting is deferred to a future enhancement.
	}

	// Environment variables: API keys from host environment.
	for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		if val := os.Getenv(key); val != "" {
			args = append(args, "-e", fmt.Sprintf("%s=%s", key, val))
		}
	}

	// Synapse metadata env vars.
	if params.TaskID != "" {
		args = append(args, "-e", fmt.Sprintf("SYNAPSE_TASK_ID=%s", params.TaskID))
	}
	if params.StageID != "" {
		args = append(args, "-e", fmt.Sprintf("SYNAPSE_STAGE_ID=%s", params.StageID))
	}
	if params.PipelineName != "" {
		args = append(args, "-e", fmt.Sprintf("SYNAPSE_PIPELINE_NAME=%s", params.PipelineName))
	}
	args = append(args, "-e", fmt.Sprintf("SYNAPSE_AGENT=%s", params.AgentName))

	// Extra env vars from params.
	for key, val := range params.EnvVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, val))
	}

	// Image and command.
	args = append(args, image)
	args = append(args, params.Command...)

	return args
}

func checkDockerAvailable() error {
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker daemon not running or docker not installed")
	}
	return nil
}

func resolveAgentImage(agentName string) string {
	switch agentName {
	case "claude_cli":
		return "synapse/agent-claude:latest"
	case "cursor_cli":
		return "synapse/agent-cursor:latest"
	default:
		return ""
	}
}
