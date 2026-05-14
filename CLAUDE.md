# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Install

```bash
make install          # builds and installs to $HOME/.local/bin/synapse
go build ./...        # compile-check all packages
go test ./...         # run all tests
go test ./internal/engine/... -run TestName  # run a single test
```

No linter is configured; `go vet ./...` is available for basic checks.

## Architecture

Synapse is a multi-agent pipeline orchestrator. It runs sequences of AI agent stages (spec-writer → adr-architect →
feature-implementer → reviewer → fix-implementer) against a target project directory, with human approval gates between
stages.

### Data flow

1. `synapse run <pipeline> --task-number N "description"` creates a `Task` in SQLite and immediately calls
   `engine.RunUntilGate`.
2. `synapse web` starts the HTTP server, which uses a `WorkerPool` to process queue items asynchronously — CLI commands
   enqueue actions (`run`, `approve`, `reject`, `retry`, `answer`) and the workers call the engine.
3. The engine (`internal/engine/engine.go`) loops through pipeline stages, invoking the configured adapter for each
   stage, and stops when a gate blocks or all stages complete.

### Key packages

| Package            | Responsibility                                                                                                                                                                                       |
|--------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `internal/domain`  | Core types: `Task`, `Pipeline`, `Stage`, `Gate`, `SynapseConfig`. Config is read from `~/.synapse/config.json` or a `.synapse/` dir walked up from CWD.                                              |
| `internal/engine`  | `PipelineEngine` — executes the run loop, evaluates gate logic, emits events. `MaxFixCycles = 3`.                                                                                                    |
| `internal/adapter` | `AgentAdapter` interface + registry. Implementations: `claude_cli` (Claude Code CLI), `cursor_cli` (Cursor), `fake` (tests). Each adapter builds a prompt from `InvokeParams` and runs a subprocess. |
| `internal/store`   | `TaskStore` interface + SQLite implementation. WAL mode, single write connection. Migrations in `migrations.go`.                                                                                     |
| `internal/queue`   | SQLite-backed work queue. `WorkerPool` dequeues items and dispatches to the engine. Used only by the web server path.                                                                                |
| `internal/cli`     | Cobra commands. CLI path calls engine directly (blocking); web path enqueues.                                                                                                                        |
| `internal/web`     | HTMX-based web UI with Go templates. Handlers in `handlers.go`, routes in `server.go`.                                                                                                               |

### Pipelines

Pipelines are YAML files loaded from `cfg.PipelinesDir` (user-configured) or from the bundled FS (
`internal/domain/pipelines/`). The `embed.go` file wires `PipelinesFS`. Bundled pipelines: `backend`, `frontend`.

Each stage has:

- `agent` — name mapped to a role prompt file (`<agent_prompts_dir>/<agent>.md`) read by the adapter
- `gate` — controls auto-advance vs human pause: `auto`, `auto_if_clean`, `human_approval`, `human_final`,
  `auto_on_approval`
- `produces_glob` — glob pattern relative to the project dir; new files since stage start become artifacts
- `model` — tier name (`low`/`medium`/`high`) resolved to a real model name via `AdapterConfig.ModelTiers`

### Gate semantics

- `auto` — always advances to next stage on success
- `auto_if_clean` — advances automatically unless artifacts contain open questions (QUESTIONS.md or `## Open Questions`
  section); otherwise pauses for human approval
- `human_approval` / `human_final` — always pauses; `human_final` marks done on approve, routes to `fix` stage on reject
- `auto_on_approval` — parses a `**Verdict:** APPROVED | NEEDS FIXES | BLOCKED` line from the stage's artifact; NEEDS
  FIXES routes to `fix` stage (up to `MaxFixCycles = 3`), then escalates

### Adapter routing block

`internal/adapter/routing.go:FormatRoutingBlock` prepends a structured block to every agent invocation describing the
current pipeline/stage/gate and per-stage instructions. This is how agents know their role and constraints without
reading a separate file.

### Stage Permissions

Each stage may declare a `permissions:` block controlling what the native agent is
allowed to do. CLI adapters are trusted and always skip post-stage enforcement.

| Field | Type | Default (native) | Default (cli) | Description |
|-------|------|-----------------|---------------|-------------|
| `allow_writes` | bool | false | true | Permit filesystem writes |
| `allow_shell` | bool | false | true | Permit shell execution |
| `blocked_paths` | []string | [] | [] | Glob/prefix patterns always denied |
| `max_changed_files` | int | 0 (unlimited) | 0 (unlimited) | Max files changed per stage run |

Engine enforces `allow_writes`, `max_changed_files`, and `blocked_paths` via git-status
diff for native agents. Tool-level enforcement (write_file, shell) runs inline.

### Config location

- Global: `~/.synapse/config.json`
- Project-scoped: `.synapse/config.json` walked up from CWD (`domain.FindSynapseDir`)
- Initialize with `synapse init`
