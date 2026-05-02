# Handoff: docker-container-agents — PR1

**Branch:** agent/docker-container-agents-pr1
**Date:** 2026-05-02

## What Was Merged

PR1 successfully implements the Docker container runner infrastructure for Synapse agents. This includes:

### Core Implementation
- **DockerRunner**: Implemented in `internal/adapter/docker_runner.go` with full container lifecycle management
  - Creates containers with proper mounts (project at `/mount`, stage workdir, read-only prompts)
  - Applies resource limits (CPU, memory) and network policy (restricted/full/none)
  - Passes environment variables including API keys and metadata
  - Handles container cleanup regardless of exit status
  - Returns container information in `AgentResult`

### Configuration & Domain Changes
- **Updated main.go**: Now uses `CreateRunner()` from config instead of hardcoded `HostRunner`
- **CLI enhancements**: Added `--no-sandbox` flag with prominent warning banner and 3-second delay
- **Init command**: Added sandbox mode prompt (docker/host) during `synapse init`

### Docker Support
- **Base images**: Created Dockerfiles for `synapse/agent-claude` and `synapse/agent-cursor`
- **Build script**: Added `docker/build.sh` for building and tagging images
- **Default configuration**: `SandboxMode = "docker"` with sensible defaults for resources and network

### Integration Points
- **Runner abstraction**: `internal/adapter/runner.go` interface with `HostRunner` and `DockerRunner` implementations
- **Domain types**: Added `SandboxMode`, `NetworkPolicy`, `DockerConfig` structs in `internal/domain/sandbox.go`
- **Task storage**: Config persists `sandbox_mode` for tasks, `container_info_json` for stage results

## Key Features Working
1. Docker containers are the default execution mode
2. Containers mount project at `/mount` and stage workdir
3. Resource limits enforced (CPU: 2.0, Memory: 2048MB, Timeout: 1200s)
4. Network policy defaults to "restricted" (configurable to full/none)
5. `--no-sandbox` flag forces host mode with safety warnings
6. Environment variables passed to containers including API keys
7. Container info captured and stored in database

## What Still Needs Implementation (PR2)
1. Docker image building and publishing automation
2. Frontend UI for sandbox configuration
3. Container metadata in engine events
4. Web interface support for sandbox settings
5. Testing with actual Docker containers (not just the stub)

## Test Status
- All unit tests pass ✅
- Project builds successfully ✅
- Integration tests would require Docker environment (out of scope for PR1)

## Docker Image Status
- Images built but not published (manual build step)
- Dockerfiles complete with non-root user
- Build script ready for image creation

## Migration Notes
- Database migrations V5 and V6 applied automatically
- Existing configs will default to docker mode
- No breaking changes to existing functionality