package adapter

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
)

const (
	timeoutSeconds = 20 * 60
	maxOutputBytes = 50 * 1024
)

const promptTemplate = `# Task
%s

# Orchestrator routing (stage contract — follow this; full detail in docs/agent-routing.md)
%s

# Context from prior stages
%s

# Previous attempt feedback (if retry)
%s

# Previous attempt output (if retry)
%s

# Your working directory
%s

Do the work described in your system prompt. When done, print a final line:
SYNAPSE_AGENT_DONE: <summary>
on its own line. Return nonzero exit code on unrecoverable failure.
`

func truncateOutput(text string, maxBytes int) string {
	encoded := []byte(text)
	if len(encoded) <= maxBytes {
		return text
	}
	truncated := encoded[:maxBytes]
	return string(truncated) + "\n[... truncated ...]"
}

func BuildTaskPrompt(
	taskDescription, workingDir string,
	contextArtifacts map[string][]string,
	rejectionFeedback *string,
	openQuestionAnswers *string,
	stageID string,
	gate domain.Gate,
	pipelineName, agentName string,
	fixCycleCount, prIndex int,
	previousStdout, previousStderr *string,
) string {
	var contextSection string
	if len(contextArtifacts) > 0 {
		var lines []string
		for artStageID, paths := range contextArtifacts {
			for _, p := range paths {
				lines = append(lines, fmt.Sprintf("See: %s  (from stage: %s)", p, artStageID))
			}
		}
		contextSection = strings.Join(lines, "\n")
	} else {
		contextSection = "N/A"
	}

	routingSection := FormatRoutingBlock(stageID, gate, agentName, pipelineName, fixCycleCount, prIndex)

	var previousOutputSection string
	if previousStdout != nil || previousStderr != nil {
		var parts []string
		if previousStdout != nil {
			parts = append(parts, fmt.Sprintf("## stdout\n%s", *previousStdout))
		}
		if previousStderr != nil {
			parts = append(parts, fmt.Sprintf("## stderr\n%s", *previousStderr))
		}
		previousOutputSection = strings.Join(parts, "\n\n")
	} else {
		previousOutputSection = "N/A"
	}

	var feedback string
	if rejectionFeedback != nil {
		feedback = *rejectionFeedback
	} else if openQuestionAnswers != nil {
		feedback = fmt.Sprintf("The user has provided answers to the open questions raised in your previous output.\nPlease address these answers and finalize your work — this is not a rejection.\n\n%s", *openQuestionAnswers)
	} else {
		feedback = "N/A"
	}

	return fmt.Sprintf(promptTemplate,
		taskDescription,
		routingSection,
		contextSection,
		feedback,
		previousOutputSection,
		workingDir,
	)
}

func RunCLICommand(ctx context.Context, cmd []string, workingDir, stageWorkdir string) domain.AgentResult {
	start := time.Now()
	os.MkdirAll(stageWorkdir, 0o755)

	exe := "(empty)"
	if len(cmd) > 0 {
		exe = cmd[0]
	}
	log.Printf("subprocess start exe=%s argv_len=%d cwd=%s logs=%s", exe, len(cmd), workingDir, stageWorkdir)

	timeout := time.Duration(timeoutSeconds) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(cmdCtx, cmd[0], cmd[1:]...)
	command.Dir = workingDir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()

	duration := time.Since(start).Seconds()

	if cmdCtx.Err() == context.DeadlineExceeded {
		stdoutStr := truncateOutput(stdout.String(), maxOutputBytes)
		stderrStr := truncateOutput(stderr.String()+"\n[SYNAPSE: agent timed out after 1200s — hard-killed]", maxOutputBytes)
		os.WriteFile(filepath.Join(stageWorkdir, "stdout.log"), []byte(stdoutStr), 0o644)
		os.WriteFile(filepath.Join(stageWorkdir, "stderr.log"), []byte(stderrStr), 0o644)
		log.Printf("subprocess timeout after %ds exe=%s cwd=%s duration_s=%.2f", timeoutSeconds, exe, workingDir, duration)
		return domain.AgentResult{
			Success:          false,
			Stdout:           stdoutStr,
			Stderr:           stderrStr,
			ArtifactsCreated: nil,
			DurationSeconds:  duration,
			ExitCode:         nil,
		}
	}

	stdoutStr := truncateOutput(stdout.String(), maxOutputBytes)
	stderrStr := truncateOutput(stderr.String(), maxOutputBytes)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
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

	os.WriteFile(filepath.Join(stageWorkdir, "stdout.log"), []byte(stdoutStr), 0o644)
	os.WriteFile(filepath.Join(stageWorkdir, "stderr.log"), []byte(stderrStr), 0o644)

	log.Printf("subprocess done exe=%s exit_code=%d duration_s=%.2f done_marker=%v cwd=%s",
		exe, exitCode, duration, doneMarkerFound, workingDir)

	return domain.AgentResult{
		Success:          exitCode == 0,
		Stdout:           stdoutStr,
		Stderr:           stderrStr,
		ArtifactsCreated: nil,
		DurationSeconds:  duration,
		ExitCode:         &exitCode,
	}
}
