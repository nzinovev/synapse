package main

import (
	"fmt"
	"os"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/cli"
)

var version = "dev"

func main() {
	registry := adapter.NewRegistry()

	runner := adapter.Runner(&adapter.HostRunner{})

	adapter.RegisterFake(registry)
	adapter.RegisterClaudeCLI(registry, runner)
	adapter.RegisterCursorCLI(registry, runner)

	deps := &cli.Dependencies{
		Registry: registry,
		Version:  version,
		Runner:   runner,
	}

	cmd := cli.NewRootCmd(deps)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
