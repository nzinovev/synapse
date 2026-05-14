package engine

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/tool"
)

func snapshotGitStatus(workDir string) (string, error) {
	cmd := exec.Command("git", "-C", workDir, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseGitStatusLines(snapshot string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, line := range strings.Split(snapshot, "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			result[line] = struct{}{}
		}
	}
	return result
}

func diffGitSnapshot(pre, post string) []string {
	preLines := parseGitStatusLines(pre)
	var diff []string
	for _, line := range strings.Split(post, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if _, exists := preLines[line]; !exists {
			diff = append(diff, line)
		}
	}
	return diff
}

func extractPaths(diffLines []string) []string {
	var paths []string
	for _, line := range diffLines {
		if len(line) < 3 {
			continue
		}
		rest := line[3:] // skip "XY " status prefix
		// Rename lines: "old -> new" — extract the destination path only.
		if idx := strings.Index(rest, " -> "); idx >= 0 {
			paths = append(paths, rest[idx+4:])
		} else {
			paths = append(paths, rest)
		}
	}
	return paths
}

func validatePostStageChanges(workDir, preSnapshot string, stage *domain.Stage) error {
	postSnapshot, err := snapshotGitStatus(workDir)
	if err != nil {
		return nil // not a git repo or git unavailable; skip validation
	}

	diffLines := diffGitSnapshot(preSnapshot, postSnapshot)
	paths := extractPaths(diffLines)
	perm := stage.Permissions
	runtime := stage.Runtime

	if !perm.EffectiveAllowWrites(runtime) && len(diffLines) > 0 {
		names := strings.Join(paths, ", ")
		return fmt.Errorf("stage permissions violation: allow_writes=false but %d file(s) were modified: %s", len(paths), names)
	}

	if limit := perm.EffectiveMaxChangedFiles(); limit > 0 && len(paths) > limit {
		return fmt.Errorf("stage permissions violation: max_changed_files=%d exceeded (%d files changed)", limit, len(paths))
	}

	if blocked := perm.EffectiveBlockedPaths(); len(blocked) > 0 {
		for _, p := range paths {
			absPath := filepath.Join(workDir, p)
			if err := tool.CheckPath(absPath, tool.ToolPermission{
				BlockedPaths: blocked,
				WorkspaceDir: workDir,
			}); err != nil {
				return fmt.Errorf("stage permissions violation: file %q matches blocked pattern", p)
			}
		}
	}

	return nil
}
