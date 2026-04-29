package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var openQuestionsHeaderRe = regexp.MustCompile(`(?mi)^##\s+Open Questions\s*$`)

func openQuestionsSectionIsClean(text string) bool {
	loc := openQuestionsHeaderRe.FindStringIndex(text)
	if loc == nil {
		return false
	}
	rest := text[loc[1]:]
	nextSection := regexp.MustCompile(`(?m)^##\s+\S`).FindStringIndex(rest)
	sectionBody := rest
	if nextSection != nil {
		sectionBody = rest[:nextSection[0]]
	}
	return strings.TrimSpace(sectionBody) == "None at this time."
}

func ArtifactsAllowAutoIfClean(artifactPaths []string) bool {
	if len(artifactPaths) == 0 {
		return false
	}

	for _, p := range artifactPaths {
		if strings.EqualFold(filepath.Base(p), "questions.md") {
			return false
		}
	}

	var mdSpecs []string
	for _, p := range artifactPaths {
		if strings.EqualFold(filepath.Ext(p), ".md") {
			mdSpecs = append(mdSpecs, p)
		}
	}
	if len(mdSpecs) == 0 {
		return false
	}

	for _, p := range mdSpecs {
		data, err := os.ReadFile(p)
		if err != nil {
			return false
		}
		if !openQuestionsSectionIsClean(string(data)) {
			return false
		}
	}
	return true
}
