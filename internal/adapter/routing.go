package adapter

import (
	"fmt"
	"strings"

	"github.com/nzinovev/synapse/internal/domain"
)

var pipelineOverview = "**Backend pipeline (reference):** spec-writer → adr-architect → feature-implementer → spec-reviewer → [fix-implementer → spec-reviewer]* → done. Fix loop: up to 3 cycles, then ESCALATED.\n\n**Global rules:** Do not write implementation code without a confirmed spec and a human-approved ADR. Read `CLAUDE.md` or `AGENTS.md` at the project root (walk parent dirs until found). Full routing: `docs/agent-routing.md` in the synapse repo (or your project copy)."

var stageCopy = map[string]string{
	"spec": "**Stage `spec` — agent `spec-writer`**\n\n" +
		"- **Produces:** `docs/specs/{slug}.md` (full specification). If the task is too ambiguous for testable acceptance criteria, write `docs/specs/QUESTIONS.md` and stop; set work aside for human input.\n" +
		"- **Gate:** `auto_if_clean` — orchestrator advances automatically only when `## Open Questions` is exactly `None at this time.`; otherwise you pause for human approval.\n" +
		"- **Forbidden:** implementation suggestions, class or package names, architectural choices.",

	"ui": "**Stage `ui` — agent `ui-designer`**\n\n" +
		"- **Produces:** UI specification under the path defined in project `AGENTS.md` / pipeline (e.g. `docs/ui-specs/{slug}.md`).\n" +
		"- **Forbidden:** implementation code; keep to UX/UI specification unless project docs say otherwise.",

	"adr": "**Stage `adr` — agent `adr-architect`**\n\n" +
		"- **Reads:** approved spec `docs/specs/{slug}.md`, existing ADRs in `docs/adr/`, relevant code via grep/glob.\n" +
		"- **Produces:** `docs/adr/{NNN}-{slug}.md` — ADR must include: exact class names and packages; full HTTP API (method, path, DTO fields/types); DB migration plan with ⚠️ on schema changes needing confirmation; delivery plan (PRs, branches, CI); at least two alternatives considered.\n" +
		"- **Gate:** always `human_approval` — humans approve or reject before implementation.\n" +
		"- **Forbidden:** any implementation source code.",

	"implement": "**Stage `implement` — agent `feature-implementer`**\n\n" +
		"- **Reads:** ADR is the governing document; spec is secondary; prior handoff `docs/handoff/{slug}-pr{n-1}.md` if this is PR2+.\n" +
		"- **Produces:** source, tests, migrations per ADR; `docs/handoff/{slug}-pr{n}.md` for non-final PRs before summary.\n" +
		"- **Gate:** `auto` — exit 0 to advance.\n" +
		"- **Stop without coding if:** ADR missing or not approved; ADR ambiguous; DB schema change or new dependency or auth/JWT change — report and wait for human confirmation per ADR/project rules.\n" +
		"- **Forbidden:** step-by-step comments in code; refactors outside ADR; new branch if one already exists for this PR; push to `main`/`master`/`develop`; force-push.",

	"review": "**Stage `review` — agent `spec-reviewer`**\n\n" +
		"- **Reads:** spec, ADR, changed files (`git diff` vs main), tests (e.g. `./mvnw test` when applicable).\n" +
		"- **Produces:** `docs/reviews/{slug}-review.md` with mandatory verdict line exactly one of:\n" +
		"  `**Verdict:** APPROVED` | `**Verdict:** NEEDS FIXES` | `**Verdict:** BLOCKED`\n" +
		"- **Gate:** `auto_on_approval` — APPROVED finishes the review step; NEEDS FIXES routes to fix (until fix_cycle limit); minors alone → APPROVED.\n" +
		"- **Forbidden:** modifying source, tests, migrations, or config files.",

	"fix": "**Stage `fix` — agent `fix-implementer`**\n\n" +
		"- **Primary input:** `docs/reviews/{slug}-review.md` — only address Critical/Major items; note Minors, do not fix them.\n" +
		"- **ADR:** reference only; do not violate it; if a finding conflicts with ADR, stop and report.\n" +
		"- **Uses existing PR branch only** — never create a new branch or force-push. Commit message pattern: `fix({scope}): address reviewer findings — cycle {n}`.\n" +
		"- **Gate:** `auto` — exit 0 to return to reviewer.\n" +
		"- **Stop if:** review missing; verdict is BLOCKED (not NEEDS FIXES); fix needs schema/dependency change — report first.",
}

func FormatRoutingBlock(stageID string, gate domain.Gate, agentName, pipelineName string, fixCycleCount, prIndex int) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("**Pipeline:** `%s`  ·  **Stage:** `%s`  ·  **Agent:** `%s`  ·  **Gate:** `%s`",
		pipelineName, stageID, agentName, string(gate)))

	if fixCycleCount > 0 {
		lines = append(lines, fmt.Sprintf(
			"**Fix loop:** `fix_cycle_count` = %d (incremented on NEEDS FIXES; pipeline escalates after 3 — see `docs/agent-routing.md`).",
			fixCycleCount))
	}

	if stageID == "implement" && prIndex > 1 {
		lines = append(lines, fmt.Sprintf(
			"**Delivery plan:** This is PR %d. Read the handoff file from PR %d listed under \"Context from prior stages\" before starting.",
			prIndex, prIndex-1))
	}

	lines = append(lines, "")
	lines = append(lines, strings.TrimSpace(pipelineOverview))
	lines = append(lines, "")

	if body, ok := stageCopy[stageID]; ok {
		lines = append(lines, strings.TrimSpace(body))
	} else {
		lines = append(lines, fmt.Sprintf(
			"**Custom stage `%s`** — follow `CLAUDE.md` / `AGENTS.md` and your role prompt file for `%s.md`. Respect gate `%s` when finishing this stage.",
			stageID, agentName, string(gate)))
	}

	return strings.Join(lines, "\n")
}
