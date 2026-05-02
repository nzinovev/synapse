# Handoff: docker-container-agents — PR2

**Branch:** agent/docker-container-agents-pr2
**Date:** 2026-05-02

## What Was Merged

PR2 completes the DockerRunner implementation with proper container lifecycle management. Key changes:

- **DockerRunner rewritten**: Two-step lifecycle (`docker create` → `docker start -a`) with buffer-based output capture matching the `HostRunner` pattern. Returns proper `AgentResult` with `Stdout`, `Stderr`, `ExitCode`, `DurationSeconds`, and `ContainerInfo`.
- **Output capture**: DockerRunner now captures stdout/stderr into buffers (not `os.Stdout`/`os.Stderr`), writes log files to stage workdir, checks for `SYNAPSE_AGENT_DONE` marker, and truncates output at `maxOutputBytes` — matching `RunCLICommand` behavior exactly.
- **Prompt files mount**: Agent prompts directory mounted read-only at `/prompts` inside the container.
- **macOS performance**: Project mount uses `:cached` flag on macOS for VirtioFS performance.
- **Timeout handling**: On timeout, container is explicitly killed before deferred removal (no race condition).
- **Environment variables**: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` forwarded from host. `SYNAPSE_TASK_ID`, `SYNAPSE_STAGE_ID`, `SYNAPSE_PIPELINE_NAME`, `SYNAPSE_AGENT` set from invocation params.
- **RunnerParams updated**: Replaced `SandboxConfig` and `PromptFiles` fields with `TaskID`, `StageID`, `PipelineName`, `PromptDir` — simpler and more directly useful.
- **InvokeParams updated**: Added `TaskID` field so adapters can pass it to the runner.
- **Engine updated**: Passes `TaskID` in `InvokeParams`.

## DB State

No schema changes in this PR. Migrations V5 (`sandbox_mode`) and V6 (`container_info_json`) from PR1 remain the latest.

## Intentional Stubs / Incomplete Interfaces

| Stub | Location | What PR3 must do |
|------|----------|------------------|
| Engine events for container info | `internal/engine/engine.go` | After stage completion, emit an event with container metadata (`container_id`, `image`) when `result.ContainerInfo != nil`. See ADR section "Modified file: internal/engine/engine.go" for exact code. |
| `printTask` sandbox/container output | `internal/cli/run.go` | Add sandbox mode line (`Sandbox: docker (restricted)` or `Sandbox: host (no isolation)`) and container info lines (Container ID, Network, Resources) to `printTask` function. |
| Container info not yet surfaced to store | Already persisted via `container_info_json` column | Verify that the `insertStageRunInTx` and `loadStageRuns` correctly round-trip `ContainerInfo` JSON. The code exists from PR1 but hasn't been tested with real Docker containers. |

## Gotchas Discovered

- **DockerRunner `docker create` returns container ID on stdout**: The container ID is captured from `createCmd.Output()`, not from the `--name` flag. Both work, but the returned ID is authoritative.
- **`RunnerParams` simplified**: The original ADR had `SandboxConfig domain.DockerConfig` and `PromptFiles map[string]string` in `RunnerParams`. These were replaced with `PromptDir string` (the whole directory is mounted, not individual files) and the DockerRunner uses its own config (injected at construction). This is cleaner because the runner already knows its config.
- **Network `restricted` mode**: V1 uses Docker's default bridge network with no additional restrictions. `AllowedEndpoints` in config is accepted but not enforced. This is a known limitation documented in the ADR.

## What PR3 Must Read Before Starting

- [ ] This file
- [ ] `docs/adr/001-docker-container-agents.md` — sections: "Modified file: internal/engine/engine.go", "Modified file: internal/cli/run.go"
- [ ] `internal/engine/engine.go` — the `runLoop` function around line 615 where `EventStageCompleted` is emitted
- [ ] `internal/cli/run.go` — the `printTask` function
- [ ] `internal/adapter/docker_runner.go` — the `ContainerInfo` struct returned in `AgentResult`
