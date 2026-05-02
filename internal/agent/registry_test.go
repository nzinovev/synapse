package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/domain"
)

type fakeAgent struct {
	name string
}

func (f *fakeAgent) Name() string { return f.name }
func (f *fakeAgent) Run(_ context.Context, _ agent.RunInput) (agent.RunResult, error) {
	return agent.RunResult{}, nil
}

func fakeFactory(name string) func(domain.AdapterConfig) (agent.Agent, error) {
	return func(_ domain.AdapterConfig) (agent.Agent, error) {
		return &fakeAgent{name: name}, nil
	}
}

func TestAgentRegistry_RegisterAndCreate(t *testing.T) {
	r := agent.NewAgentRegistry()
	if err := r.Register("spec-writer", fakeFactory("spec-writer")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a, err := r.Create("spec-writer", domain.AdapterConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "spec-writer" {
		t.Errorf("got name %q, want %q", a.Name(), "spec-writer")
	}
}

func TestAgentRegistry_DuplicateRegistration(t *testing.T) {
	r := agent.NewAgentRegistry()
	_ = r.Register("spec-writer", fakeFactory("spec-writer"))
	err := r.Register("spec-writer", fakeFactory("spec-writer"))
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}
	if !strings.Contains(err.Error(), "spec-writer") {
		t.Errorf("error %q does not contain the agent name", err.Error())
	}
}

func TestAgentRegistry_UnknownName(t *testing.T) {
	r := agent.NewAgentRegistry()
	_ = r.Register("adr-architect", fakeFactory("adr-architect"))
	_ = r.Register("spec-writer", fakeFactory("spec-writer"))

	_, err := r.Create("foo", domain.AdapterConfig{})
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "foo") {
		t.Errorf("error %q does not contain missing name", msg)
	}
	if !strings.Contains(msg, "adr-architect") || !strings.Contains(msg, "spec-writer") {
		t.Errorf("error %q does not list registered agents", msg)
	}
}

func TestAgentRegistry_Has(t *testing.T) {
	r := agent.NewAgentRegistry()
	_ = r.Register("spec-writer", fakeFactory("spec-writer"))

	if !r.Has("spec-writer") {
		t.Error("Has returned false for registered agent")
	}
	if r.Has("missing") {
		t.Error("Has returned true for unregistered agent")
	}
}

func TestAgentRegistry_Names(t *testing.T) {
	r := agent.NewAgentRegistry()

	if names := r.Names(); len(names) != 0 {
		t.Errorf("expected empty slice, got %v", names)
	}

	_ = r.Register("spec-writer", fakeFactory("spec-writer"))
	_ = r.Register("adr-architect", fakeFactory("adr-architect"))

	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "adr-architect" || names[1] != "spec-writer" {
		t.Errorf("expected sorted order, got %v", names)
	}
}

func TestAgentRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	r := agent.NewAgentRegistry()
	_ = r.Register("spec-writer", fakeFactory("spec-writer"))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Create("spec-writer", domain.AdapterConfig{})
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = r.Register("adr-architect", fakeFactory("adr-architect"))
	}()

	wg.Wait()
}
