# Synapse

Synapse is a multi-agent pipeline orchestrator. It runs sequences of AI coding agents (spec-writer → architect →
implementer → reviewer → fix-implementer) against a target project directory, with human approval gates between stages.

It is a thin coordinator: it does not contain AI logic itself. Each stage invokes an external agent (Claude Code CLI or
Cursor) with a structured prompt, collects artifacts, evaluates gate conditions, and either auto-advances or pauses for
human review.

## Prerequisites

- Go 1.25+
- At least one supported agent:
    - [Claude Code](https://claude.ai/code) CLI (`claude`) — for the `claude_cli` adapter
    - Cursor `agent` binary — for the `cursor_cli` adapter

## Install

```bash
make install
```

This builds and installs the `synapse` binary to `$HOME/.local/bin/synapse`. Make sure that directory is on your
`$PATH`.

To compile-check without installing:

```bash
go build ./...
```

## Configuration

Run the interactive setup wizard once:

```bash
synapse init
```

This creates `~/.synapse/config.json`. You will be prompted for:

- **Default adapter** — `claude_cli`, `cursor_cli`, or `fake` (for tests)
- **Path to the `claude` binary** — defaults to `claude`
- **Path to the Cursor `agent` binary** — defaults to `agent`
- **Agent prompts directory** — directory containing `<agent-name>.md` system prompt files; defaults to
  `~/.claude/agents`

You can re-run with `--force` to overwrite an existing config.

### Config reference (`~/.synapse/config.json`)

| Field                              | Default                                           | Description                                        |
|------------------------------------|---------------------------------------------------|----------------------------------------------------|
| `adapter`                          | `fake`                                            | Agent adapter to use: `claude_cli` or `cursor_cli` |
| `adapter_config.claude_binary`     | `claude`                                          | Path to Claude Code CLI                            |
| `adapter_config.agent_binary`      | `agent`                                           | Path to Cursor agent binary                        |
| `adapter_config.agent_prompts_dir` | `~/.claude/agents`                                | Directory with `<agent>.md` system prompts         |
| `adapter_config.cli_profile`       | `claude`                                          | Claude CLI profile name                            |
| `adapter_config.model_tiers`       | `{"low":"haiku","medium":"sonnet","high":"opus"}` | Maps tier names to model names                     |
| `db_path`                          | `~/.synapse/synapse.db`                           | SQLite database location                           |
| `worker_count`                     | `2`                                               | Parallel worker count for the web server           |
| `host`                             | `127.0.0.1`                                       | Web server bind address                            |
| `port`                             | `8765`                                            | Web server port                                    |

## Usage

### Run a pipeline

```bash
synapse run <pipeline> --task-number <N> "task description"
```

The task description can also be read from a file:

```bash
synapse run backend --task-number 42 --file task.md
```

By default the project directory is the current working directory. Override with `--project <path>`.

### Built-in pipelines

| Pipeline   | Stages                                            |
|------------|---------------------------------------------------|
| `backend`  | spec → adr → implement → review → fix → done      |
| `frontend` | spec → ui → adr → implement → review → fix → done |

### Approve or reject a gate

After a stage completes and pauses at a human gate:

```bash
synapse approve <task-id>
synapse reject  <task-id> --feedback "your feedback here"
```

Rejection sends feedback to the next agent invocation as context.

### Other commands

```bash
synapse status <task-id>          # show task status and run history
synapse list-pipelines            # list available pipelines
synapse show-pipeline <name>      # show pipeline stages
synapse retry <task-id>           # re-run the current stage
synapse answer <task-id> "..."    # answer open questions raised by an agent
synapse web                       # start the HTMX web UI on localhost:8765
synapse migrate                   # migrate a database from the Python predecessor
```

### Web UI

```bash
synapse web
```

Starts an HTMX web interface at `http://localhost:8765`. It exposes the same approve/reject/retry/answer actions through
a browser instead of the terminal, backed by a worker pool that processes tasks asynchronously.

### JSON output

Most commands accept `--json` for machine-readable output:

```bash
synapse run backend --task-number 1 "add login page" --json
synapse status <task-id> --json
```

## Gate semantics

| Gate               | Behavior                                                                                                                                            |
|--------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `auto`             | Always advances to the next stage on success                                                                                                        |
| `auto_if_clean`    | Advances automatically unless the agent produced a `QUESTIONS.md` or an `## Open Questions` section; then pauses                                    |
| `human_approval`   | Always pauses for human review                                                                                                                      |
| `human_final`      | Pauses; approve marks the task done                                                                                                                 |
| `auto_on_approval` | Reads a `**Verdict:** APPROVED / NEEDS FIXES / BLOCKED` line from the artifact; NEEDS FIXES re-runs the `fix` stage (up to 3 times), then escalates |

## Custom pipelines

Create a YAML file and set `pipelines_dir` in your config to point at its directory:

```yaml
name: my-pipeline
stages:
  - id: spec
    agent: spec-writer
    gate: auto_if_clean
    produces_glob: "docs/specs/*.md"
    model: low

  - id: implement
    agent: feature-implementer
    gate: human_final
    produces_glob: ""
    model: medium
```

`agent` is the filename (without `.md`) of the system prompt in `agent_prompts_dir`. `model` is a tier name (`low` /
`medium` / `high`) resolved via `model_tiers` in config.

## Limitations

- **Single-machine only.** There is no remote worker or distributed queue. The web server and all agent processes run on
  the same host.
- **No auth on the web UI.** The web server binds to `127.0.0.1` by default. Do not expose it to a network without
  adding your own authentication layer.
- **Sequential stages per task.** Stages within a task run one at a time. Multiple tasks can run in parallel (controlled
  by `worker_count`), but stages within one task are always sequential.
- **Fix cycle cap.** `auto_on_approval` gates will automatically re-run the fix stage at most 3 times before escalating
  to a human.
- **Adapter subprocess model.** Each stage launch spawns a new agent process. Long-running or expensive stages block the
  worker thread for their duration. There is no timeout enforcement built in.
- **No streaming output.** Agent stdout/stderr is captured and stored only after the subprocess exits. There is no
  real-time log streaming in the web UI.
- **Agent prompts are not bundled.** You must supply your own `<agent>.md` system prompt files. The pipeline stages
  reference agent names, but the prompt content is entirely yours to write and maintain.
- **SQLite only.** The store layer has no other backend; concurrent write throughput is limited to what SQLite WAL mode
  provides.

## Development

```bash
go test ./...                              # run all tests
go test ./internal/engine/... -run TestX  # run a specific test
go vet ./...                              # basic static checks
```

## License

See [LICENSE](LICENSE).
