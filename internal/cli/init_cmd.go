package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create ~/.synapse/config.json (global configuration).",
		RunE: func(cmd *cobra.Command, args []string) error {
			hubDir := domain.GetHubDir()
			configPath := filepath.Join(hubDir, "config.json")

			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("~/.synapse/config.json already exists. Use --force to overwrite")
			}

			fmt.Println("synapse init — initializing global configuration")
			fmt.Println()

			reader := bufio.NewReader(cmd.InOrStdin())

			adapterChoice := prompt(reader, "Default adapter [claude_cli/cursor_cli/fake]", "fake")
			adapterChoice = strings.ToLower(strings.TrimSpace(adapterChoice))
			if adapterChoice != "claude_cli" && adapterChoice != "cursor_cli" && adapterChoice != "fake" {
				return fmt.Errorf("unknown adapter %q. Choose claude_cli, cursor_cli, or fake", adapterChoice)
			}

			claudeBinary := prompt(reader, "Path to claude binary", "claude")
			agentBinary := prompt(reader, "Path to Cursor `agent` binary", "agent")

			defaultPromptsDir := filepath.Join(homeDir(), ".claude", "agents")
			agentPromptsDir := prompt(reader, "Path to agent .md files directory", defaultPromptsDir)

			cfg := domain.DefaultSynapseConfig()
			cfg.Adapter = adapterChoice
			cfg.AdapterConfig.ClaudeBinary = claudeBinary
			cfg.AdapterConfig.AgentBinary = agentBinary
			cfg.AdapterConfig.AgentPromptsDir = agentPromptsDir

			if err := cfg.SaveGlobal(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("\nCreated %s\n", configPath)
			fmt.Printf("  adapter:      %s\n", adapterChoice)
			fmt.Printf("  claude:       %s\n", claudeBinary)
			fmt.Printf("  agent:        %s\n", agentBinary)
			fmt.Printf("  prompts_dir:  %s\n", agentPromptsDir)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config.")
	return cmd
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
