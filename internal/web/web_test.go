package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/engine"
	"github.com/nzinovev/synapse/internal/queue"
	"github.com/nzinovev/synapse/internal/store"
)

func setupWebTest(t *testing.T) (*Server, string) {
	t.Helper()
	ctx := context.Background()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := adapter.NewRegistry()
	adapter.RegisterFake(reg)
	a, err := reg.Create("fake", domain.AdapterConfig{})
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}

	// Create a pipelines dir with backend.yaml
	pipelinesDir := filepath.Join(tmpDir, "pipelines")
	os.MkdirAll(pipelinesDir, 0o755)
	pipelineYAML := `name: backend
stages:
  - id: spec
    agent: spec-writer
    gate: auto
    produces_glob: "docs/specs/*.md"
  - id: adr
    agent: adr-architect
    gate: human_approval
    produces_glob: "docs/adr/*.md"
  - id: done
    agent: ""
    gate: auto
`
	os.WriteFile(filepath.Join(pipelinesDir, "backend.yaml"), []byte(pipelineYAML), 0o644)

	eng := engine.NewPipelineEngine(s, a, pipelinesDir)
	q := queue.NewSQLiteQueueStore(s.DB())

	cfg := &domain.SynapseConfig{
		PipelinesDir: pipelinesDir,
		Host:         "127.0.0.1",
		Port:         0,
	}

	srv, err := NewServer(cfg, s, eng, q, []string{"claude_cli", "cursor_cli"}, "claude_cli")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	return srv, tmpDir
}

func createTestTask(t *testing.T, s *store.SQLiteStore, status domain.TaskStatus) *domain.Task {
	t.Helper()
	ctx := context.Background()
	task := &domain.Task{
		ID:             fmt.Sprintf("test-task-%d", time.Now().UnixNano()),
		PipelineName:   "backend",
		Description:    "Test task description",
		WorkingDir:     t.TempDir(),
		CurrentStageID: "spec",
		Status:         status,
		Runs:           []domain.StageRun{},
		Artifacts:      map[string][]string{},
		FixCycleCount:  0,
		PRIndex:        1,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func TestHandleIndex(t *testing.T) {
	srv, _ := setupWebTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET / status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No tasks yet") && !strings.Contains(body, "task-table") {
		t.Errorf("GET / body should contain task list or empty state")
	}
}

func TestHandleIndexWithTasks(t *testing.T) {
	srv, _ := setupWebTest(t)
	// Access store directly
	ctx := context.Background()
	task := &domain.Task{
		ID:             "my-test-task",
		PipelineName:   "backend",
		Description:    "A test task",
		WorkingDir:     "/tmp",
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Runs:           []domain.StageRun{},
		Artifacts:      map[string][]string{},
		FixCycleCount:  0,
		PRIndex:        1,
	}
	srv.store.CreateTask(ctx, task)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET / status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "my-test-task") {
		t.Error("response should contain task ID")
	}
}

func TestHandleTaskDetail(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusRunning)

	req := httptest.NewRequest("GET", "/tasks/"+task.ID, nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET /tasks/%s status = %d, want 200", task.ID, w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, task.ID) {
		t.Error("response should contain task ID")
	}
}

func TestHandleTaskDetailNotFound(t *testing.T) {
	srv, _ := setupWebTest(t)

	req := httptest.NewRequest("GET", "/tasks/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("GET /tasks/nonexistent status = %d, want 404", w.Code)
	}
}

func TestHandleStageCard(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusRunning)

	req := httptest.NewRequest("GET", "/tasks/"+task.ID+"/stage-card", nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET stage-card status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "stage-card") {
		t.Error("response should contain stage-card element")
	}
}

func TestHandleApprove(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusAwaitingGate)

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/approve", nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST approve status = %d, want 200", w.Code)
	}
}

func TestHandleApproveWrongStatus(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusRunning)

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/approve", nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("POST approve (wrong status) = %d, want 400", w.Code)
	}
}

func TestHandleReject(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusAwaitingGate)

	form := url.Values{"feedback": []string{"Needs improvement"}}
	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/reject", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST reject status = %d, want 200", w.Code)
	}
}

func TestHandleRejectEmptyFeedback(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusAwaitingGate)

	form := url.Values{"feedback": []string{"  "}}
	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/reject", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("POST reject (empty feedback) = %d, want 400", w.Code)
	}
}

func TestHandleCancel(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusAwaitingGate)

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/cancel", nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST cancel status = %d, want 200", w.Code)
	}

	// Verify task is now cancelled
	updated, _ := srv.store.LoadTask(context.Background(), task.ID)
	if updated.Status != domain.StatusCancelled {
		t.Errorf("task status = %q, want %q", updated.Status, domain.StatusCancelled)
	}
}

func TestHandleRetry(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusBlocked)

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/retry", nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST retry status = %d, want 200", w.Code)
	}
}

func TestHandleRetryWrongStatus(t *testing.T) {
	srv, _ := setupWebTest(t)
	task := createTestTask(t, srv.store, domain.StatusDone)

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/retry", nil)
	req.SetPathValue("id", task.ID)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("POST retry (wrong status) = %d, want 400", w.Code)
	}
}

func TestHandleStageLogs(t *testing.T) {
	srv, _ := setupWebTest(t)
	ctx := context.Background()

	task := &domain.Task{
		ID:             "logs-test-task",
		PipelineName:   "backend",
		Description:    "Test task",
		WorkingDir:     "/tmp",
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Runs: []domain.StageRun{
			{
				StageID:   "spec",
				Attempt:   1,
				Trigger:   domain.TriggerInitial,
				StartedAt: time.Now().UTC(),
				AgentResult: &domain.AgentResult{
					Success:         true,
					Stdout:          "Hello from spec stage",
					Stderr:          "",
					DurationSeconds: 1.5,
					ExitCode:        intPtr(0),
				},
			},
		},
		Artifacts:     map[string][]string{},
		FixCycleCount: 0,
		PRIndex:       1,
	}
	srv.store.CreateTask(ctx, task)

	req := httptest.NewRequest("GET", "/tasks/logs-test-task/stage-logs/spec", nil)
	req.SetPathValue("id", "logs-test-task")
	req.SetPathValue("stage", "spec")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET stage-logs status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Hello from spec stage") {
		t.Error("response should contain stdout content")
	}
}

func TestHandleCreateTask(t *testing.T) {
	srv, tmpDir := setupWebTest(t)
	workDir := filepath.Join(tmpDir, "project")
	os.MkdirAll(workDir, 0o755)

	form := url.Values{
		"pipeline_name": []string{"backend"},
		"task_number":   []string{"JIRA-42"},
		"description":   []string{"Implement feature X"},
		"working_dir":   []string{workDir},
	}
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("POST /tasks status = %d, want 200", w.Code)
	}
	redirect := w.Header().Get("HX-Redirect")
	if !strings.Contains(redirect, "/tasks/") {
		t.Errorf("HX-Redirect = %q, should contain /tasks/", redirect)
	}
}

func TestHandleCreateTaskValidation(t *testing.T) {
	srv, _ := setupWebTest(t)

	form := url.Values{
		"pipeline_name": []string{""},
		"task_number":   []string{""},
		"description":   []string{""},
		"working_dir":   []string{""},
	}
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "form-error") {
		t.Error("empty form should return form-error")
	}
}

func TestHandleStaticAssets(t *testing.T) {
	srv, _ := setupWebTest(t)

	req := httptest.NewRequest("GET", "/static/style.css", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET /static/style.css status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, should contain text/css", ct)
	}
}

func TestHandleHTMX(t *testing.T) {
	srv, _ := setupWebTest(t)

	req := httptest.NewRequest("GET", "/static/htmx.min.js", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET /static/htmx.min.js status = %d, want 200", w.Code)
	}
}

func TestStatusColors(t *testing.T) {
	tests := []struct {
		status domain.TaskStatus
		want   string
	}{
		{domain.StatusRunning, "running"},
		{domain.StatusAwaitingGate, "awaiting_gate"},
		{domain.StatusDone, "done"},
		{domain.StatusBlocked, "blocked"},
		{domain.StatusEscalated, "escalated"},
		{domain.StatusCancelled, "cancelled"},
	}
	for _, tt := range tests {
		got := statusColor(tt.status)
		if got != tt.want {
			t.Errorf("statusColor(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestGenerateTaskID(t *testing.T) {
	now := time.Date(2026, 4, 29, 14, 30, 0, 0, time.UTC)

	got := domain.GenerateTaskID("13", now, 0)
	want := "task-013-20260429-143000"
	if got != want {
		t.Errorf("GenerateTaskID = %q, want %q", got, want)
	}
	if len(got) > 30 {
		t.Errorf("GenerateTaskID = %q, exceeds 30 chars", got)
	}

	gotCollision := domain.GenerateTaskID("13", now, 1)
	wantCollision := "task-013-20260429-143000-1"
	if gotCollision != wantCollision {
		t.Errorf("GenerateTaskID with collision = %q, want %q", gotCollision, wantCollision)
	}
	if got == gotCollision {
		t.Errorf("collision suffix should produce different IDs")
	}
}

func TestTemplateFuncs(t *testing.T) {
	now := time.Date(2026, 4, 25, 14, 30, 0, 0, time.UTC)

	if got := fmtTime(now); got != "2026-04-25 14:30" {
		t.Errorf("fmtTime = %q, want %q", got, "2026-04-25 14:30")
	}

	if got := lastSegment("/path/to/file.md"); got != "file.md" {
		t.Errorf("lastSegment = %q, want %q", got, "file.md")
	}

	if got := fmtDuration(12.345); got != "12.3" {
		t.Errorf("fmtDuration = %q, want %q", got, "12.3")
	}
}

func intPtr(v int) *int { return &v }

// Verify JSON output format
func TestTaskJSONRoundTrip(t *testing.T) {
	task := &domain.Task{
		ID:             "json-test",
		PipelineName:   "backend",
		Description:    "test",
		WorkingDir:     "/tmp",
		CurrentStageID: "spec",
		Status:         domain.StatusRunning,
		Runs:           []domain.StageRun{},
		Artifacts:      map[string][]string{},
		FixCycleCount:  0,
		PRIndex:        1,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "json-test") {
		t.Error("JSON should contain task ID")
	}
}
