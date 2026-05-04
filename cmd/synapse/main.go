package main

import (
	"fmt"
	"os"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/agent"
	"github.com/nzinovev/synapse/internal/agent/cliagent"
	"github.com/nzinovev/synapse/internal/cli"
)

var version = "dev"

func main() {
	registry := adapter.NewRegistry()
	adapter.RegisterClaudeCLI(registry)
	adapter.RegisterCursorCLI(registry)

	agentRegistry := agent.NewAgentRegistry()
	if err := cliagent.RegisterClaudeCLI(agentRegistry); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := cliagent.RegisterCursorCLI(agentRegistry); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	deps := &cli.Dependencies{
		Registry:      registry,
		AgentRegistry: agentRegistry,
		Version:       version,
	}

	cmd := cli.NewRootCmd(deps)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
