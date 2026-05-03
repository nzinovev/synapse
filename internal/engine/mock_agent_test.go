package engine

import (
	"context"

	"github.com/nzinovev/synapse/internal/agent"
)

type MockAgent struct {
	RunResult agent.RunResult
	RunError  error
	LastInput agent.RunInput
	RunCalls  int
}

func (m *MockAgent) Name() string { return "mock" }

func (m *MockAgent) Run(ctx context.Context, input agent.RunInput) (agent.RunResult, error) {
	m.RunCalls++
	m.LastInput = input
	return m.RunResult, m.RunError
}
