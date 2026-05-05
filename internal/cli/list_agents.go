package cli

import (
	"fmt"

	"github.com/nzinovev/synapse/internal/agent"
	"github.com/spf13/cobra"
)

func newListAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-agents",
		Short: "List all available agent definitions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			agents, err := agent.ListAgents(*cfg)
			if err != nil {
				return fmt.Errorf("list agents: %w", err)
			}

			if len(agents) == 0 {
				fmt.Println("No agents found.")
				return nil
			}

			fmt.Println()
			fmt.Printf("  %-25s %-10s %-10s %s\n", "NAME", "RUNTIME", "SOURCE", "DESCRIPTION")
			fmt.Println("  " + "--------------------------------------------------------------")

			for _, a := range agents {
				desc := a.Description
				if desc == "" {
					desc = "-"
				}
				fmt.Printf("  %-25s %-10s %-10s %s\n", a.Name, a.Runtime, a.Source, desc)
			}
			fmt.Println()
			return nil
		},
	}
}
