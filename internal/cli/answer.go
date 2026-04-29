package cli

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newAnswerCmd(deps *Dependencies) *cobra.Command {
	var feedback string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "answer <task-id>",
		Short: "Provide answers to open questions and re-run the current stage.",
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

			eng, err := createEngine(ctx, cfg, s, deps)
			if err != nil {
				return err
			}

			if !jsonOutput {
				fmt.Printf("Answering open questions for task %s...\n", taskID)
			}

			task, err = eng.Answer(ctx, taskID, feedback)
			if err != nil {
				return err
			}

			printTask(task, jsonOutput)
			return nil
		},
	}

	cmd.Flags().StringVarP(&feedback, "feedback", "f", "", "Answers to the open questions (required).")
	cmd.MarkFlagRequired("feedback")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Machine-readable JSON output.")
	return cmd
}
