package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
)

var agentArtifacts = map[string]string{
	"spec-writer":         "docs/specs/%s.md",
	"adr-architect":       "docs/adr/0001-%s.md",
	"ui-designer":         "docs/ui-specs/%s.md",
	"feature-implementer": "docs/handoff/%s-handoff.md",
	"fix-implementer":     "docs/handoff/%s-fix-handoff.md",
	"spec-reviewer":       "docs/reviews/%s-review.md",
}

var stubContent = map[string]string{
	"spec-writer":         "# Spec: %s\n\n> Auto-generated stub by FakeAdapter.\n\nTODO: fill in spec.\n",
	"adr-architect":       "# ADR 0001: %s\n\n> Auto-generated stub by FakeAdapter.\n\n## Status\n\nProposed\n",
	"ui-designer":         "# UI Spec: %s\n\n> Auto-generated stub by FakeAdapter.\n\n## Components\n\nTBD\n",
	"feature-implementer": "# Handoff: %s\n\nFake implementation stub.\n",
	"fix-implementer":     "# Fix Handoff: %s\n\nFake fix stub.\n",
	"spec-reviewer":       "# Review: %s\n\n> Auto-generated stub by FakeAdapter.\n\n**Verdict:** APPROVED\n\nNo issues found.\n",
}

var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(text string) string {
	if len(text) > 50 {
		text = text[:50]
	}
	text = strings.ToLower(text)
	text = slugifyRe.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	return text
}

type FakeAdapter struct {
	AgentPromptsDir string
	ShouldFail      bool
}

func (f *FakeAdapter) Name() string {
	return "fake"
}

func (f *FakeAdapter) Invoke(ctx context.Context, params domain.InvokeParams) (domain.AgentResult, error) {
	start := time.Now()

	if f.AgentPromptsDir != "" {
		promptFile := filepath.Join(f.AgentPromptsDir, params.AgentName+".md")
		if _, err := os.Stat(promptFile); err != nil {
			return domain.AgentResult{}, fmt.Errorf("agent prompt file not found: %s: %w", promptFile, err)
		}
	}

	if err := os.MkdirAll(params.StageWorkdir, 0o755); err != nil {
		return domain.AgentResult{}, fmt.Errorf("create stage workdir: %w", err)
	}

	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return domain.AgentResult{}, ctx.Err()
	}

	if f.ShouldFail {
		msg := fmt.Sprintf("FakeAdapter configured to fail for agent %q", params.AgentName)
		os.WriteFile(filepath.Join(params.StageWorkdir, "stdout.log"), []byte(""), 0o644)
		os.WriteFile(filepath.Join(params.StageWorkdir, "stderr.log"), []byte(msg), 0o644)
		exitCode := 1
		return domain.AgentResult{
			Success:          false,
			Stdout:           "",
			Stderr:           msg,
			ArtifactsCreated: nil,
			DurationSeconds:  time.Since(start).Seconds(),
			ExitCode:         &exitCode,
		}, nil
	}

	slug := slugify(params.TaskDescription)
	var artifactsCreated []string

	if template, ok := agentArtifacts[params.AgentName]; ok {
		relPath := fmt.Sprintf(template, slug)
		artifactPath := filepath.Join(params.WorkingDir, relPath)
		os.MkdirAll(filepath.Dir(artifactPath), 0o755)

		contentTmpl := "Stub artifact.\n"
		if c, ok := stubContent[params.AgentName]; ok {
			contentTmpl = c
		}
		content := fmt.Sprintf(contentTmpl, slug)
		os.WriteFile(artifactPath, []byte(content), 0o644)
		artifactsCreated = append(artifactsCreated, artifactPath)
	}

	stdoutText := fmt.Sprintf("FakeAdapter: ran %q on task %q\nSYNAPSE_AGENT_DONE: stub run complete\n",
		params.AgentName, slug)
	os.WriteFile(filepath.Join(params.StageWorkdir, "stdout.log"), []byte(stdoutText), 0o644)
	os.WriteFile(filepath.Join(params.StageWorkdir, "stderr.log"), []byte(""), 0o644)

	exitCode := 0
	return domain.AgentResult{
		Success:          true,
		Stdout:           stdoutText,
		Stderr:           "",
		ArtifactsCreated: artifactsCreated,
		DurationSeconds:  time.Since(start).Seconds(),
		ExitCode:         &exitCode,
	}, nil
}

func RegisterFake(registry *AdapterRegistry) {
	registry.Register("fake", func(cfg domain.AdapterConfig) (AgentAdapter, error) {
		return &FakeAdapter{}, nil
	})
}
