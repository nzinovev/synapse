package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzinovev/synapse/internal/domain"
	"github.com/spf13/cobra"
)

func newRunCmd(deps *Dependencies) *cobra.Command {
	var file string
	var project string
	var adapterName string
	var jsonOutput bool
	var taskNumber string
	var noSandbox bool

	cmd := &cobra.Command{
		Use:   "run <pipeline> [description]",
		Short: "Create and run a new task until the first gate.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pipelineName := args[0]
			var description string

			if len(args) > 1 {
				description = strings.Join(args[1:], " ")
			}

			if file != "" && description != "" {
				return fmt.Errorf("provide either an inline description or --file, not both")
			}
			if file != "" {
				data, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				description = string(data)
			}
			description = strings.TrimSpace(description)
			if description == "" {
				return fmt.Errorf("no description provided. Pass it inline or use --file <path>")
			}

			if taskNumber == "" {
				return fmt.Errorf("--task-number is required")
			}

			if adapterName != "" {
				if err := validateAdapterName(deps, adapterName); err != nil {
					return err
				}
			}

			projectDir := project
			if projectDir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get working directory: %w", err)
				}
				projectDir = wd
			}
			absProject, err := filepath.Abs(projectDir)
			if err != nil {
				return fmt.Errorf("failed to resolve project path: %w", err)
			}
			info, err := os.Stat(absProject)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("project directory does not exist: %s", absProject)
			}

			ctx := context.Background()
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Handle --no-sandbox flag
			if noSandbox {
				printNoSandboxWarning()
				time.Sleep(3 * time.Second)
				cfg.AdapterConfig.SandboxMode = domain.SandboxHost
			}

			s, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer s.Close()

			pipeline, err := domain.ResolvePipeline(pipelineName, cfg.PipelinesDir)
			if err != nil {
				return err
			}

			eng, err := createEngine(ctx, cfg, s, deps)
			if err != nil {
				return err
			}

			firstStage := pipeline.Stages[0]
			now := time.Now().UTC()

			var task *domain.Task
			var taskID string
			for suffix := 0; suffix <= 9; suffix++ {
				taskID = domain.GenerateTaskID(taskNumber, now, suffix)
				task = &domain.Task{
					ID:             taskID,
					PipelineName:   pipelineName,
					Description:    description,
					WorkingDir:     absProject,
					CurrentStageID: firstStage.ID,
					Status:         domain.StatusRunning,
					Artifacts:      make(map[string][]string),
					Adapter:        adapterName,
					SandboxMode:    cfg.AdapterConfig.SandboxMode,
				}
				if err := s.CreateTask(ctx, task); err != nil {
					if domain.IsDuplicateIDError(err) {
						continue
					}
					return fmt.Errorf("failed to create task: %w", err)
				}
				break
			}
			if task == nil {
				return fmt.Errorf("failed to create task: could not generate unique ID after 10 attempts")
			}

			if !jsonOutput {
				fmt.Printf("\nCreated task %s\n", taskID)
				fmt.Printf("  Pipeline: %s\n", pipelineName)
				fmt.Printf("  Project:  %s\n", absProject)
				fmt.Printf("  Stage:    %s\n\n", firstStage.ID)
			}

			task, err = eng.RunUntilGate(ctx, taskID)
			if err != nil {
				return fmt.Errorf("error running task: %w", err)
			}

			printTask(task, jsonOutput)
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Read task description from a file.")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project directory (defaults to CWD).")
	cmd.Flags().StringVarP(&taskNumber, "task-number", "n", "", "Task number (required).")
	cmd.Flags().StringVar(&adapterName, "adapter", "", "Agent adapter to use (e.g., claude_cli, cursor_cli). Defaults to global config.")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Machine-readable JSON output.")
	cmd.Flags().BoolVar(&noSandbox, "no-sandbox", false, "Run agent directly on host without Docker sandbox (WARNING: reduces security)")
	return cmd
}

func printNoSandboxWarning() {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "╔══════════════════════════════════════════════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║ WARNING: Running agent directly on host without Docker sandbox             ║")
	fmt.Fprintln(os.Stderr, "║                                                                            ║")
	fmt.Fprintln(os.Stderr, "║ This gives the agent full access to your machine including:                 ║")
	fmt.Fprintln(os.Stderr, "║ • Your home directory and all its contents                                  ║")
	fmt.Fprintln(os.Stderr, "║ • SSH keys, shell history, and environment variables                       ║")
	fmt.Fprintln(os.Stderr, "║ • Full filesystem access                                                    ║")
	fmt.Fprintln(os.Stderr, "║ Network connectivity to external services                                    ║")
	fmt.Fprintln(os.Stderr, "║                                                                            ║")
	fmt.Fprintln(os.Stderr, "║ For better security, use Docker containers (the default mode).              ║")
	fmt.Fprintln(os.Stderr, "║                                                                            ║")
	fmt.Fprintln(os.Stderr, "╚══════════════════════════════════════════════════════════════════════════════╝")
	fmt.Fprintln(os.Stderr, "")
}

func printTask(task *domain.Task, asJSON bool) {
	if asJSON {
		data, _ := json.MarshalIndent(task, "", "  ")
		fmt.Println(string(data))
		return
	}

	statusLabel := string(task.Status)
	if task.Status == domain.StatusEscalated {
		statusLabel = "escalated — max fix cycles reached, human required"
	}

	fmt.Printf("  Task:     %s\n", task.ID)
	fmt.Printf("  Pipeline: %s\n", task.PipelineName)
	fmt.Printf("  Stage:    %s\n", task.CurrentStageID)
	fmt.Printf("  Status:   %s\n", statusLabel)
	fmt.Printf("  Project:  %s\n", task.WorkingDir)
	fmt.Printf("  Created:  %s\n", task.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("  Updated:  %s\n", task.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("  Runs:     %d\n", len(task.Runs))

	if task.Adapter != "" {
		fmt.Printf("  Adapter:  %s\n", task.Adapter)
	}

	switch task.SandboxMode {
	case domain.SandboxDocker:
		fmt.Printf("  Sandbox:  docker (restricted)\n")
	case domain.SandboxHost:
		fmt.Printf("  Sandbox:  host (no isolation)\n")
	}

	// Show container info from the most recent completed run.
	for i := len(task.Runs) - 1; i >= 0; i-- {
		run := task.Runs[i]
		if run.AgentResult != nil && run.AgentResult.ContainerInfo != nil {
			ci := run.AgentResult.ContainerInfo
			shortID := ci.ContainerID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			fmt.Printf("  Image:    %s\n", ci.Image)
			fmt.Printf("  Container:%s\n", shortID)
			fmt.Printf("  Network:  %s\n", ci.NetworkPolicy)
			fmt.Printf("  Resources:%.1f CPU / %d MB RAM\n", ci.CPULimit, ci.MemoryLimitMB)
			break
		}
	}

	if task.Status == domain.StatusAwaitingGate {
		fmt.Println()
		fmt.Println("  Awaiting gate approval. Use:")
		fmt.Printf("    synapse approve %s\n", task.ID)
		fmt.Printf("    synapse reject %s --feedback \"your feedback\"\n", task.ID)
	}
}
