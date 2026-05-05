package agent

import "context"

// MockAgent is a test double for the Agent interface.
type MockAgent struct {
	NameFn func() string
	RunFn  func(ctx context.Context, input RunInput) (RunResult, error)
}

func (m *MockAgent) Name() string {
	if m.NameFn != nil {
		return m.NameFn()
	}
	return "mock"
}

func (m *MockAgent) Run(ctx context.Context, input RunInput) (RunResult, error) {
	if m.RunFn != nil {
		return m.RunFn(ctx, input)
	}
	return RunResult{
		SchemaVersion: SchemaVersion,
		Status:        StatusCompleted,
	}, nil
}
