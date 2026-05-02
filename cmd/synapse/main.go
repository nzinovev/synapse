package main

import (
	"fmt"
	"os"

	"github.com/nzinovev/synapse/internal/adapter"
	"github.com/nzinovev/synapse/internal/cli"
	"github.com/nzinovev/synapse/internal/domain"
)

var version = "dev"

func main() {
	registry := adapter.NewRegistry()

	cfg, err := domain.LoadConfigGlobal()
	if err != nil {
		fmt.Fprintf(os.Stderr, "No synapse configuration found. Run `synapse init` first: %v\n", err)
		os.Exit(1)
	}

	runner := cli.CreateRunner(cfg, nil) // nil deps for CLI mode

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
