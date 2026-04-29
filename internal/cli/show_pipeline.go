package cli

import (
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newShowPipelineCmd(deps *Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "show-pipeline <name>",
		Short: "Pretty-print a pipeline definition.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			pipeline, err := domain.ResolvePipeline(name, cfg.PipelinesDir)
			if err != nil {
				return err
			}

			fmt.Printf("\nPipeline: %s\n", pipeline.Name)
			fmt.Println()
			fmt.Printf("  %-4s %-16s %-20s %-20s %s\n", "#", "Stage ID", "Agent", "Gate", "Produces")
			fmt.Println("  " + "-------------------------------------------------------------------------")

			for i, stage := range pipeline.Stages {
				agent := stage.Agent
				if agent == "" || agent == "null" {
					agent = "—"
				}
				produces := stage.ProducesGlob
				if produces == "" {
					produces = "—"
				}
				fmt.Printf("  %-4d %-16s %-20s %-20s %s\n", i+1, stage.ID, agent, string(stage.Gate), produces)
			}
			fmt.Println()
			return nil
		},
	}
}
