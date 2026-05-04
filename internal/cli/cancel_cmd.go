package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newCancelCmd(deps *Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <task-id>",
		Short: "Cancel a running or awaiting-gate task.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			ctx := context.Background()
			s, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer s.Close()

			eng, err := createEngine(ctx, cfg, s, deps)
			if err != nil {
				return err
			}

			task, err := eng.Cancel(ctx, taskID)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Task %s cancelled (stage: %s)\n", task.ID, task.CurrentStageID)
			return nil
		},
	}
}
