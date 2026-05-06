package domain

// InvokeParams holds the parameters passed to an AgentAdapter.Invoke call.
type InvokeParams struct {
	AgentName           string
	TaskDescription     string
	WorkingDir          string
	ContextArtifacts    map[string][]string
	RejectionFeedback   *string
	OpenQuestionAnswers *string
	StageWorkdir        string
	StageID             string
	Gate                Gate
	PipelineName        string
	FixCycleCount       int
	PRIndex             int
	PreviousStdout      *string
	PreviousStderr      *string
	Model               string
	SystemPrompt        string // inline prompt text; when non-empty, adapters use this instead of reading from disk
}
