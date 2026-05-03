package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/store"
)

const MaxFixCycles = 3

type PipelineEngine struct {
	store          store.TaskStore
	adapter        adapter.AgentAdapter // legacy: single fixed adapter
	registry       *adapter.AdapterRegistry
	adapterConfig  domain.AdapterConfig
	defaultAdapter string
	agentRegistry  *agent.AgentRegistry
	pipelinesDir   string
	pipeline       *domain.Pipeline
	taskMutexes    sync.Map
	adapterCache   sync.Map // map[string]adapter.AgentAdapter
}

func NewPipelineEngine(s store.TaskStore, a adapter.AgentAdapter, pipelinesDir string) *PipelineEngine {
	return &PipelineEngine{
		store:        s,
		adapter:      a,
		pipelinesDir: pipelinesDir,
	}
}

func NewPipelineEngineWithPipeline(s store.TaskStore, a adapter.AgentAdapter, p *domain.Pipeline) *PipelineEngine {
	return &PipelineEngine{
		store:    s,
		adapter:  a,
		pipeline: p,
	}
}

func NewPipelineEngineWithRegistry(
	s store.TaskStore,
	r *adapter.AdapterRegistry,
	cfg domain.AdapterConfig,
	defaultAdapter string,
	pipelinesDir string,
) *PipelineEngine {
	return &PipelineEngine{
		store:          s,
		registry:       r,
		adapterConfig:  cfg,
		defaultAdapter: defaultAdapter,
		pipelinesDir:   pipelinesDir,
	}
}

func NewPipelineEngineWithAgents(
	s store.TaskStore,
	ar *agent.AgentRegistry,
	r *adapter.AdapterRegistry,
	cfg domain.AdapterConfig,
	defaultAdapter string,
	pipelinesDir string,
) *PipelineEngine {
	return &PipelineEngine{
		store:          s,
		registry:       r,
		adapterConfig:  cfg,
		defaultAdapter: defaultAdapter,
		agentRegistry:  ar,
		pipelinesDir:   pipelinesDir,
	}
}

func (e *PipelineEngine) resolveAdapter(task *domain.Task) (adapter.AgentAdapter, string, error) {
	if e.registry == nil {
		return e.adapter, e.adapter.Name(), nil
	}

	adapterName := task.Adapter
	if adapterName == "" {
		adapterName = e.defaultAdapter
	}

	if cached, ok := e.adapterCache.Load(adapterName); ok {
		return cached.(adapter.AgentAdapter), adapterName, nil
	}

	a, err := e.registry.Create(adapterName, e.adapterConfig)
	if err != nil {
		return nil, "", fmt.Errorf("resolve adapter %q: %w", adapterName, err)
	}
	e.adapterCache.Store(adapterName, a)
	return a, adapterName, nil
}

func (e *PipelineEngine) getMutex(taskID string) *sync.Mutex {
	val, _ := e.taskMutexes.LoadOrStore(taskID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

func (e *PipelineEngine) loadPipeline(task *domain.Task) (*domain.Pipeline, error) {
	if e.pipeline != nil {
		return e.pipeline, nil
	}
	return domain.ResolvePipeline(task.PipelineName, e.pipelinesDir)
}

func (e *PipelineEngine) RunUntilGate(ctx context.Context, taskID string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	return e.runLoop(ctx, task)
}

func (e *PipelineEngine) Approve(ctx context.Context, taskID string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if err := e.doApprove(ctx, task); err != nil {
		return nil, err
	}
	return e.runLoop(ctx, task)
}

func (e *PipelineEngine) Reject(ctx context.Context, taskID, feedback string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if err := e.doReject(ctx, task, feedback); err != nil {
		return nil, err
	}
	return e.runLoop(ctx, task)
}

func (e *PipelineEngine) Retry(ctx context.Context, taskID string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if task.Status != domain.StatusBlocked && task.Status != domain.StatusEscalated {
		return nil, fmt.Errorf("task %s is not BLOCKED or ESCALATED (status=%s); cannot retry", taskID, task.Status)
	}

	if err := e.doRetry(ctx, task); err != nil {
		return nil, err
	}
	return e.runLoop(ctx, task)
}

func (e *PipelineEngine) Cancel(ctx context.Context, taskID string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer e.getMutex(taskID).Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if task.Status != domain.StatusAwaitingGate {
		return nil, fmt.Errorf("task %s is not AWAITING_GATE (status=%s); cannot cancel", taskID, task.Status)
	}

	return e.doCancel(ctx, task)
}

func (e *PipelineEngine) PrepareApprove(ctx context.Context, taskID string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	if err := e.doApprove(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (e *PipelineEngine) Answer(ctx context.Context, taskID, answers string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if err := e.doAnswer(ctx, task, answers); err != nil {
		return nil, err
	}
	return e.runLoop(ctx, task)
}

func (e *PipelineEngine) PrepareAnswer(ctx context.Context, taskID, answers string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	if err := e.doAnswer(ctx, task, answers); err != nil {
		return nil, err
	}
	return task, nil
}

func (e *PipelineEngine) PrepareReject(ctx context.Context, taskID, feedback string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	if err := e.doReject(ctx, task, feedback); err != nil {
		return nil, err
	}
	return task, nil
}

func (e *PipelineEngine) PrepareRetry(ctx context.Context, taskID string) (*domain.Task, error) {
	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if task.Status != domain.StatusBlocked && task.Status != domain.StatusEscalated {
		return nil, fmt.Errorf("task %s is not BLOCKED or ESCALATED (status=%s); cannot retry", taskID, task.Status)
	}

	if err := e.doRetry(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// Internal state transitions

func (e *PipelineEngine) doApprove(ctx context.Context, task *domain.Task) error {
	if task.Status != domain.StatusAwaitingGate {
		return fmt.Errorf("task %s is not AWAITING_GATE (status=%s)", task.ID, task.Status)
	}

	pipeline, err := e.loadPipeline(task)
	if err != nil {
		return fmt.Errorf("load pipeline: %w", err)
	}

	currentStage, err := pipeline.GetStage(task.CurrentStageID)
	if err != nil {
		return fmt.Errorf("get stage: %w", err)
	}

	e.emitEvent(ctx, task.ID, task.CurrentStageID, domain.EventGateApproved,
		fmt.Sprintf("Gate approved for stage %q", task.CurrentStageID), nil)

	if currentStage.Gate != domain.GateHumanFinal {
		for _, pathStr := range task.Artifacts[currentStage.ID] {
			StampArtifactApproved(pathStr)
		}
	}

	if currentStage.Gate == domain.GateHumanFinal {
		task.Status = domain.StatusDone
		e.emitEvent(ctx, task.ID, task.CurrentStageID, domain.EventTaskDone, "Task completed.", nil)
		return e.store.SaveTask(ctx, task)
	}

	nextStage, err := pipeline.NextStage(task.CurrentStageID)
	if err != nil {
		return fmt.Errorf("next stage: %w", err)
	}
	if nextStage == nil {
		task.Status = domain.StatusDone
		e.emitEvent(ctx, task.ID, task.CurrentStageID, domain.EventTaskDone, "Task completed (last stage approved).", nil)
		return e.store.SaveTask(ctx, task)
	}

	task.CurrentStageID = nextStage.ID
	task.Status = domain.StatusRunning
	return e.store.SaveTask(ctx, task)
}

func (e *PipelineEngine) doReject(ctx context.Context, task *domain.Task, feedback string) error {
	if task.Status != domain.StatusAwaitingGate {
		return fmt.Errorf("task %s is not AWAITING_GATE (status=%s)", task.ID, task.Status)
	}

	pipeline, err := e.loadPipeline(task)
	if err != nil {
		return fmt.Errorf("load pipeline: %w", err)
	}

	currentStage, err := pipeline.GetStage(task.CurrentStageID)
	if err != nil {
		return fmt.Errorf("get stage: %w", err)
	}

	e.emitEvent(ctx, task.ID, task.CurrentStageID, domain.EventGateRejected,
		fmt.Sprintf("Gate rejected for stage %q: %s", task.CurrentStageID, feedback),
		map[string]any{"feedback": feedback})

	for i := len(task.Runs) - 1; i >= 0; i-- {
		if task.Runs[i].StageID == task.CurrentStageID {
			task.Runs[i].RejectionFeedback = &feedback
			break
		}
	}

	if currentStage.Gate == domain.GateHumanFinal {
		fixStage, err := pipeline.GetStage("fix")
		if err == nil {
			task.FixCycleCount++
			task.CurrentStageID = fixStage.ID
		}
	}

	task.Status = domain.StatusRunning
	return e.store.SaveTask(ctx, task)
}

func (e *PipelineEngine) doAnswer(ctx context.Context, task *domain.Task, answers string) error {
	if task.Status != domain.StatusAwaitingGate {
		return fmt.Errorf("task %s is not AWAITING_GATE (status=%s)", task.ID, task.Status)
	}

	e.emitEvent(ctx, task.ID, task.CurrentStageID, domain.EventGateAnswered,
		fmt.Sprintf("Open question answers provided for stage %q", task.CurrentStageID),
		map[string]any{"answers": answers})

	for i := len(task.Runs) - 1; i >= 0; i-- {
		if task.Runs[i].StageID == task.CurrentStageID {
			task.Runs[i].AnswersFeedback = &answers
			break
		}
	}

	task.Status = domain.StatusRunning
	return e.store.SaveTask(ctx, task)
}

func (e *PipelineEngine) doRetry(ctx context.Context, task *domain.Task) error {
	if task.Status == domain.StatusEscalated {
		task.FixCycleCount = 0
	}
	task.Status = domain.StatusRunning
	return e.store.SaveTask(ctx, task)
}

func (e *PipelineEngine) doCancel(ctx context.Context, task *domain.Task) (*domain.Task, error) {
	task.Status = domain.StatusCancelled
	e.emitEvent(ctx, task.ID, task.CurrentStageID, domain.EventTaskCancelled, "Task cancelled by user.", nil)
	if err := e.store.SaveTask(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// Core execution loop

func (e *PipelineEngine) runLoop(ctx context.Context, task *domain.Task) (*domain.Task, error) {
	pipeline, err := e.loadPipeline(task)
	if err != nil {
		return nil, fmt.Errorf("load pipeline: %w", err)
	}

	for {
		if task.Status == domain.StatusDone || task.Status == domain.StatusBlocked ||
			task.Status == domain.StatusEscalated || task.Status == domain.StatusCancelled {
			return task, nil
		}

		stage, err := pipeline.GetStage(task.CurrentStageID)
		if err != nil {
			return nil, fmt.Errorf("get stage: %w", err)
		}

		// Determine attempt number and trigger.
		// Skip incomplete runs (StartedAt set, FinishedAt nil): they were interrupted
		// mid-invocation and do not count toward the attempt number.
		var priorRuns []domain.StageRun
		for _, r := range task.Runs {
			if r.StageID == stage.ID {
				if !r.StartedAt.IsZero() && r.FinishedAt == nil {
					continue
				}
				priorRuns = append(priorRuns, r)
			}
		}
		attempt := len(priorRuns) + 1

		var trigger domain.RunTrigger
		var rejectionFeedback *string
		var openQuestionAnswers *string
		var previousStdout *string
		var previousStderr *string

		if attempt == 1 {
			trigger = domain.TriggerInitial
		} else {
			lastRun := priorRuns[len(priorRuns)-1]
			if lastRun.AnswersFeedback != nil {
				trigger = domain.TriggerAnswers
				openQuestionAnswers = lastRun.AnswersFeedback
			} else if lastRun.RejectionFeedback != nil {
				trigger = domain.TriggerRejection
				rejectionFeedback = lastRun.RejectionFeedback
			} else {
				trigger = domain.TriggerRetry
			}
			if lastRun.AgentResult != nil {
				previousStdout = &lastRun.AgentResult.Stdout
				previousStderr = &lastRun.AgentResult.Stderr
			}
		}

		// Cross-stage fallback: when fix stage starts fresh (attempt 1) with no
		// rejection feedback from same-stage runs, look for the most recent
		// completed run from another stage that carries rejection feedback.
		// This handles human_final rejection where feedback is stored on the
		// rejected stage (e.g. "done"), not on the fix stage.
		if stage.ID == "fix" && rejectionFeedback == nil && openQuestionAnswers == nil {
			for i := len(task.Runs) - 1; i >= 0; i-- {
				r := task.Runs[i]
				if r.StageID == stage.ID || r.FinishedAt == nil {
					continue
				}
				if r.RejectionFeedback != nil {
					rejectionFeedback = r.RejectionFeedback
					trigger = domain.TriggerRejection
					if r.AgentResult != nil {
						previousStdout = &r.AgentResult.Stdout
						previousStderr = &r.AgentResult.Stderr
					}
					break
				}
			}
		}

		now := time.Now().UTC()
		stageRun := domain.StageRun{
			StageID:   stage.ID,
			Attempt:   attempt,
			Trigger:   trigger,
			StartedAt: now,
		}
		// Strip any incomplete runs for this stage before appending the recovery run.
		// Keeping them would cause a UNIQUE(task_id, stage_id, attempt) violation on save.
		cleanRuns := make([]domain.StageRun, 0, len(task.Runs))
		for _, r := range task.Runs {
			if r.StageID == stage.ID && !r.StartedAt.IsZero() && r.FinishedAt == nil {
				continue
			}
			cleanRuns = append(cleanRuns, r)
		}
		task.Runs = append(cleanRuns, stageRun)

		e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageStarted,
			fmt.Sprintf("Starting stage %q (attempt %d, trigger=%s)", stage.ID, attempt, trigger),
			map[string]any{"attempt": attempt, "trigger": string(trigger)})

		if err := e.store.SaveTask(ctx, task); err != nil {
			return nil, fmt.Errorf("save task (stage start): %w", err)
		}

		// Gather context artifacts from prior stages.
		contextArtifacts := make(map[string][]string)
		for _, prevStage := range pipeline.Stages {
			if prevStage.ID == stage.ID {
				break
			}
			if paths, ok := task.Artifacts[prevStage.ID]; ok && len(paths) > 0 {
				contextArtifacts[prevStage.ID] = paths
			}
		}

		// For fix stage, also include review artifacts.
		if stage.ID == "fix" {
			if reviewPaths, ok := task.Artifacts["review"]; ok && len(reviewPaths) > 0 {
				contextArtifacts["review"] = reviewPaths
			}
		}

		// For implement stage on PR2+, include previous handoff.
		handoffRe := regexp.MustCompile(`/handoff/.*-pr\d+\.md$`)
		if stage.ID == "implement" && task.PRIndex > 1 {
			var handoffPaths []string
			for _, p := range task.Artifacts["implement"] {
				if handoffRe.MatchString(p) {
					handoffPaths = append(handoffPaths, p)
				}
			}
			if len(handoffPaths) > 0 {
				contextArtifacts["implement_handoff"] = handoffPaths
			}
		}

		stageWorkdir := e.store.StageWorkdir(task.ID, stage.ID, attempt)

		// Snapshot files matching produces_glob before invocation.
		var beforeGlob []string
		if stage.ProducesGlob != "" {
			beforeGlob = globFiles(task.WorkingDir, stage.ProducesGlob)
		}

		// Invoke agent or adapter (or use null result for done stage).
		var result domain.AgentResult
		var runResult *agent.RunResult

		if stage.Agent == "" || stage.Agent == "null" {
			exitCode := 0
			result = domain.AgentResult{
				Success:          true,
				ExitCode:         &exitCode,
				DurationSeconds:  0,
				ArtifactsCreated: nil,
			}
		} else if e.agentRegistry != nil && e.agentRegistry.Has(stage.Agent) {
			agentInst, err := e.agentRegistry.Create(stage.Agent, e.adapterConfig)
			if err != nil {
				return nil, fmt.Errorf("resolve agent %q: %w", stage.Agent, err)
			}

			resolvedModel := ""
			if stage.Model != "" {
				if name, ok := e.adapterConfig.ModelTiers[stage.Model]; ok {
					resolvedModel = name
				} else {
					e.emitEvent(ctx, task.ID, stage.ID, domain.EventAgentOutput,
						fmt.Sprintf("model tier %q has no mapping for the active adapter; invoking without --model flag", stage.Model),
						nil)
				}
			}

			runInput := agent.NewRunInput(task.ID, task.ID, task.Description, task.WorkingDir, stage.ID, task.PipelineName, stage.Gate)
			runInput.PriorOutputs = buildPriorOutputs(contextArtifacts)
			if rejectionFeedback != nil {
				runInput.Feedback = &agent.FeedbackDetail{Kind: "rejection", Text: *rejectionFeedback}
			}
			if openQuestionAnswers != nil {
				runInput.Feedback = &agent.FeedbackDetail{Kind: "answers", Text: *openQuestionAnswers}
			}
			runInput.StageWorkdir = stageWorkdir
			runInput.PRIndex = task.PRIndex
			runInput.FixCycleCount = task.FixCycleCount
			runInput.PreviousStdout = previousStdout
			runInput.PreviousStderr = previousStderr
			runInput.Model = resolvedModel

			rr, err := agentInst.Run(ctx, runInput)
			if err != nil {
				return nil, fmt.Errorf("agent run: %w", err)
			}
			runResult = &rr

			exitCode := 0
			result = domain.AgentResult{
				Success:         true,
				Stdout:          rr.Stdout,
				Stderr:          rr.Stderr,
				DurationSeconds: rr.DurationSeconds,
				ExitCode:        &exitCode,
			}
			for _, art := range rr.Artifacts {
				result.ArtifactsCreated = append(result.ArtifactsCreated, art.Path)
			}
			stageRun.Adapter = agentInst.Name()
		} else {
			resolvedAdapter, resolvedName, err := e.resolveAdapter(task)
			if err != nil {
				return nil, err
			}
			stageRun.Adapter = resolvedName
			task.Runs[len(task.Runs)-1] = stageRun

			resolvedModel := ""
			if stage.Model != "" {
				if name, ok := e.adapterConfig.ModelTiers[stage.Model]; ok {
					resolvedModel = name
				} else {
					e.emitEvent(ctx, task.ID, stage.ID, domain.EventAgentOutput,
						fmt.Sprintf("model tier %q has no mapping for the active adapter; invoking without --model flag", stage.Model),
						nil)
				}
			}

			r, err := resolvedAdapter.Invoke(ctx, domain.InvokeParams{
				AgentName:           stage.Agent,
				TaskDescription:     task.Description,
				WorkingDir:          task.WorkingDir,
				ContextArtifacts:    contextArtifacts,
				RejectionFeedback:   rejectionFeedback,
				OpenQuestionAnswers: openQuestionAnswers,
				StageWorkdir:        stageWorkdir,
				StageID:             stage.ID,
				Gate:                stage.Gate,
				PipelineName:        task.PipelineName,
				FixCycleCount:       task.FixCycleCount,
				PRIndex:             task.PRIndex,
				PreviousStdout:      previousStdout,
				PreviousStderr:      previousStderr,
				Model:               resolvedModel,
			})
			if err != nil {
				return nil, fmt.Errorf("adapter invoke: %w", err)
			}
			result = r
		}

		// Read result.json from stage workdir if available and merge with in-memory result.
		if fileResult, err := agent.ReadStageResult(stageWorkdir); err == nil && fileResult != nil {
			if runResult == nil {
				runResult = fileResult
			} else {
				mergeRunResult(runResult, fileResult)
			}
		}

		// Resolve new artifacts via glob diff.
		var newArtifacts []string
		if stage.ProducesGlob != "" {
			afterGlob := globFiles(task.WorkingDir, stage.ProducesGlob)
			newArtifacts = diffGlobResults(beforeGlob, afterGlob)
		} else {
			for _, p := range result.ArtifactsCreated {
				abs, err := filepath.Abs(p)
				if err != nil {
					abs = p
				}
				newArtifacts = append(newArtifacts, abs)
			}
		}

		// Update the StageRun.
		finishedAt := time.Now().UTC()
		stageRun.FinishedAt = &finishedAt
		stageRun.AgentResult = &result
		// Update the last run in the task's runs slice.
		task.Runs[len(task.Runs)-1] = stageRun

		if len(newArtifacts) > 0 {
			task.Artifacts[stage.ID] = newArtifacts
		}

		if !result.Success {
			task.Status = domain.StatusBlocked
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageFailed,
				fmt.Sprintf("Stage %q failed (exit_code=%v)", stage.ID, result.ExitCode),
				map[string]any{"exit_code": result.ExitCode})
			e.store.SaveTask(ctx, task)
			return task, nil
		}

		e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageCompleted,
			fmt.Sprintf("Stage %q completed successfully", stage.ID),
			map[string]any{"artifacts": newArtifacts})

		// Evaluate gate using the standalone function.
		// Use existing stage artifacts when no new artifacts were produced,
		// so gate evaluation can access previously written files (e.g. review verdicts).
		stdout := result.Stdout
		gateArtifacts := newArtifacts
		if len(gateArtifacts) == 0 {
			gateArtifacts = task.Artifacts[stage.ID]
		}
		gr := evaluateGate(stage.Gate, stage.ID, runResult, stdout, gateArtifacts)

		terminal, err := e.applyGateResult(ctx, task, pipeline, stage, gr, newArtifacts)
		if err != nil {
			return nil, err
		}
		if terminal {
			return task, nil
		}
	}
}

// applyGateResult interprets a GateResult and performs the corresponding state transitions.
func (e *PipelineEngine) applyGateResult(ctx context.Context, task *domain.Task, pipeline *domain.Pipeline, stage *domain.Stage, gr GateResult, newArtifacts []string) (bool, error) {
	switch gr.Outcome {
	case GateAdvance:
		return e.continueAfterAutoGate(ctx, task, pipeline, stage)

	case GateRoute:
		return e.applyRoute(ctx, task, pipeline, stage, gr)

	default:
		task.Status = domain.StatusAwaitingGate
		gateLabel := string(stage.Gate)
		if gr.VerdictRaw != "" {
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventGateAwaiting,
				fmt.Sprintf("Reviewer verdict %s (or unreadable) — awaiting human", gr.VerdictRaw),
				map[string]any{"gate": gateLabel, "verdict": gr.VerdictRaw})
		} else {
			reason := ""
			if stage.Gate == domain.GateAutoIfClean {
				reason = " (auto_if_clean: open questions or QUESTIONS.md)"
			}
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventGateAwaiting,
				fmt.Sprintf("Waiting for %s at stage %q%s", gateLabel, stage.ID, reason),
				map[string]any{"gate": gateLabel})
		}
		e.store.SaveTask(ctx, task)
		return true, nil
	}
}

// applyRoute handles routing after auto_on_approval evaluates to a route result.
func (e *PipelineEngine) applyRoute(ctx context.Context, task *domain.Task, pipeline *domain.Pipeline, stage *domain.Stage, gr GateResult) (bool, error) {
	if gr.RouteTo == "done" {
		// Check for multi-PR handoff detection.
		handoffRe := regexp.MustCompile(`/handoff/.*-pr\d+\.md$`)
		hasHandoff := false
		for _, p := range task.Artifacts["implement"] {
			if handoffRe.MatchString(p) {
				hasHandoff = true
				break
			}
		}

		if hasHandoff {
			task.PRIndex++
			task.Artifacts["implement"] = nil
			implementStage, err := pipeline.GetStage("implement")
			if err != nil {
				return false, fmt.Errorf("get implement stage: %w", err)
			}
			task.CurrentStageID = implementStage.ID
			task.Status = domain.StatusRunning
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageStarted,
				fmt.Sprintf("PR %d approved — starting implement for PR %d", task.PRIndex-1, task.PRIndex),
				map[string]any{"pr_index": task.PRIndex})
			e.store.SaveTask(ctx, task)
			return false, nil
		}

		doneStage, err := pipeline.GetStage("done")
		if err != nil {
			return false, fmt.Errorf("get done stage: %w", err)
		}
		task.CurrentStageID = doneStage.ID
		task.Status = domain.StatusRunning
		e.store.SaveTask(ctx, task)
		return false, nil
	}

	if gr.RouteTo == "fix" {
		if task.FixCycleCount >= MaxFixCycles {
			task.Status = domain.StatusEscalated
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageFailed,
				fmt.Sprintf("Max fix cycles (%d) reached — escalated, human intervention required", MaxFixCycles),
				map[string]any{"reason": "max_fix_cycles_exceeded"})
			e.store.SaveTask(ctx, task)
			return true, nil
		}
		task.FixCycleCount++
		fixStage, err := pipeline.GetStage("fix")
		if err != nil {
			return false, fmt.Errorf("get fix stage: %w", err)
		}
		task.CurrentStageID = fixStage.ID
		task.Status = domain.StatusRunning
		e.store.SaveTask(ctx, task)
		return false, nil
	}

	return false, nil
}

func (e *PipelineEngine) continueAfterAutoGate(ctx context.Context, task *domain.Task, pipeline *domain.Pipeline, stage *domain.Stage) (bool, error) {
	// After fix, route to reviewer stage if present.
	var reviewerStage *domain.Stage
	for i := range pipeline.Stages {
		if pipeline.Stages[i].Gate == domain.GateAutoOnApproval {
			reviewerStage = &pipeline.Stages[i]
			break
		}
	}

	var nextStage *domain.Stage
	if reviewerStage != nil && stage.ID == "fix" {
		nextStage = reviewerStage
	} else {
		var err error
		nextStage, err = pipeline.NextStage(stage.ID)
		if err != nil {
			return false, err
		}
	}

	if nextStage == nil {
		task.Status = domain.StatusDone
		e.emitEvent(ctx, task.ID, stage.ID, domain.EventTaskDone, "All stages complete.", nil)
		e.store.SaveTask(ctx, task)
		return true, nil
	}

	// Handle handoff detection for multi-PR flow (auto_on_approval APPROVED).
	handoffRe := regexp.MustCompile(`/handoff/.*-pr\d+\.md$`)
	hasHandoff := false
	for _, p := range task.Artifacts["implement"] {
		if handoffRe.MatchString(p) {
			hasHandoff = true
			break
		}
	}

	if hasHandoff && stage.Gate == domain.GateAutoOnApproval {
		task.PRIndex++
		task.Artifacts["implement"] = nil
		implementStage, err := pipeline.GetStage("implement")
		if err != nil {
			return false, fmt.Errorf("get implement stage: %w", err)
		}
		task.CurrentStageID = implementStage.ID
		task.Status = domain.StatusRunning
		e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageStarted,
			fmt.Sprintf("PR %d approved — starting implement for PR %d", task.PRIndex-1, task.PRIndex),
			map[string]any{"pr_index": task.PRIndex})
		e.store.SaveTask(ctx, task)
		return false, nil
	}

	task.CurrentStageID = nextStage.ID
	task.Status = domain.StatusRunning
	e.store.SaveTask(ctx, task)
	return false, nil
}

// parseReviewerVerdict is retained for the adapter fallback path.
func (e *PipelineEngine) parseReviewerVerdict(stageID string, task *domain.Task) string {
	artifactPaths := task.Artifacts[stageID]
	if len(artifactPaths) == 0 {
		return ""
	}
	reviewPath := artifactPaths[len(artifactPaths)-1]
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		return ""
	}
	return ParseVerdict(string(data))
}

// Helpers

func (e *PipelineEngine) emitEvent(ctx context.Context, taskID, stageID string, kind domain.EventKind, message string, metadata map[string]any) {
	e.store.AppendEvent(ctx, &domain.AgentEvent{
		TaskID:    taskID,
		StageID:   stageID,
		Timestamp: time.Now().UTC(),
		Kind:      kind,
		Message:   message,
		Metadata:  metadata,
	})
}

func globFiles(baseDir, pattern string) []string {
	fullPattern := filepath.Join(baseDir, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil
	}
	return matches
}

func diffGlobResults(before, after []string) []string {
	beforeSet := make(map[string]bool, len(before))
	for _, p := range before {
		beforeSet[p] = true
	}
	var diff []string
	for _, p := range after {
		if !beforeSet[p] {
			diff = append(diff, p)
		}
	}
	return diff
}

func buildPriorOutputs(contextArtifacts map[string][]string) []agent.PriorOutput {
	var outputs []agent.PriorOutput
	for stageID, paths := range contextArtifacts {
		var refs []agent.ArtifactRef
		for _, p := range paths {
			refs = append(refs, agent.ArtifactRef{Path: p, StageID: stageID})
		}
		outputs = append(outputs, agent.PriorOutput{StageID: stageID, Artifacts: refs})
	}
	return outputs
}

func mergeRunResult(base, file *agent.RunResult) {
	if file.Verdict != "" {
		base.Verdict = file.Verdict
	}
	if len(file.OpenQuestions) > 0 {
		base.OpenQuestions = file.OpenQuestions
	}
	if file.FromFile {
		base.FromFile = true
	}
	if file.Stdout != "" {
		base.Stdout = file.Stdout
	}
	if file.Stderr != "" {
		base.Stderr = file.Stderr
	}
	if len(file.Artifacts) > 0 {
		base.Artifacts = file.Artifacts
	}
}
