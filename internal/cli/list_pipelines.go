package cli

import (
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newListPipelinesCmd(deps *Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "list-pipelines",
		Short: "List all available pipelines.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			var pipelines []*domain.Pipeline

			if cfg.PipelinesDir != "" {
				pipelines, _ = domain.ListPipelines(cfg.PipelinesDir)
			}

			bundled, err := domain.ListBundledPipelines()
			if err == nil {
				pipelines = mergePipelines(pipelines, bundled)
			}

			if len(pipelines) == 0 {
				fmt.Println("No pipelines found.")
				return nil
			}

			fmt.Println()
			fmt.Printf("  %-20s %-8s %s\n", "Name", "Stages", "Gates")
			fmt.Println("  " + "------------------------------------------------")

			for _, p := range pipelines {
				var gates []string
				for _, s := range p.Stages {
					if s.Gate != domain.GateAuto {
						gates = append(gates, s.ID+":"+string(s.Gate))
					}
				}
				gateStr := "—"
				if len(gates) > 0 {
					gateStr = gates[0]
					for i := 1; i < len(gates); i++ {
						gateStr += ", " + gates[i]
					}
				}
				fmt.Printf("  %-20s %-8d %s\n", p.Name, len(p.Stages), gateStr)
			}
			fmt.Println()
			return nil
		},
	}
}

func mergePipelines(fileBased, bundled []*domain.Pipeline) []*domain.Pipeline {
	seen := make(map[string]bool)
	var result []*domain.Pipeline
	for _, p := range fileBased {
		seen[p.Name] = true
		result = append(result, p)
	}
	for _, p := range bundled {
		if !seen[p.Name] {
			result = append(result, p)
		}
	}
	return result
}
