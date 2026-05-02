# Handoff: docker-container-agents — PR3

**Branch:** agent/docker-container-agents-pr3
**Date:** 2026-05-02

## What Was Merged

PR3 adds observability for container execution: the engine now emits a dedicated event when a stage completes inside a Docker container (with container metadata), the CLI `printTask` function shows sandbox mode and container details, and the `ContainerInfo` store round-trip is verified with a test.

- **Engine container info event**: After `EventStageCompleted`, if `result.ContainerInfo != nil`, the engine emits an `EventAgentOutput` event with `container_id`, `image`, `network_policy`, `cpu_limit`, and `memory_limit_mb` in the metadata map.
- **`printTask` sandbox output**: `printTask` now shows `Sandbox: docker (restricted)` or `Sandbox: host (no isolation)`. When a completed run has `ContainerInfo`, it also shows `Image`, `Container` (12-char truncated ID), `Network`, and `Resources` lines.
- **`TestContainerInfoRoundTrip`**: Verifies that `ContainerInfo` with all fields (container ID, image, network policy, CPU limit, memory limit) survives `CreateTask` → `LoadTask` → `SaveTask` → `LoadTask` cycle. Also verifies that runs without `ContainerInfo` load back as `nil`.

## DB State

No schema changes in this PR. Migrations V5 (`sandbox_mode`) and V6 (`container_info_json`) from PR1 remain the latest.

## Intentional Stubs / Incomplete Interfaces

| Stub | Location | What PR4 must do |
|------|----------|------------------|
| No sandbox warning banner in web UI | `internal/web/templates/task.html` | Add a `SandboxWarningBanner` between header and main content when `SandboxMode == "host"`. Use `role="alert"`, `aria-live="assertive"`, red left border, warning icon. See UI spec. |
| No container info row in web UI | `internal/web/templates/task.html` | Add a `ContainerInfoRow` in the Description card after "Created" row when `SandboxMode == "docker"`. Show truncated container ID (monospace), image name, network policy badge. |
| No sandbox badge in stage cards | `internal/web/templates/stage_card.html` | Add sandbox badge in `.pipeline-footer`: green for docker, red for host. |
| No sandbox settings in new task form | `internal/web/templates/new_task.html`, `index.html` | Add collapsible "Sandbox Settings" section below working_dir field. Fields: network policy select, CPU limit input, memory limit input. Use `<details>`/`<summary>`. |
| No container metadata in stage logs | `internal/web/templates/stage_logs.html` | Add container metadata row in attempt header when run used docker. |
| No sandbox-related CSS | `internal/web/static/style.css` | Add `.sandbox-warning-banner` styles, `.network-badge-*` variants, `.advanced-section` collapsible styles. |
| `handleCreateTask` doesn't accept sandbox fields | `internal/web/handlers.go` | Accept `sandbox_mode`, `network_policy`, `cpu_limit`, `memory_limit_mb` form fields and pass to task. |
| `handleTaskDetail` template data lacks sandbox context | `internal/web/handlers.go` | `Task.SandboxMode` and `Task.Runs[].AgentResult.ContainerInfo` are already available on the `Task` object — no handler changes needed beyond what `handleCreateTask` requires. |

## Gotchas Discovered

- **`printTask` iterates runs backward to find ContainerInfo**: Since only Docker runs produce `ContainerInfo`, the loop scans from the most recent run backward to find the first run with container data. This correctly shows the latest container info even if multiple stages ran.
- **Engine event uses `EventAgentOutput` kind**: Container info is emitted as an `EventAgentOutput` event (not a new event type) to avoid adding a new `EventKind`. The metadata map carries all container fields, making it parseable by consumers.

## What PR4 Must Read Before Starting

- [ ] This file
- [ ] `docs/adr/001-docker-container-agents.md` — section "Frontend Plan"
- [ ] `docs/specs/001-docker-container-agents.md` — sections on frontend impact
- [ ] `internal/web/handlers.go` — `handleTaskDetail` (template data), `handleCreateTask` (form parsing)
- [ ] `internal/web/templates/task.html` — current task detail layout
- [ ] `internal/web/templates/new_task.html` — current new task form
- [ ] `internal/web/templates/stage_card.html` — current stage card layout
- [ ] `internal/web/templates/stage_logs.html` — current stage logs layout
- [ ] `internal/web/static/style.css` — current styles
