package web

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/engine"
	"github.com/nzinovev/synapse/internal/queue"
	"github.com/nzinovev/synapse/internal/store"
)

type Server struct {
	cfg            *domain.SynapseConfig
	store          *store.SQLiteStore
	engine         *engine.PipelineEngine
	qStore         *queue.SQLiteQueueStore
	pool           *queue.WorkerPool
	templates      *TemplateCache
	mux            *http.ServeMux
	adapterNames   []string
	defaultAdapter string
}

func NewServer(cfg *domain.SynapseConfig, s *store.SQLiteStore, eng *engine.PipelineEngine, q *queue.SQLiteQueueStore, adapterNames []string, defaultAdapter string) (*Server, error) {
	tc, err := LoadTemplates()
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	srv := &Server{
		cfg:            cfg,
		store:          s,
		engine:         eng,
		qStore:         q,
		templates:      tc,
		adapterNames:   adapterNames,
		defaultAdapter: defaultAdapter,
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	srv.mux = mux

	return srv, nil
}

func (s *Server) Handler() http.Handler {
	return recoveryMiddleware(loggingMiddleware(s.mux))
}

func (s *Server) Start(host string, port int) error {
	pool := queue.NewWorkerPool(s.qStore, s.engine, s.cfg.WorkerCount)
	s.pool = pool
	pool.Start()

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("synapse web: starting server at http://%s", addr)

	server := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	go func() {
		<-context.Background().Done()
		pool.Stop()
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /{repo}", s.handleRepoTasks)
	mux.HandleFunc("GET /tasks/{id}", s.handleTaskDetail)
	mux.HandleFunc("GET /tasks/{id}/stage-card", s.handleStageCard)
	mux.HandleFunc("GET /tasks/{id}/artifacts-card", s.handleArtifactsCard)
	mux.HandleFunc("POST /tasks/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /tasks/{id}/reject", s.handleReject)
	mux.HandleFunc("POST /tasks/{id}/answer", s.handleAnswer)
	mux.HandleFunc("POST /tasks/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /tasks/{id}/retry", s.handleRetry)
	mux.HandleFunc("GET /tasks/{id}/artifact", s.handleArtifact)
	mux.HandleFunc("GET /tasks/{id}/artifact-modal", s.handleArtifactModal)
	mux.HandleFunc("GET /tasks/{id}/stage-logs/{stage}", s.handleStageLogs)
	mux.HandleFunc("POST /tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
}

// resolvePipeline loads a pipeline for a task, returning nil if not found.
func (s *Server) resolvePipeline(task *domain.Task) *domain.Pipeline {
	p, err := domain.ResolvePipeline(task.PipelineName, s.cfg.PipelinesDir)
	if err != nil {
		return nil
	}
	return p
}

// NewServerFromConfig is a convenience that wires up all dependencies from a config.
func NewServerFromConfig(cfg *domain.SynapseConfig, registry *adapter.AdapterRegistry) (*Server, error) {
	ctx := context.Background()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=1", cfg.DBPath))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := store.RunMigrations(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	s, err := store.NewSQLiteStore(ctx, cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	eng := engine.NewPipelineEngineWithRegistry(s, registry, cfg.AdapterConfig, cfg.Adapter, cfg.PipelinesDir)
	q := queue.NewSQLiteQueueStore(db)

	adapterNames := registry.SelectableNames()

	return NewServer(cfg, s, eng, q, adapterNames, cfg.Adapter)
}
