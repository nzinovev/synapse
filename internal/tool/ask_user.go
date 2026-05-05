package tool

import "context"

// askUserTool emits a structured question back to the native runtime caller.
// The runtime (T11) is responsible for surfacing this as an agent.Question
// in RunResult.OpenQuestions.
type askUserTool struct{}

// NewAskUserTool returns a Tool that encodes a question for the runtime to
// surface to the user. It does not prompt interactively.
func NewAskUserTool() Tool {
	return &askUserTool{}
}

func (t *askUserTool) Name() string { return "ask_user" }

func (t *askUserTool) Description() string {
	return "Emit a structured question to the user. " +
		"The runtime will pause and surface the question; the answer will be " +
		"provided in the next invocation via RunInput.Feedback."
}

func (t *askUserTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question_id": map[string]any{
				"type":        "string",
				"description": "Stable identifier for the question (used to correlate answers).",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "The question text to present to the user.",
			},
		},
		"required": []string{"question_id", "text"},
	}
}

// Execute returns the question payload unchanged so the runtime can record it
// as an OpenQuestion in RunResult.
func (t *askUserTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	questionID, err := stringField(input, "question_id")
	if err != nil {
		return nil, err
	}
	text, err := stringField(input, "text")
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"question_id": questionID,
		"text":        text,
	}, nil
}
