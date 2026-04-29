package cli

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newApproveCmd(deps *Dependencies) *cobra.Command {
	var adapterName string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "approve <task-id>",
		Short: "Approve the pending gate and continue the pipeline.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
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

			task, err := s.LoadTask(ctx, taskID)
			if err != nil {
				return fmt.Errorf("task %q not found", taskID)
			}

			if task.Status != domain.StatusAwaitingGate {
				return fmt.Errorf("task %q is not awaiting a gate (status=%s)", taskID, task.Status)
			}

			if adapterName != "" {
				if err := validateAdapterName(deps, adapterName); err != nil {
					return err
				}
				task.Adapter = adapterName
				if err := s.SaveTask(ctx, task); err != nil {
					return fmt.Errorf("failed to update task adapter: %w", err)
				}
			}

			eng, err := createEngine(ctx, cfg, s, deps)
			if err != nil {
				return err
			}

			if !jsonOutput {
				fmt.Printf("Approving gate for task %s...\n", taskID)
			}

			task, err = eng.Approve(ctx, taskID)
			if err != nil {
				return err
			}

			printTask(task, jsonOutput)
			return nil
		},
	}

	cmd.Flags().StringVar(&adapterName, "adapter", "", "Change adapter for the next stage run.")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Machine-readable JSON output.")
	return cmd
}
