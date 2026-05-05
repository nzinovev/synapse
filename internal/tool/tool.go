package tool

import "context"

// Tool is the interface that all callable tools must implement.
// The native runtime (T11) uses tools to give agents structured
// capabilities (filesystem access, search, user questions).
type Tool interface {
	Name() string
	Description() string
	// InputSchema returns a JSON Schema object describing the tool's input.
	InputSchema() map[string]any
	// Execute runs the tool with the given input and returns a result map or error.
	Execute(ctx context.Context, input map[string]any) (map[string]any, error)
}
