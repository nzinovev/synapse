package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ToolPermission describes what a tool is allowed to do and which paths it
// may access. Each agent constructs a ToolPermission at run time from the
// stage's permissions configuration.
type ToolPermission struct {
	// AllowWrites permits tools that modify the filesystem (write_file).
	AllowWrites bool
	// AllowShell permits tools that execute shell commands (reserved for T13).
	AllowShell bool
	// AllowedPaths is a list of absolute paths the tool may access.
	// An empty slice means the tool is restricted to WorkspaceDir only.
	AllowedPaths []string
	// BlockedPaths is a list of absolute paths or glob patterns that are
	// always denied, even if they fall within WorkspaceDir.
	BlockedPaths []string
	// WorkspaceDir is the root directory. All resolved paths must lie within it.
	WorkspaceDir string
}

// CheckPath validates that path is safe to access under perm.
//
// Rules (in order):
//  1. Resolve path to absolute via filepath.Abs.
//  2. Resolve symlinks via filepath.EvalSymlinks; if that fails because the
//     path does not exist yet (write_file target), use the absolute path.
//     If EvalSymlinks succeeds, the resolved path must stay under WorkspaceDir.
//  3. Check BlockedPaths — glob match against the absolute path; deny if matched.
//  4. Confirm the resolved path is under WorkspaceDir; deny if not.
func CheckPath(path string, perm ToolPermission) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("cannot resolve path %q: %w", path, err)
	}

	// Attempt symlink resolution. If the path does not exist yet (new file),
	// EvalSymlinks will fail — that is acceptable. We only enforce the escape
	// check when the path already exists.
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		// Path exists; check that symlink resolution does not escape workspace.
		if perm.WorkspaceDir != "" && !isUnder(resolved, perm.WorkspaceDir) {
			return fmt.Errorf("path %q resolves outside workspace %q", path, perm.WorkspaceDir)
		}
	} else {
		// Path does not exist yet; use the absolute path for further checks.
		resolved = abs
	}

	// Check blocked paths (glob or prefix match against absolute path).
	for _, blocked := range perm.BlockedPaths {
		if matchBlocked(resolved, blocked) {
			return fmt.Errorf("path %q matches blocked pattern %q", path, blocked)
		}
	}

	// Confirm the path is under the workspace root.
	if perm.WorkspaceDir != "" && !isUnder(resolved, perm.WorkspaceDir) {
		return fmt.Errorf("path %q is outside workspace %q", path, perm.WorkspaceDir)
	}

	return nil
}

// isUnder reports whether child is the same as or nested under parent.
func isUnder(child, parent string) bool {
	// Ensure both are clean absolute paths.
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// matchBlocked reports whether path matches the blocked pattern, which may be
// an absolute path or a glob pattern understood by filepath.Match.
func matchBlocked(path, pattern string) bool {
	// Direct prefix/equality check first.
	if isUnder(path, pattern) {
		return true
	}
	// Glob match.
	matched, err := filepath.Match(pattern, path)
	if err == nil && matched {
		return true
	}
	// Also try matching the base name against the pattern (for simple patterns
	// like ".env" that do not include directory separators).
	if !strings.Contains(pattern, string(filepath.Separator)) {
		base := filepath.Base(path)
		matched, err = filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
	}
	return false
}
