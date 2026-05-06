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
	"github.com/nzinovev/synapse/internal/tool"
)

const MaxFixCycles = 3

// NativeFactory is a function that constructs a native agent for a given
// AgentDefinition and ToolPermission. If nil, the engine falls back to the
// AgentRegistry path when stage.Runtime == "native".
type NativeFactory func(def agent.AgentDefinition, perm tool.ToolPermission) (agent.Agent, error)

type PipelineEngine struct {
	store          store.TaskStore
	adapter        adapter.AgentAdapter // legacy: single fixed adapter
	registry       *adapter.AdapterRegistry
	agentRegistry  *agent.AgentRegistry
	nativeFactory  NativeFactory
	synapseConfig  domain.SynapseConfig
	adapterConfig  domain.AdapterConfig
	defaultAdapter string
	pipelinesDir   string
	pipeline       *domain.Pipeline
	taskMutexes    sync.Map
	adapterCache   sync.Map // map[string]adapter.AgentAdapter
	cancelFuncs    sync.Map // map[string]context.CancelFunc — per-task cancel functions
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
	agentRegistry *agent.AgentRegistry,
	r *adapter.AdapterRegistry,
	cfg domain.AdapterConfig,
	defaultAdapter string,
	pipelinesDir string,
) *PipelineEngine {
	return &PipelineEngine{
		store:          s,
		agentRegistry:  agentRegistry,
		registry:       r,
		adapterConfig:  cfg,
		defaultAdapter: defaultAdapter,
		pipelinesDir:   pipelinesDir,
	}
}

// SetNativeFactory registers a NativeFactory on the engine. When set, any
// stage with Runtime == "native" is dispatched through this factory instead of
// the AgentRegistry. If not set, the engine falls back to AgentRegistry for
// native stages.
func (e *PipelineEngine) SetNativeFactory(f NativeFactory) {
	e.nativeFactory = f
}

// SetSynapseConfig stores the full SynapseConfig so the engine can load
// AgentDefinitions for native stages.
func (e *PipelineEngine) SetSynapseConfig(cfg domain.SynapseConfig) {
	e.synapseConfig = cfg
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

func (e *PipelineEngine) setCancelFunc(taskID string, cancel context.CancelFunc) {
	e.cancelFuncs.Store(taskID, cancel)
}

func (e *PipelineEngine) clearCancelFunc(taskID string) {
	e.cancelFuncs.Delete(taskID)
}

func (e *PipelineEngine) getCancelFunc(taskID string) (context.CancelFunc, bool) {
	v, ok := e.cancelFuncs.Load(taskID)
	if !ok {
		return nil, false
	}
	return v.(context.CancelFunc), true
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
	if cancel, ok := e.getCancelFunc(taskID); ok {
		cancel()
	}

	mu := e.getMutex(taskID)
	mu.Lock()
	defer mu.Unlock()

	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	if task.Status == domain.StatusCancelled {
		return task, nil
	}

	if task.Status != domain.StatusAwaitingGate && task.Status != domain.StatusRunning {
		return nil, fmt.Errorf("task %s is not cancellable (status=%s)", taskID, task.Status)
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
	var cleaned []domain.StageRun
	for _, r := range task.Runs {
		if r.StartedAt.IsZero() || r.FinishedAt != nil {
			cleaned = append(cleaned, r)
		}
	}
	task.Runs = cleaned

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

		// Build RunInput for the agent.
		input := agent.NewRunInput(
			task.ID, task.ID, task.Description,
			task.WorkingDir, stage.ID, task.PipelineName, stage.Gate,
		)
		input.StageWorkdir = stageWorkdir
		input.PRIndex = task.PRIndex
		input.FixCycleCount = task.FixCycleCount
		input.PreviousStdout = previousStdout
		input.PreviousStderr = previousStderr
		if stage.Model != "" {
			if name, ok := e.adapterConfig.ModelTiers[stage.Model]; ok {
				input.Model = name
			} else {
				e.emitEvent(ctx, task.ID, stage.ID, domain.EventAgentOutput,
					fmt.Sprintf("model tier %q has no mapping for the active adapter; invoking without --model flag", stage.Model),
					nil)
			}
		}

		// Build PriorOutputs from context artifacts.
		for stageID, paths := range contextArtifacts {
			refs := make([]agent.ArtifactRef, 0, len(paths))
			for _, p := range paths {
				refs = append(refs, agent.ArtifactRef{Path: p, StageID: stageID})
			}
			input.PriorOutputs = append(input.PriorOutputs, agent.PriorOutput{
				StageID:   stageID,
				Artifacts: refs,
			})
		}

		// Set feedback.
		if rejectionFeedback != nil {
			input.Feedback = &agent.FeedbackDetail{Kind: "rejection", Text: *rejectionFeedback}
		} else if openQuestionAnswers != nil {
			input.Feedback = &agent.FeedbackDetail{Kind: "answers", Text: *openQuestionAnswers}
		}

		// Write durable input snapshot before invocation.
		agent.WriteStageInput(stageWorkdir, input)

		// Invoke agent (or use null result for done/null stages).
		var runResult agent.RunResult
		if stage.Agent == "" || stage.Agent == "null" {
			runResult = agent.RunResult{
				SchemaVersion: agent.SchemaVersion,
				Status:        agent.StatusCompleted,
			}
		} else {
			stageRun.Adapter = e.defaultAdapter
			task.Runs[len(task.Runs)-1] = stageRun

			invokeCtx, invokeCancel := context.WithCancel(ctx)
			e.setCancelFunc(task.ID, invokeCancel)
			defer func() {
				invokeCancel()
				e.clearCancelFunc(task.ID)
			}()

			rr, err := e.resolveAndRunAgent(invokeCtx, task, stage, input, stageWorkdir)
			if err != nil {
				if invokeCtx.Err() == context.Canceled {
					return e.doCancel(ctx, task)
				}
				return nil, fmt.Errorf("agent invoke: %w", err)
			}
			runResult = rr

			if invokeCtx.Err() == context.Canceled {
				return e.doCancel(ctx, task)
			}
		}

		// Write durable logs.
		agent.WriteStageLogs(stageWorkdir, runResult.Stdout, runResult.Stderr)

		// Resolve new artifacts via glob diff.
		var newArtifacts []string
		if stage.ProducesGlob != "" {
			afterGlob := globFiles(task.WorkingDir, stage.ProducesGlob)
			newArtifacts = diffGlobResults(beforeGlob, afterGlob)
		} else {
			for _, p := range runResult.Artifacts {
				abs, err := filepath.Abs(p.Path)
				if err != nil {
					abs = p.Path
				}
				newArtifacts = append(newArtifacts, abs)
			}
		}

		// Write durable artifact list and result.
		artifactRefs := make([]agent.ArtifactRef, 0, len(newArtifacts))
		for _, p := range newArtifacts {
			artifactRefs = append(artifactRefs, agent.ArtifactRef{Path: p, StageID: stage.ID})
		}
		agent.WriteStageArtifacts(stageWorkdir, artifactRefs)
		agent.WriteStageResult(stageWorkdir, runResult)

		// Map RunResult back to domain.AgentResult for backward compat storage.
		exitCode := 0
		agentSuccess := runResult.Status == agent.StatusCompleted
		if !agentSuccess {
			exitCode = 1
		}
		agentResult := domain.AgentResult{
			Success:         agentSuccess,
			Stdout:          runResult.Stdout,
			Stderr:          runResult.Stderr,
			DurationSeconds: runResult.DurationSeconds,
			ExitCode:        &exitCode,
		}

		// Update the StageRun.
		finishedAt := time.Now().UTC()
		stageRun.FinishedAt = &finishedAt
		stageRun.AgentResult = &agentResult
		task.Runs[len(task.Runs)-1] = stageRun

		if len(newArtifacts) > 0 {
			task.Artifacts[stage.ID] = newArtifacts
		}

		if !agentSuccess {
			task.Status = domain.StatusBlocked
			e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageFailed,
				fmt.Sprintf("Stage %q failed (exit_code=%v)", stage.ID, exitCode),
				map[string]any{"exit_code": exitCode})
			e.store.SaveTask(ctx, task)
			return task, nil
		}

		e.emitEvent(ctx, task.ID, stage.ID, domain.EventStageCompleted,
			fmt.Sprintf("Stage %q completed successfully", stage.ID),
			map[string]any{"artifacts": newArtifacts})

		// Evaluate gate using structured result.
		terminal, err := e.evaluateGate(ctx, task, pipeline, stage, runResult, newArtifacts)
		if err != nil {
			return nil, err
		}
		if terminal {
			return task, nil
		}
	}
}

// resolveAndRunAgent runs the agent for a stage. Routing priority:
//  1. stage.Runtime == "native" + nativeFactory set → NativeFactory path
//  2. agentRegistry has stage.Agent → AgentRegistry path
//  3. adapter fallback
//
// After running, it attempts to read a result.json written by the agent and
// merges it into the result.
func (e *PipelineEngine) resolveAndRunAgent(
	ctx context.Context,
	task *domain.Task,
	stage *domain.Stage,
	input agent.RunInput,
	stageWorkdir string,
) (agent.RunResult, error) {
	// Native runtime path — use NativeFactory when the stage opts in.
	if stage.Runtime == "native" && e.nativeFactory != nil {
		def, err := agent.LoadAgent(stage.Agent, e.synapseConfig)
		if err != nil {
			return agent.RunResult{}, fmt.Errorf("load agent definition %q: %w", stage.Agent, err)
		}
		perm := tool.ToolPermission{
			AllowWrites:  true,
			AllowShell:   false,
			WorkspaceDir: task.WorkingDir,
		}
		a, err := e.nativeFactory(def, perm)
		if err != nil {
			return agent.RunResult{}, fmt.Errorf("create native agent %q: %w", stage.Agent, err)
		}
		result, err := a.Run(ctx, input)
		if err != nil {
			return agent.RunResult{}, err
		}
		return e.mergeFileResult(result, stageWorkdir), nil
	}

	// Agent registry path.
	if e.agentRegistry != nil && e.agentRegistry.Has(stage.Agent) {
		a, err := e.agentRegistry.Create(stage.Agent, e.adapterConfig)
		if err != nil {
			return agent.RunResult{}, fmt.Errorf("create agent %q: %w", stage.Agent, err)
		}

		result, err := a.Run(ctx, input)
		if err != nil {
			return agent.RunResult{}, err
		}

		return e.mergeFileResult(result, stageWorkdir), nil
	}

	// Adapter fallback path.
	resolvedAdapter, resolvedName, err := e.resolveAdapter(task)
	if err != nil {
		return agent.RunResult{}, err
	}

	// Record which adapter was used.
	for i := range task.Runs {
		if task.Runs[i].StageID == stage.ID && task.Runs[i].FinishedAt == nil {
			task.Runs[i].Adapter = resolvedName
		}
	}

	// Build InvokeParams from RunInput for backward compat.
	contextArtifacts := make(map[string][]string)
	for _, po := range input.PriorOutputs {
		for _, ref := range po.Artifacts {
			contextArtifacts[po.StageID] = append(contextArtifacts[po.StageID], ref.Path)
		}
	}

	var rejectionFeedback *string
	var openQuestionAnswers *string
	if input.Feedback != nil {
		switch input.Feedback.Kind {
		case "rejection":
			rejectionFeedback = &input.Feedback.Text
		case "answer", "answers":
			openQuestionAnswers = &input.Feedback.Text
		}
	}

	r, err := resolvedAdapter.Invoke(ctx, domain.InvokeParams{
		AgentName:           stage.Agent,
		TaskDescription:     input.Goal,
		WorkingDir:          input.WorkspacePath,
		ContextArtifacts:    contextArtifacts,
		RejectionFeedback:   rejectionFeedback,
		OpenQuestionAnswers: openQuestionAnswers,
		StageWorkdir:        stageWorkdir,
		StageID:             stage.ID,
		Gate:                stage.Gate,
		PipelineName:        input.PipelineName,
		FixCycleCount:       input.FixCycleCount,
		PRIndex:             input.PRIndex,
		PreviousStdout:      input.PreviousStdout,
		PreviousStderr:      input.PreviousStderr,
		Model:               input.Model,
	})
	if err != nil {
		return agent.RunResult{}, err
	}

	status := agent.StatusFailed
	if r.Success {
		status = agent.StatusCompleted
	}

	return agent.RunResult{
		SchemaVersion:   agent.SchemaVersion,
		Status:          status,
		Stdout:          r.Stdout,
		Stderr:          r.Stderr,
		DurationSeconds: r.DurationSeconds,
	}, nil
}

// mergeFileResult reads result.json from stageWorkdir (if present) and merges
// its Verdict, OpenQuestions, and result_json_present metadata into result.
func (e *PipelineEngine) mergeFileResult(result agent.RunResult, stageWorkdir string) agent.RunResult {
	fileResult, ferr := agent.ReadStageResult(stageWorkdir)
	if ferr != nil || fileResult == nil {
		return result
	}
	if fileResult.Verdict != "" {
		result.Verdict = fileResult.Verdict
	}
	if len(fileResult.OpenQuestions) > 0 {
		result.OpenQuestions = fileResult.OpenQuestions
	}
	if result.Metadata == nil {
		result.Metadata = make(map[string]string)
	}
	result.Metadata["result_json_present"] = "true"
	return result
}

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
