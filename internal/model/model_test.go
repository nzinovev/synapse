package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/nzinovev/synapse/internal/model"
)

// ---------------------------------------------------------------------------
// MockModel tests
// ---------------------------------------------------------------------------

func TestMockModel_ScriptedResponses_InOrder(t *testing.T) {
	resp1 := model.CompletionResponse{Content: "first", StopReason: "end_turn", ModelID: "m1"}
	resp2 := model.CompletionResponse{Content: "second", StopReason: "end_turn", ModelID: "m1"}

	m := &model.MockModel{
		Responses: []model.CompletionResponse{resp1, resp2},
	}

	ctx := context.Background()
	req := model.CompletionRequest{Messages: []model.Message{{Role: "user", Content: "hello"}}}

	got1, err := m.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got1.Content != "first" {
		t.Errorf("got content %q, want %q", got1.Content, "first")
	}

	got2, err := m.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got2.Content != "second" {
		t.Errorf("got content %q, want %q", got2.Content, "second")
	}

	// Third call: last response should be repeated.
	got3, err := m.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got3.Content != "second" {
		t.Errorf("got content %q, want %q (last repeated)", got3.Content, "second")
	}
}

func TestMockModel_ScriptedResponse_WithToolCalls(t *testing.T) {
	toolResp := model.CompletionResponse{
		StopReason: "tool_use",
		ToolCalls: []model.ToolCall{
			{
				ID:    "call-1",
				Name:  "read_file",
				Input: map[string]any{"path": "/tmp/test.txt"},
			},
		},
		Usage: model.TokenUsage{InputTokens: 10, OutputTokens: 5},
	}

	m := &model.MockModel{
		Responses: []model.CompletionResponse{toolResp},
	}

	got, err := m.Complete(context.Background(), model.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want %q", got.StopReason, "tool_use")
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", got.ToolCalls[0].Name, "read_file")
	}
}

func TestMockModel_RunFn_DelegatesAndRecords(t *testing.T) {
	called := 0
	m := &model.MockModel{
		RunFn: func(_ context.Context, req model.CompletionRequest) (model.CompletionResponse, error) {
			called++
			return model.CompletionResponse{Content: "dynamic"}, nil
		},
	}

	req := model.CompletionRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}}
	got, err := m.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "dynamic" {
		t.Errorf("content = %q, want %q", got.Content, "dynamic")
	}
	if called != 1 {
		t.Errorf("RunFn called %d times, want 1", called)
	}
	if len(m.Calls) != 1 {
		t.Errorf("Calls recorded %d, want 1", len(m.Calls))
	}
}

func TestMockModel_RunFn_ErrorPropagates(t *testing.T) {
	sentinel := errors.New("model error")
	m := &model.MockModel{
		RunFn: func(_ context.Context, _ model.CompletionRequest) (model.CompletionResponse, error) {
			return model.CompletionResponse{}, sentinel
		},
	}

	_, err := m.Complete(context.Background(), model.CompletionRequest{})
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want %v", err, sentinel)
	}
	// Call must still be recorded even on error.
	if len(m.Calls) != 1 {
		t.Errorf("Calls recorded %d, want 1", len(m.Calls))
	}
}

func TestMockModel_CallsRecorded(t *testing.T) {
	m := &model.MockModel{
		Responses: []model.CompletionResponse{
			{Content: "ok"},
		},
	}

	req1 := model.CompletionRequest{SystemPrompt: "system1"}
	req2 := model.CompletionRequest{SystemPrompt: "system2"}

	_, _ = m.Complete(context.Background(), req1)
	_, _ = m.Complete(context.Background(), req2)

	if len(m.Calls) != 2 {
		t.Fatalf("Calls = %d, want 2", len(m.Calls))
	}
	if m.Calls[0].SystemPrompt != "system1" {
		t.Errorf("Calls[0].SystemPrompt = %q", m.Calls[0].SystemPrompt)
	}
	if m.Calls[1].SystemPrompt != "system2" {
		t.Errorf("Calls[1].SystemPrompt = %q", m.Calls[1].SystemPrompt)
	}
}

func TestMockModel_EmptyResponses_ReturnsZero(t *testing.T) {
	m := &model.MockModel{}
	got, err := m.Complete(context.Background(), model.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "" {
		t.Errorf("expected zero-value response, got content %q", got.Content)
	}
}

// ---------------------------------------------------------------------------
// ConfigTierResolver tests
// ---------------------------------------------------------------------------

func makeSampleConfig() domain.AdapterConfig {
	return domain.AdapterConfig{
		ModelTiers: map[string]string{
			"low":    "haiku-3",
			"medium": "sonnet-3-5",
			"high":   "opus-3",
		},
	}
}

func TestConfigTierResolver_ResolvesAllTiers(t *testing.T) {
	cfg := makeSampleConfig()

	resolver := model.NewConfigTierResolver(cfg, func(modelName string) (model.Model, error) {
		return &model.MockModel{
			Responses: []model.CompletionResponse{{ModelID: modelName}},
		}, nil
	})

	for tier, wantModelID := range map[string]string{
		"low":    "haiku-3",
		"medium": "sonnet-3-5",
		"high":   "opus-3",
	} {
		m, err := resolver.Resolve(tier)
		if err != nil {
			t.Errorf("Resolve(%q) error: %v", tier, err)
			continue
		}
		resp, err := m.Complete(context.Background(), model.CompletionRequest{})
		if err != nil {
			t.Errorf("Complete after Resolve(%q) error: %v", tier, err)
			continue
		}
		if resp.ModelID != wantModelID {
			t.Errorf("tier %q: ModelID = %q, want %q", tier, resp.ModelID, wantModelID)
		}
	}
}

func TestConfigTierResolver_UnknownTier_ReturnsError(t *testing.T) {
	cfg := makeSampleConfig()
	resolver := model.NewConfigTierResolver(cfg, func(modelName string) (model.Model, error) {
		return &model.MockModel{}, nil
	})

	_, err := resolver.Resolve("ultra")
	if err == nil {
		t.Fatal("expected error for unknown tier, got nil")
	}
}

func TestConfigTierResolver_FactoryError_Propagates(t *testing.T) {
	cfg := makeSampleConfig()
	factoryErr := errors.New("factory failed")

	resolver := model.NewConfigTierResolver(cfg, func(_ string) (model.Model, error) {
		return nil, factoryErr
	})

	_, err := resolver.Resolve("low")
	if !errors.Is(err, factoryErr) {
		t.Errorf("error = %v, want %v", err, factoryErr)
	}
}

func TestConfigTierResolver_EmptyTierMap_AllFail(t *testing.T) {
	cfg := domain.AdapterConfig{ModelTiers: map[string]string{}}
	resolver := model.NewConfigTierResolver(cfg, func(_ string) (model.Model, error) {
		return &model.MockModel{}, nil
	})

	for _, tier := range []string{"low", "medium", "high"} {
		_, err := resolver.Resolve(tier)
		if err == nil {
			t.Errorf("expected error for tier %q with empty map, got nil", tier)
		}
	}
}
