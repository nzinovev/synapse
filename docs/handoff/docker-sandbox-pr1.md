# Handoff: docker-container-agents — PR1

**Branch:** docker-sandbox-pr1
**Date:** 2026-05-02

## What Was Merged

PR1 introduces the domain types, config changes, DB migrations, Runner abstraction, and adapter refactoring needed for Docker sandbox execution. The `Runner` interface (`internal/adapter/runner.go`) replaces direct `RunCLICommand` calls in adapters. `HostRunner` implements the existing host subprocess behavior. `DockerRunner` is a stub that returns an error — it will be implemented in PR2. Adapters (`claude_cli.go`, `cursor_cli.go`) now accept a `Runner` via their constructors and delegate execution to it. The `Dependencies` struct in `root.go` includes a `Runner` field; `createRunner()` selects between Docker and Host based on `SandboxMode`. DB migrations V5 (sandbox_mode on tasks) and V6 (container_info_json on stage_runs) are applied. Store layer persists and restores both fields. The `SandboxMode` default is `"docker"` but `HostRunner` is used in `main.go` until PR2 wires `createRunner` properly.

## DB State

- V5: `ALTER TABLE tasks ADD COLUMN sandbox_mode TEXT NOT NULL DEFAULT 'docker';`
- V6: `ALTER TABLE stage_runs ADD COLUMN container_info_json TEXT DEFAULT NULL;`

## Intentional Stubs / Incomplete Interfaces

| Stub | Location | What PR2 must do |
|------|----------|----------------------|
| `DockerRunner.Run()` | `internal/adapter/docker_runner.go` | Implement full Docker container lifecycle: create container with project mount, agent prompts, env vars, network policy, resource limits; start container; capture stdout/stderr; extract logs; remove container; return `AgentResult` with `ContainerInfo` populated |
| `createRunner()` in `root.go` | `internal/cli/root.go` | Currently `main.go` hardcodes `&HostRunner{}`. PR2 should use `createRunner()` which reads `cfg.AdapterConfig.SandboxMode` and creates a `DockerRunner` when docker mode is selected |
| `--no-sandbox` flag | `internal/cli/run.go` | Not yet added. PR2 adds the flag, prints warning, overrides sandbox mode to host |
| `init` sandbox prompt | `internal/cli/init_cmd.go` | Not yet added. PR2 adds sandbox mode question to `synapse init` |
| Docker image definitions | `docker/` directory | Not yet created. PR2 creates `docker/agent-claude/Dockerfile`, `docker/agent-cursor/Dockerfile`, `docker/build.sh` |
| Engine container events | `internal/engine/engine.go` | Not yet emitting container info in stage events. PR3 handles this |
| CLI `printTask` sandbox info | `internal/cli/run.go` | Not yet printing sandbox/container info. PR3 handles this |

## Gotchas Discovered

1. **Default mode is docker but HostRunner is used**: `main.go` creates `&HostRunner{}` directly, bypassing `createRunner()`. This means all tasks run on host despite `SandboxMode` defaulting to `"docker"`. PR2 must fix this by calling `createRunner()` or wiring the config-driven runner selection into the bootstrap.

2. **FakeAdapter doesn't use Runner**: `FakeAdapter` never calls subprocesses and doesn't need a `Runner`. Its `RegisterFake()` function signature was not changed — it continues to work without a runner. This is correct.

3. **Web server path**: The web server (`NewServerFromConfig`) creates its own engine with the registry. Since adapters are registered with runners at startup, the web path automatically uses whatever runner was injected. No additional changes needed for PR2 in `server.go`.

4. **`container_info_json` column**: The column stores JSON-serialized `ContainerInfo`. It's `NULL` for host-mode runs. The store layer handles serialization/deserialization correctly for both cases.

5. **Test fix**: `adapter_test.go:33` was updated to pass `&HostRunner{}` to `RegisterClaudeCLI` since the function signature changed.

## What PR2 Must Read Before Starting

- [ ] This file
- [ ] `docs/adr/001-docker-container-agents.md` — sections: Decision, Backend Plan (DockerRunner, Modified files), Docker Images, Risks & Open Questions
- [ ] `docs/specs/001-docker-container-agents.md` — sections: Acceptance Criteria, Detailed Implementation Notes
- [ ] `internal/adapter/runner.go` — Runner interface and RunnerParams struct
- [ ] `internal/adapter/docker_runner.go` — current stub
- [ ] `internal/adapter/host_runner.go` — reference implementation for Runner
- [ ] `internal/domain/sandbox.go` — DockerConfig defaults and ContainerInfo struct
- [ ] `internal/cli/root.go` — `createRunner()` function
