package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newStatusCmd(deps *Dependencies) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status [task-id]",
		Short: "Show task status. Omit task-id to list all tasks.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			s, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer s.Close()

			if len(args) == 0 {
				tasks, err := s.ListTasks(ctx)
				if err != nil {
					return fmt.Errorf("failed to list tasks: %w", err)
				}

				if jsonOutput {
					data, _ := json.MarshalIndent(tasks, "", "  ")
					fmt.Println(string(data))
					return nil
				}

				if len(tasks) == 0 {
					fmt.Println("No tasks found.")
					return nil
				}

				printTaskTable(tasks)
			} else {
				taskID := args[0]
				task, err := s.LoadTask(ctx, taskID)
				if err != nil {
					return fmt.Errorf("task %q not found", taskID)
				}
				printTask(task, jsonOutput)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Machine-readable JSON output.")
	return cmd
}

func printTaskTable(tasks []*domain.Task) {
	fmt.Println()
	fmt.Printf("  %-35s %-12s %-12s %-14s %-12s %-30s %-16s\n", "ID", "Pipeline", "Stage", "Status", "Adapter", "Project", "Created")
	fmt.Println("  " + "--------------------------------------------------------------------------------------------------------------------------")

	for _, t := range tasks {
		projDisplay := t.WorkingDir
		parts := splitPath(projDisplay)
		if len(parts) > 3 {
			projDisplay = ".../" + filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
		}

		statusLabel := string(t.Status)
		adapterLabel := t.Adapter
		if adapterLabel == "" {
			adapterLabel = "-"
		}
		fmt.Printf("  %-35s %-12s %-12s %-14s %-12s %-30s %-16s\n",
			truncate(t.ID, 33),
			t.PipelineName,
			t.CurrentStageID,
			statusLabel,
			truncate(adapterLabel, 10),
			truncate(projDisplay, 28),
			t.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}

func splitPath(p string) []string {
	return filepath.SplitList(p)
}
