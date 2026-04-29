package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type TemplateCache struct {
	pages map[string]*template.Template
}

var funcMap template.FuncMap

func init() {
	funcMap = template.FuncMap{
		"fmtTime":          fmtTime,
		"fmtTimeFull":      fmtTimeFull,
		"statusColor":      statusColor,
		"lastSegment":      lastSegment,
		"fmtDuration":      fmtDuration,
		"stageRunsFor":     stageRunsFor,
		"reverse":          reverseSlice,
		"urlq":             url.QueryEscape,
		"joinPath":         filepath.Join,
		"pipelineProgress": pipelineProgress,
		"derefInt": func(p *int) string {
			if p == nil {
				return "—"
			}
			return fmt.Sprintf("%d", *p)
		},
		"truncate": func(s string, n int) string {
			if len(s) > n {
				return s[:n] + "..."
			}
			return s
		},
		"countByStatus": func(tasks []*domain.Task, status string) int {
			n := 0
			for _, t := range tasks {
				if string(t.Status) == status {
					n++
				}
			}
			return n
		},
		"countFailed": func(tasks []*domain.Task) int {
			n := 0
			for _, t := range tasks {
				if t.Status == domain.StatusBlocked || t.Status == domain.StatusEscalated {
					n++
				}
			}
			return n
		},
	}
}

func LoadTemplates() (*TemplateCache, error) {
	// Parse the base layout and fragment templates (shared by all pages).
	base := template.New("").Funcs(funcMap)
	base = template.Must(base.ParseFS(templateFS,
		"templates/base.html",
		"templates/stage_card.html",
		"templates/stage_logs.html",
		"templates/artifact_modal.html",
		"templates/browse.html",
		"templates/artifacts_card.html",
	))

	// Each page gets its own clone of the base + fragments so
	// define/block overrides don't collide across pages.
	pageFiles := []string{
		"templates/index.html",
		"templates/tasks.html",
		"templates/task.html",
		"templates/artifact.html",
		"templates/run_logs.html",
		"templates/new_task.html",
	}

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, f := range pageFiles {
		clone := template.Must(base.Clone())
		clone = template.Must(clone.ParseFS(templateFS, f))
		pages[f] = clone
	}

	return &TemplateCache{pages: pages}, nil
}

func pageName(name string) string {
	return "templates/" + name
}

func (tc *TemplateCache) ExecuteTemplate(w io.Writer, name string, data any) error {
	// For fragment templates (stage_card.html, stage_logs.html),
	// pick any page's template set — they all share the same fragments.
	if name == "stage_card.html" || name == "stage_logs.html" || name == "browse.html" || name == "artifact_modal.html" || name == "artifacts_card.html" {
		for _, tmpl := range tc.pages {
			return tmpl.ExecuteTemplate(w, name, data)
		}
	}

	// For full pages, use the page-specific template set.
	key := pageName(name)
	tmpl, ok := tc.pages[key]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return tmpl.ExecuteTemplate(w, "base.html", data)
}

func fmtTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04")
}

func fmtTimeFull(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

func fmtDuration(seconds float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", seconds), "0"), ".")
}

func statusColor(status domain.TaskStatus) string {
	colors := map[domain.TaskStatus]string{
		domain.StatusPending:      "pending",
		domain.StatusRunning:      "running",
		domain.StatusAwaitingGate: "awaiting_gate",
		domain.StatusDone:         "done",
		domain.StatusBlocked:      "blocked",
		domain.StatusEscalated:    "escalated",
		domain.StatusCancelled:    "cancelled",
	}
	if c, ok := colors[status]; ok {
		return c
	}
	return "pending"
}

func lastSegment(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func reverseSlice(runs []domain.StageRun) []domain.StageRun {
	result := make([]domain.StageRun, len(runs))
	for i, r := range runs {
		result[len(runs)-1-i] = r
	}
	return result
}

func stageRunsFor(runs []domain.StageRun, stageID string) []domain.StageRun {
	var filtered []domain.StageRun
	for _, r := range runs {
		if r.StageID == stageID {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

var _ fs.FS = staticFS

func pipelineProgress(stages []domain.Stage, currentStageID string) float64 {
	if len(stages) <= 1 {
		return 0
	}
	idx := 0
	for i, s := range stages {
		if s.ID == currentStageID {
			idx = i
			break
		}
	}
	return float64(idx) / float64(len(stages)-1) * 100
}
