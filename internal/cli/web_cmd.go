package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nzinovev/synapse/internal/web"
)

func newWebCmd(deps *Dependencies) *cobra.Command {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the web dashboard.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if host != "" {
				cfg.Host = host
			}
			if port != 0 {
				cfg.Port = port
			}

			srv, err := web.NewServerFromConfig(cfg, deps.Registry)
			if err != nil {
				return fmt.Errorf("create server: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Starting synapse dashboard at http://%s:%d\n", cfg.Host, cfg.Port)

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				<-sigCh
				fmt.Fprintln(cmd.OutOrStdout(), "\nShutting down...")
				os.Exit(0)
			}()

			return srv.Start(cfg.Host, cfg.Port)
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "Host to bind the web server (default from config).")
	cmd.Flags().IntVar(&port, "port", 0, "Port to bind the web server (default from config).")
	return cmd
}
