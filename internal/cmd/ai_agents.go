package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
)

func newAIAgentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ai-agents",
		Aliases: []string{"ai-agent", "agents"},
		Short:   "Deploy and manage AI agents on clusters or Prysm-managed infrastructure",
	}

	cmd.AddCommand(
		newAIAgentsListCommand(),
		newAIAgentsCreateCommand(),
		newAIAgentsDeployCommand(),
		newAIAgentsUndeployCommand(),
		newAIAgentsLogsCommand(),
		newAIAgentsStatusCommand(),
		newAIAgentsDeleteCommand(),
	)

	return cmd
}

func newAIAgentsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List AI agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			agents, err := app.API.ListAIAgents(ctx)
			if err != nil {
				return fmt.Errorf("list AI agents: %w", err)
			}

			if len(agents) == 0 {
				fmt.Println("No AI agents found. Create one with: prysm ai-agents create")
				return nil
			}

			bold := color.New(color.Bold)
			bold.Println("AI Agents")
			fmt.Println(strings.Repeat("-", 110))
			bold.Printf("%-5s %-20s %-14s %-14s %-12s %-10s %-30s\n",
				"ID", "NAME", "TYPE", "RUNTIME", "STATUS", "REPLICAS", "ENDPOINT")
			fmt.Println(strings.Repeat("-", 110))

			for _, a := range agents {
				statusColor := color.FgYellow
				switch a.Status {
				case "active":
					statusColor = color.FgGreen
				case "error":
					statusColor = color.FgRed
				case "disabled":
					statusColor = color.FgHiBlack
				}

				endpoint := a.EndpointURL
				if len(endpoint) > 28 {
					endpoint = endpoint[:28] + ".."
				}

				fmt.Printf("%-5d %-20s %-14s %-14s ",
					a.ID,
					truncateStr(a.Name, 18),
					a.Type,
					a.Runtime,
				)
				color.New(statusColor).Printf("%-12s", a.Status)
				fmt.Printf(" %d/%-7d %-30s\n",
					a.ReadyReplicas, a.Replicas,
					endpoint,
				)
			}

			return nil
		},
	}
}

func newAIAgentsCreateCommand() *cobra.Command {
	var (
		name         string
		agentType    string
		runtime      string
		clusterID    uint
		model        string
		image        string
		replicas     int
		memory       string
		cpu          string
		gpu          string
		systemPrompt string
		envVars      []string
		tags         []string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new AI agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			// Build config JSON based on type
			config := make(map[string]interface{})
			if model != "" {
				config["model"] = model
			}
			if image != "" {
				config["image"] = image
			}
			if systemPrompt != "" {
				config["system_prompt"] = systemPrompt
			}
			if memory != "" {
				config["memory_limit"] = memory
			}
			if cpu != "" {
				config["cpu_limit"] = cpu
			}
			if gpu != "" {
				config["gpu_limit"] = gpu
			}

			// Parse env vars
			if len(envVars) > 0 {
				envMap := make(map[string]string)
				for _, e := range envVars {
					parts := strings.SplitN(e, "=", 2)
					if len(parts) == 2 {
						envMap[parts[0]] = parts[1]
					}
				}
				config["env"] = envMap
			}

			configJSON, _ := json.Marshal(config)

			req := api.AIAgentCreateRequest{
				Name:     name,
				Type:     agentType,
				Runtime:  runtime,
				Config:   configJSON,
				Tags:     tags,
				Replicas: replicas,
			}

			if clusterID > 0 {
				req.ClusterID = &clusterID
			}

			agent, err := app.API.CreateAIAgent(ctx, req)
			if err != nil {
				return fmt.Errorf("create AI agent: %w", err)
			}

			color.New(color.FgGreen, color.Bold).Printf("AI agent created: %s (ID: %d)\n", agent.Name, agent.ID)
			fmt.Printf("Type: %s | Runtime: %s | Status: %s\n", agent.Type, agent.Runtime, agent.Status)
			fmt.Println("\nDeploy with: prysm ai-agents deploy", agent.ID)

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Agent name (required)")
	cmd.Flags().StringVar(&agentType, "type", "llm-chat", "Agent type: llm-chat or model-serving")
	cmd.Flags().StringVar(&runtime, "runtime", "k8s-cluster", "Runtime: k8s-cluster or prysm-managed")
	cmd.Flags().UintVar(&clusterID, "cluster", 0, "Target cluster ID (required for k8s-cluster runtime)")
	cmd.Flags().StringVar(&model, "model", "", "LLM model name (e.g., qwen2.5-coder:7b)")
	cmd.Flags().StringVar(&image, "image", "", "Container image (for model-serving type)")
	cmd.Flags().IntVar(&replicas, "replicas", 1, "Number of replicas")
	cmd.Flags().StringVar(&memory, "memory", "", "Memory limit (e.g., 4Gi)")
	cmd.Flags().StringVar(&cpu, "cpu", "", "CPU limit (e.g., 2)")
	cmd.Flags().StringVar(&gpu, "gpu", "", "GPU limit (e.g., 1)")
	cmd.Flags().StringVar(&systemPrompt, "system-prompt", "", "System prompt for LLM chat agents")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "Environment variables (key=value)")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Tags")

	cmd.MarkFlagRequired("name")

	return cmd
}

func newAIAgentsDeployCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "deploy <id>",
		Short: "Deploy an AI agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			if err := app.API.DeployAIAgent(ctx, id); err != nil {
				return fmt.Errorf("deploy AI agent: %w", err)
			}

			color.New(color.FgGreen).Printf("Deployment initiated for agent %d\n", id)
			return nil
		},
	}
}

func newAIAgentsUndeployCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "undeploy <id>",
		Short: "Undeploy an AI agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			if err := app.API.UndeployAIAgent(ctx, id); err != nil {
				return fmt.Errorf("undeploy AI agent: %w", err)
			}

			color.New(color.FgYellow).Printf("Agent %d undeployed\n", id)
			return nil
		},
	}
}

func newAIAgentsLogsCommand() *cobra.Command {
	var tail int

	cmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "View AI agent logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			lines, err := app.API.GetAIAgentLogs(ctx, id, tail)
			if err != nil {
				return fmt.Errorf("get AI agent logs: %w", err)
			}

			if len(lines) == 0 {
				fmt.Println("No logs available.")
				return nil
			}

			for _, line := range lines {
				fmt.Println(line)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 100, "Number of log lines to show")
	return cmd
}

func newAIAgentsStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <id>",
		Short: "Show AI agent status and details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			agent, err := app.API.GetAIAgent(ctx, id)
			if err != nil {
				return fmt.Errorf("get AI agent: %w", err)
			}

			bold := color.New(color.Bold)
			bold.Printf("AI Agent: %s (ID: %d)\n", agent.Name, agent.ID)
			fmt.Println(strings.Repeat("-", 50))

			statusColor := color.FgYellow
			switch agent.Status {
			case "active":
				statusColor = color.FgGreen
			case "error":
				statusColor = color.FgRed
			case "disabled":
				statusColor = color.FgHiBlack
			}

			fmt.Printf("Type:           %s\n", agent.Type)
			fmt.Printf("Runtime:        %s\n", agent.Runtime)
			fmt.Print("Status:         ")
			color.New(statusColor).Println(agent.Status)
			if agent.StatusMessage != "" {
				fmt.Printf("Message:        %s\n", agent.StatusMessage)
			}
			fmt.Printf("Replicas:       %d/%d\n", agent.ReadyReplicas, agent.Replicas)
			if agent.EndpointURL != "" {
				fmt.Printf("Endpoint:       %s\n", agent.EndpointURL)
			}
			if agent.Description != "" {
				fmt.Printf("Description:    %s\n", agent.Description)
			}
			if len(agent.Tags) > 0 {
				fmt.Printf("Tags:           %s\n", strings.Join(agent.Tags, ", "))
			}
			fmt.Printf("Created:        %s\n", agent.CreatedAt.Format(time.RFC3339))
			if agent.LastReconcileTime != nil {
				fmt.Printf("Last Reconcile: %s\n", agent.LastReconcileTime.Format(time.RFC3339))
			}

			return nil
		},
	}
}

func newAIAgentsDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an AI agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			if err := app.API.DeleteAIAgent(ctx, id); err != nil {
				return fmt.Errorf("delete AI agent: %w", err)
			}

			color.New(color.FgRed).Printf("Agent %d deleted\n", id)
			return nil
		},
	}
}

// truncateStr shortens a string to max length (ai_agents variant).
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
