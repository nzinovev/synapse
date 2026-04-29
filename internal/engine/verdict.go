package engine

import (
	"regexp"
)

var verdictRe = regexp.MustCompile(`(?s)\*\*Verdict:\*\*\s*(APPROVED|NEEDS FIXES|BLOCKED)`)

func ParseVerdict(text string) string {
	match := verdictRe.FindStringSubmatch(text)
	if len(match) >= 2 {
		switch match[1] {
		case "APPROVED", "NEEDS FIXES", "BLOCKED":
			return match[1]
		}
	}
	return ""
}
