package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// searchTextTool implements a pure-Go recursive regex search over a directory.
type searchTextTool struct {
	perm ToolPermission
}

// NewSearchTextTool returns a Tool that searches for a regex pattern inside
// files under the given path.
func NewSearchTextTool(perm ToolPermission) Tool {
	return &searchTextTool{perm: perm}
}

func (t *searchTextTool) Name() string { return "search_text" }

func (t *searchTextTool) Description() string {
	return "Search for a regular expression pattern in files within a directory. " +
		"Returns matching lines with file paths and line numbers."
}

func (t *searchTextTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory (or file) to search within.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Whether to search subdirectories recursively. Defaults to false.",
			},
		},
		"required": []string{"path", "pattern"},
	}
}

func (t *searchTextTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	path, err := stringField(input, "path")
	if err != nil {
		return nil, err
	}
	pattern, err := stringField(input, "pattern")
	if err != nil {
		return nil, err
	}

	recursive := false
	if v, ok := input["recursive"]; ok {
		if b, ok := v.(bool); ok {
			recursive = b
		}
	}

	if err := CheckPath(path, t.perm); err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("search_text: invalid pattern %q: %w", pattern, err)
	}

	var matches []map[string]any

	err = walkPath(path, recursive, func(filePath string) error {
		// Skip if file path is not safe (e.g., symlink escaped the workspace
		// during the walk). Best-effort — errors are skipped silently.
		if checkErr := CheckPath(filePath, t.perm); checkErr != nil {
			return nil
		}

		fileMatches, scanErr := scanFile(filePath, re)
		if scanErr != nil {
			// Skip unreadable files (binary, permission denied, etc.).
			return nil
		}
		matches = append(matches, fileMatches...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search_text: %w", err)
	}

	if matches == nil {
		matches = []map[string]any{}
	}

	return map[string]any{"matches": matches}, nil
}

// walkPath calls fn for every regular file under root. If recursive is false,
// only the direct children of root are visited. If root is itself a regular
// file, fn is called once for it.
func walkPath(root string, recursive bool, fn func(string) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return fn(root)
	}

	if recursive {
		return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries
			}
			if !d.IsDir() {
				return fn(path)
			}
			return nil
		})
	}

	// Non-recursive: list direct children only.
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			if fnErr := fn(filepath.Join(root, e.Name())); fnErr != nil {
				return fnErr
			}
		}
	}
	return nil
}

// scanFile reads filePath line by line and returns all lines matching re.
func scanFile(filePath string, re *regexp.Regexp) ([]map[string]any, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []map[string]any
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			results = append(results, map[string]any{
				"file": filePath,
				"line": lineNum,
				"text": strings.TrimRight(line, "\r"),
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
