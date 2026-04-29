package cli

import (
	"context"
	"fmt"

	"github.com/nzinovev/synapse/internal/store"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-python <path>",
		Short: "Import Python .synapse/tasks/ into SQLite.",
		Long:  "Reads Python state.json and events.jsonl files from <path>/.synapse/tasks/ and imports them into the SQLite database.",
		Args:  cobra.ExactArgs(1),
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

			pythonTasksDir := args[0]
			fmt.Printf("Importing tasks from %s ...\n", pythonTasksDir)

			imported, err := store.ImportFromPython(ctx, s, pythonTasksDir)
			if err != nil {
				return err
			}

			fmt.Printf("Done. Imported %d task(s).\n", imported)
			return nil
		},
	}

	return cmd
}
