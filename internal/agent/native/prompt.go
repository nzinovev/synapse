package native

import (
	"fmt"
	"strings"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/model"
)

// buildMessages constructs the initial message slice for a model completion
// request from the given RunInput and AgentDefinition.
//
// Message layout:
//  1. A user message containing the system prompt from def.Prompt.
//  2. A user message with the task goal.
//  3. (Optional) A user message listing prior artifact paths.
//  4. (Optional) A user message with gate context.
//  5. (Optional) A user message with feedback text.
//  6. A final user message instructing the model to respond with JSON.
func buildMessages(input agent.RunInput, def agent.AgentDefinition) []model.Message {
	var msgs []model.Message

	// 1. System prompt from the agent definition.
	if def.Prompt != "" {
		msgs = append(msgs, model.Message{
			Role:    "user",
			Content: fmt.Sprintf("SYSTEM PROMPT:\n%s", def.Prompt),
		})
	}

	// 2. Task goal.
	if input.Goal != "" {
		msgs = append(msgs, model.Message{
			Role:    "user",
			Content: fmt.Sprintf("TASK GOAL:\n%s", input.Goal),
		})
	}

	// 3. Prior artifact paths from previous stages.
	if len(input.PriorOutputs) > 0 {
		var sb strings.Builder
		sb.WriteString("PRIOR OUTPUTS (read these files for context):\n")
		for _, po := range input.PriorOutputs {
			sb.WriteString(fmt.Sprintf("\nStage %q:\n", po.StageID))
			for _, ref := range po.Artifacts {
				sb.WriteString(fmt.Sprintf("  - %s\n", ref.Path))
			}
		}
		msgs = append(msgs, model.Message{
			Role:    "user",
			Content: sb.String(),
		})
	}

	// 4. Gate context.
	if input.Gate != "" {
		msgs = append(msgs, model.Message{
			Role:    "user",
			Content: fmt.Sprintf("GATE: %s", string(input.Gate)),
		})
	}

	// 5. Feedback from a previous rejection or question answers.
	if input.Feedback != nil && input.Feedback.Text != "" {
		msgs = append(msgs, model.Message{
			Role:    "user",
			Content: fmt.Sprintf("FEEDBACK (%s):\n%s", input.Feedback.Kind, input.Feedback.Text),
		})
	}

	// 6. Response format instruction.
	msgs = append(msgs, model.Message{
		Role: "user",
		Content: `Respond ONLY with a JSON object matching this schema (no markdown, no prose):
{
  "status": "completed" | "needs_input" | "failed",
  "summary": "<one-paragraph summary of what was done>",
  "verdict": "APPROVED" | "NEEDS FIXES" | "BLOCKED" | "",
  "open_questions": [{"id": "<id>", "text": "<question text>"}],
  "metadata": {},
  "artifacts": []
}`,
	})

	return msgs
}
