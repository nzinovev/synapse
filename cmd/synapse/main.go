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
	adapter.RegisterFake(registry)
	adapter.RegisterClaudeCLI(registry)
	adapter.RegisterCursorCLI(registry)

	deps := &cli.Dependencies{
		Registry: registry,
		Version:  version,
	}

	cmd := cli.NewRootCmd(deps)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
