package model

import "context"

// MockModel is a test double for Model that returns scripted responses.
// If RunFn is set, every call is delegated to it. Otherwise responses are
// dequeued from Responses in order; the last element is repeated once
// exhausted. All calls are recorded in Calls for assertion.
type MockModel struct {
	Responses []CompletionResponse
	RunFn     func(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	Calls     []CompletionRequest
}

// Complete records the request in Calls, then either delegates to RunFn or
// returns the next scripted response.
func (m *MockModel) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	m.Calls = append(m.Calls, req)

	if m.RunFn != nil {
		return m.RunFn(ctx, req)
	}

	if len(m.Responses) == 0 {
		return CompletionResponse{}, nil
	}

	// Dequeue the first response; keep the last one for all subsequent calls.
	resp := m.Responses[0]
	if len(m.Responses) > 1 {
		m.Responses = m.Responses[1:]
	}
	return resp, nil
}
