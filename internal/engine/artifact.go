package engine

import (
	"os"
	"regexp"
	"strings"
)

var (
	statusHeadingRe = regexp.MustCompile(`(?m)(## Status\s*\n+)\S[^\n]*`)
	statusFieldRe   = regexp.MustCompile(`(?m)^(Status:\s*)\S.*$`)
)

func StampArtifactApproved(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := string(data)

	updated := statusHeadingRe.ReplaceAllStringFunc(text, func(match string) string {
		return statusHeadingRe.ReplaceAllString(match, "${1}Approved")
	})
	updated = statusHeadingRe.ReplaceAllString(updated, "${1}Approved")

	if updated == text {
		updated = statusFieldRe.ReplaceAllString(text, "${1}Approved")
	}

	if updated == text {
		if !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		updated += "\n## Status\n\nApproved\n"
	}

	os.WriteFile(path, []byte(updated), 0o644)
}
