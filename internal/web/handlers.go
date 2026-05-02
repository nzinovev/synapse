package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var mdRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

// GET /
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		http.Error(w, "Failed to load tasks", http.StatusInternalServerError)
		return
	}

	pipelines, _ := domain.ListPipelines(s.cfg.PipelinesDir)
	if pipelines == nil {
		pipelines, _ = domain.ListBundledPipelines()
	}

	grouped := groupTasksByDir(tasks)

	defaultProjectDir := ""
	for dir := range grouped {
		defaultProjectDir = dir
		break
	}
	if defaultProjectDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			defaultProjectDir = home
		}
	}

	s.templates.ExecuteTemplate(w, "index.html", map[string]any{
		"GroupedTasks":      grouped,
		"Pipelines":         pipelines,
		"AdapterNames":      s.adapterNames,
		"DefaultAdapter":    s.defaultAdapter,
		"DefaultProjectDir": defaultProjectDir,
		"SandboxMode":       s.cfg.AdapterConfig.SandboxMode,
	})
}

// GET /{repo}
func (s *Server) handleRepoTasks(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	workingDir, err := url.QueryUnescape(repo)
	if err != nil {
		http.Error(w, "Invalid repo path", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		http.Error(w, "Failed to load tasks", http.StatusInternalServerError)
		return
	}

	var repoTasks []*domain.Task
	for _, t := range tasks {
		if t.WorkingDir == workingDir {
			repoTasks = append(repoTasks, t)
		}
	}

	seenStatus := make(map[string]bool)
	var uniqueStatuses []string
	for _, t := range repoTasks {
		s := string(t.Status)
		if !seenStatus[s] {
			seenStatus[s] = true
			uniqueStatuses = append(uniqueStatuses, s)
		}
	}

	pipelines, _ := domain.ListPipelines(s.cfg.PipelinesDir)
	if pipelines == nil {
		pipelines, _ = domain.ListBundledPipelines()
	}

	s.templates.ExecuteTemplate(w, "tasks.html", map[string]any{
		"RepoName":          lastSegment(workingDir),
		"RepoDir":           workingDir,
		"Tasks":             repoTasks,
		"UniqueStatuses":    uniqueStatuses,
		"Pipelines":         pipelines,
		"AdapterNames":      s.adapterNames,
		"DefaultAdapter":    s.defaultAdapter,
		"BreadcrumbRepo":    lastSegment(workingDir),
		"BreadcrumbRepoURL": url.QueryEscape(workingDir),
	})
}

// GET /tasks/{id}
func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	pipeline := s.resolvePipeline(task)
	events, _ := s.store.LoadEvents(ctx, taskID)

	var latestContainerInfo *domain.ContainerInfo
	for i := len(task.Runs) - 1; i >= 0; i-- {
		if task.Runs[i].AgentResult != nil && task.Runs[i].AgentResult.ContainerInfo != nil {
			latestContainerInfo = task.Runs[i].AgentResult.ContainerInfo
			break
		}
	}

	sandboxNetworkPolicy := string(s.cfg.AdapterConfig.DockerConfig.NetworkPolicy)
	if latestContainerInfo != nil {
		sandboxNetworkPolicy = string(latestContainerInfo.NetworkPolicy)
	}

	s.templates.ExecuteTemplate(w, "task.html", map[string]any{
		"Task":                 task,
		"Pipeline":             pipeline,
		"Events":               events,
		"WorkingDir":           task.WorkingDir,
		"AdapterNames":         s.adapterNames,
		"DefaultAdapter":       s.defaultAdapter,
		"BreadcrumbRepo":       lastSegment(task.WorkingDir),
		"BreadcrumbRepoURL":    url.QueryEscape(task.WorkingDir),
		"BreadcrumbTaskID":     task.ID,
		"SandboxMode":          task.SandboxMode,
		"SandboxNetworkPolicy": sandboxNetworkPolicy,
		"ContainerInfo":        latestContainerInfo,
	})
}

// GET /tasks/{id}/stage-card
func (s *Server) handleStageCard(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	pipeline := s.resolvePipeline(task)

	s.templates.ExecuteTemplate(w, "stage_card.html", map[string]any{
		"Task":           task,
		"Pipeline":       pipeline,
		"CurrentGate":    currentGate(task, pipeline),
		"AdapterNames":   s.adapterNames,
		"DefaultAdapter": s.defaultAdapter,
	})
}

// POST /tasks/{id}/approve
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if task.Status != domain.StatusAwaitingGate {
		http.Error(w, fmt.Sprintf("Task is not awaiting a gate (status=%s)", task.Status), http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	if adapterName := r.FormValue("adapter"); adapterName != "" && adapterName != task.Adapter {
		task.Adapter = adapterName
		s.store.SaveTask(ctx, task)
	}

	if _, err := s.engine.PrepareApprove(ctx, taskID); err != nil {
		log.Printf("web: prepare approve: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.qStore.Enqueue(ctx, taskID, "run", nil); err != nil {
		log.Printf("web: enqueue approve: %v", err)
	}

	task, _ = s.store.LoadTask(ctx, taskID)
	pipeline := s.resolvePipeline(task)

	s.templates.ExecuteTemplate(w, "stage_card.html", map[string]any{
		"Task":           task,
		"Pipeline":       pipeline,
		"CurrentGate":    currentGate(task, pipeline),
		"AdapterNames":   s.adapterNames,
		"DefaultAdapter": s.defaultAdapter,
	})
}

// POST /tasks/{id}/reject
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	feedback := strings.TrimSpace(r.FormValue("feedback"))
	if feedback == "" {
		http.Error(w, "Feedback is required for rejection.", http.StatusBadRequest)
		return
	}

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if task.Status != domain.StatusAwaitingGate {
		http.Error(w, fmt.Sprintf("Task is not awaiting a gate (status=%s)", task.Status), http.StatusBadRequest)
		return
	}

	if _, err := s.engine.PrepareReject(ctx, taskID, feedback); err != nil {
		log.Printf("web: prepare reject: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.qStore.Enqueue(ctx, taskID, "run", nil); err != nil {
		log.Printf("web: enqueue reject: %v", err)
	}

	task, _ = s.store.LoadTask(ctx, taskID)
	pipeline := s.resolvePipeline(task)

	s.templates.ExecuteTemplate(w, "stage_card.html", map[string]any{
		"Task":           task,
		"Pipeline":       pipeline,
		"CurrentGate":    currentGate(task, pipeline),
		"AdapterNames":   s.adapterNames,
		"DefaultAdapter": s.defaultAdapter,
	})
}

// POST /tasks/{id}/cancel
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if task.Status != domain.StatusAwaitingGate {
		http.Error(w, fmt.Sprintf("Task is not awaiting a gate (status=%s)", task.Status), http.StatusBadRequest)
		return
	}

	if _, err := s.engine.Cancel(ctx, taskID); err != nil {
		log.Printf("web: cancel: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	task, _ = s.store.LoadTask(ctx, taskID)
	pipeline := s.resolvePipeline(task)

	s.templates.ExecuteTemplate(w, "stage_card.html", map[string]any{
		"Task":           task,
		"Pipeline":       pipeline,
		"CurrentGate":    currentGate(task, pipeline),
		"AdapterNames":   s.adapterNames,
		"DefaultAdapter": s.defaultAdapter,
	})
}

// POST /tasks/{id}/retry
func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if task.Status != domain.StatusBlocked && task.Status != domain.StatusEscalated {
		http.Error(w, fmt.Sprintf("Task is not BLOCKED or ESCALATED (status=%s)", task.Status), http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	if adapterName := r.FormValue("adapter"); adapterName != "" && adapterName != task.Adapter {
		task.Adapter = adapterName
		s.store.SaveTask(ctx, task)
	}

	if _, err := s.engine.PrepareRetry(ctx, taskID); err != nil {
		log.Printf("web: prepare retry: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.qStore.Enqueue(ctx, taskID, "run", nil); err != nil {
		log.Printf("web: enqueue retry: %v", err)
	}

	task, _ = s.store.LoadTask(ctx, taskID)
	pipeline := s.resolvePipeline(task)

	s.templates.ExecuteTemplate(w, "stage_card.html", map[string]any{
		"Task":           task,
		"Pipeline":       pipeline,
		"CurrentGate":    currentGate(task, pipeline),
		"AdapterNames":   s.adapterNames,
		"DefaultAdapter": s.defaultAdapter,
	})
}

// GET /tasks/{id}/artifact?path=<path>
func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	artifactPath := r.URL.Query().Get("path")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	absPath, err := filepath.Abs(artifactPath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	wd, err := filepath.Abs(task.WorkingDir)
	if err != nil {
		http.Error(w, "Invalid working dir", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(absPath, wd+string(filepath.Separator)) && absPath != wd {
		http.Error(w, "Artifact path is outside the task's working directory.", http.StatusForbidden)
		return
	}

	data, err := readFileSafe(absPath)
	if err != nil {
		http.Error(w, "Artifact file not found", http.StatusNotFound)
		return
	}

	s.templates.ExecuteTemplate(w, "artifact.html", map[string]any{
		"Path":     artifactPath,
		"Filename": filepath.Base(absPath),
		"Content":  string(data),
		"TaskID":   taskID,
	})
}

// GET /tasks/{id}/artifact-modal?path=<path>
func (s *Server) handleArtifactModal(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	artifactPath := r.URL.Query().Get("path")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	absPath, err := filepath.Abs(artifactPath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	wd, err := filepath.Abs(task.WorkingDir)
	if err != nil {
		http.Error(w, "Invalid working dir", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(absPath, wd+string(filepath.Separator)) && absPath != wd {
		http.Error(w, "Artifact path is outside the task's working directory.", http.StatusForbidden)
		return
	}

	data, err := readFileSafe(absPath)
	if err != nil {
		http.Error(w, "Artifact file not found", http.StatusNotFound)
		return
	}

	contentStr := string(data)
	isMarkdown := strings.HasSuffix(strings.ToLower(absPath), ".md")

	tmplData := map[string]any{
		"Path":       artifactPath,
		"Filename":   filepath.Base(absPath),
		"Content":    contentStr,
		"TaskID":     taskID,
		"IsMarkdown": isMarkdown,
	}
	if isMarkdown {
		tmplData["ContentHTML"] = renderMarkdown(contentStr)
	}

	s.templates.ExecuteTemplate(w, "artifact_modal.html", tmplData)
}

// GET /tasks/{id}/artifacts-card
func (s *Server) handleArtifactsCard(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	task, err := s.store.LoadTask(r.Context(), taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	s.templates.ExecuteTemplate(w, "artifacts_card.html", map[string]any{
		"Task": task,
	})
}

// GET /tasks/{id}/stage-logs/{stage}
func (s *Server) handleStageLogs(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	stageID := r.PathValue("stage")
	ctx := r.Context()

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	var stageRuns []domain.StageRun
	for _, run := range task.Runs {
		if run.StageID == stageID {
			stageRuns = append(stageRuns, run)
		}
	}

	type attemptInfo struct {
		Attempt    int
		Trigger    domain.RunTrigger
		StartedAt  time.Time
		FinishedAt *time.Time
		ExitCode   *int
		Duration   float64
		Stdout     string
		Stderr     string
		ContainerInfo *domain.ContainerInfo
	}

	var attempts []attemptInfo
	for _, run := range stageRuns {
		info := attemptInfo{
			Attempt:    run.Attempt,
			Trigger:    run.Trigger,
			StartedAt:  run.StartedAt,
			FinishedAt: run.FinishedAt,
		}
		if run.AgentResult != nil {
			info.ExitCode = run.AgentResult.ExitCode
			info.Duration = run.AgentResult.DurationSeconds
			info.Stdout = run.AgentResult.Stdout
			info.Stderr = run.AgentResult.Stderr
			info.ContainerInfo = run.AgentResult.ContainerInfo
		}
		attempts = append(attempts, info)
	}

	s.templates.ExecuteTemplate(w, "stage_logs.html", map[string]any{
		"TaskID":   taskID,
		"StageID":  stageID,
		"Attempts": attempts,
	})
}

// POST /tasks
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="form-error">Invalid form data.</span>`)
		return
	}

	pipelineName := strings.TrimSpace(r.FormValue("pipeline_name"))
	taskNumber := strings.TrimSpace(r.FormValue("task_number"))
	description := strings.TrimSpace(r.FormValue("description"))
	workingDir := strings.TrimSpace(r.FormValue("working_dir"))
	adapterName := strings.TrimSpace(r.FormValue("adapter"))

	if pipelineName == "" || description == "" || taskNumber == "" || workingDir == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="form-error">All fields are required.</span>`)
		return
	}

	if adapterName == "fake" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="form-error">The fake adapter is not available for user selection.</span>`)
		return
	}
	if adapterName != "" {
		valid := false
		for _, n := range s.adapterNames {
			if n == adapterName {
				valid = true
				break
			}
		}
		if !valid {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="form-error">Unknown adapter: %s</span>`, adapterName)
			return
		}
	}

	pipeline, err := domain.ResolvePipeline(pipelineName, s.cfg.PipelinesDir)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="form-error">Pipeline not found: %s</span>`, pipelineName)
		return
	}

	ctx := r.Context()
	desc := fmt.Sprintf("Task number: %s\n\n%s", taskNumber, description)
	firstStage := pipeline.Stages[0]
	now := time.Now().UTC()

	sandboxMode := domain.SandboxMode(strings.TrimSpace(r.FormValue("sandbox_mode")))
	if sandboxMode == "" {
		sandboxMode = s.cfg.AdapterConfig.SandboxMode
	}

	var task *domain.Task
	var taskID string
	for suffix := 0; suffix <= 9; suffix++ {
		taskID = domain.GenerateTaskID(taskNumber, now, suffix)
		task = &domain.Task{
			ID:             taskID,
			PipelineName:   pipelineName,
			Description:    desc,
			WorkingDir:     workingDir,
			CurrentStageID: firstStage.ID,
			Status:         domain.StatusRunning,
			Runs:           []domain.StageRun{},
			Artifacts:      map[string][]string{},
			FixCycleCount:  0,
			PRIndex:        1,
			Adapter:        adapterName,
			SandboxMode:    sandboxMode,
		}
		if err := s.store.CreateTask(ctx, task); err != nil {
			if domain.IsDuplicateIDError(err) {
				continue
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="form-error">Failed to create task: %v</span>`, err)
			return
		}
		break
	}
	if task == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="form-error">Failed to create task: could not generate unique ID</span>`)
		return
	}

	if err := s.qStore.Enqueue(ctx, taskID, "run", nil); err != nil {
		log.Printf("web: enqueue run: %v", err)
	}

	w.Header().Set("HX-Redirect", fmt.Sprintf("/tasks/%s", taskID))
	w.WriteHeader(http.StatusOK)
}

// GET /api/browse?path=...
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || !filepath.IsAbs(path) {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		path = home
	}
	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	if err != nil {
		home, _ := os.UserHomeDir()
		path = home
		entries, _ = os.ReadDir(path)
	}

	type dirEntry struct {
		Name string
		Path string
	}
	var dirs []dirEntry
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, dirEntry{
				Name: e.Name(),
				Path: filepath.Join(path, e.Name()),
			})
		}
	}

	parent := filepath.Dir(path)
	s.templates.ExecuteTemplate(w, "browse.html", map[string]any{
		"Path":   path,
		"Parent": parent,
		"Dirs":   dirs,
		"AtRoot": path == parent,
	})
}

// POST /tasks/{id}/answer
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	answers := strings.TrimSpace(r.FormValue("feedback"))
	if answers == "" {
		http.Error(w, "Answers text is required.", http.StatusBadRequest)
		return
	}

	task, err := s.store.LoadTask(ctx, taskID)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if task.Status != domain.StatusAwaitingGate {
		http.Error(w, fmt.Sprintf("Task is not awaiting a gate (status=%s)", task.Status), http.StatusBadRequest)
		return
	}

	if _, err := s.engine.PrepareAnswer(ctx, taskID, answers); err != nil {
		log.Printf("web: prepare answer: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.qStore.Enqueue(ctx, taskID, "run", nil); err != nil {
		log.Printf("web: enqueue answer: %v", err)
	}

	task, _ = s.store.LoadTask(ctx, taskID)
	pipeline := s.resolvePipeline(task)

	s.templates.ExecuteTemplate(w, "stage_card.html", map[string]any{
		"Task":           task,
		"Pipeline":       pipeline,
		"CurrentGate":    currentGate(task, pipeline),
		"AdapterNames":   s.adapterNames,
		"DefaultAdapter": s.defaultAdapter,
	})
}

func currentGate(task *domain.Task, pipeline *domain.Pipeline) domain.Gate {
	if pipeline == nil {
		return ""
	}
	for _, s := range pipeline.Stages {
		if s.ID == task.CurrentStageID {
			return s.Gate
		}
	}
	return ""
}

// Helpers

func groupTasksByDir(tasks []*domain.Task) map[string][]*domain.Task {
	grouped := make(map[string][]*domain.Task)
	for _, t := range tasks {
		grouped[t.WorkingDir] = append(grouped[t.WorkingDir], t)
	}
	return grouped
}

func readFileSafe(path string) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}
