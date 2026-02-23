package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prysmsh/pkg/retry"
	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newAICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "AI assistant, agents, and embeddings",
	}

	cmd.AddCommand(
		newAIChatCommand(),
		newAIConversationsCommand(),
		newAIToolsCommand(),
		newAIAgentsSubcommand(),
		newAIEmbeddingsSubcommand(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// Chat (was: prysm agent ask)
// ---------------------------------------------------------------------------

func newAIChatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "chat [message]",
		Short: "Send a message to the AI assistant",
		Long:  "Creates a conversation, sends your message, runs the AI agent tools, and prints the reply.\nExample: prysm ai chat \"List clusters with critical CVEs\"",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			message := strings.TrimSpace(strings.Join(args, " "))
			if message == "" {
				return fmt.Errorf("provide a message, e.g. prysm ai chat \"List my clusters\"")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			// Create conversation
			var createResp struct {
				ID        uint   `json:"id"`
				Title     string `json:"title"`
				CreatedAt string `json:"created_at"`
			}
			resp, err := app.API.Do(ctx, "POST", "agent-chat/conversations", nil, &createResp)
			if err != nil {
				return fmt.Errorf("create conversation: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("create conversation: %s", resp.Status)
			}

			convID := createResp.ID
			if convID == 0 {
				return fmt.Errorf("create conversation: no id returned")
			}

			// Add message and run agent (retry transient failures)
			var msgResp struct {
				Messages []struct {
					ID      uint   `json:"id"`
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := ui.WithSpinner("Thinking...", func() error {
				return retry.Do(ctx, 3, func() error {
					resp, err = app.API.Do(ctx, "POST", fmt.Sprintf("agent-chat/conversations/%d/messages", convID), map[string]interface{}{
						"content":   message,
						"run_agent": true,
					}, &msgResp)
					if err != nil {
						return fmt.Errorf("send message: %w", err)
					}
					if resp != nil && resp.StatusCode >= 500 {
						return fmt.Errorf("send message: %s", resp.Status)
					}
					if resp != nil && resp.StatusCode >= 400 {
						return fmt.Errorf("send message: %w", fmt.Errorf("%s%w", resp.Status, retry.ErrNonRetryable))
					}
					return nil
				})
			}); err != nil {
				return err
			}

			// Print assistant reply (last message with role assistant)
			for i := len(msgResp.Messages) - 1; i >= 0; i-- {
				m := msgResp.Messages[i]
				if m.Role == "assistant" {
					fmt.Println(m.Content)
					return nil
				}
			}
			fmt.Fprintln(os.Stderr, style.Warning.Render("No assistant reply in response."))
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Conversations (was: prysm agent conversations)
// ---------------------------------------------------------------------------

func newAIConversationsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "conversations",
		Short: "List AI chat conversations",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			var data struct {
				Conversations []struct {
					ID        uint   `json:"id"`
					Title     string `json:"title"`
					CreatedAt string `json:"created_at"`
					UpdatedAt string `json:"updated_at"`
				} `json:"conversations"`
			}
			resp, err := app.API.Do(ctx, "GET", "agent-chat/conversations", nil, &data)
			if err != nil {
				return fmt.Errorf("list conversations: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list conversations: %s", resp.Status)
			}

			if len(data.Conversations) == 0 {
				fmt.Println("No conversations. Use `prysm ai chat \"...\"` to start one.")
				return nil
			}
			for _, c := range data.Conversations {
				title := c.Title
				if title == "" {
					title = "New conversation"
				}
				fmt.Printf("%d\t%s\t%s\n", c.ID, title, c.UpdatedAt)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Tools (was: prysm agent tools)
// ---------------------------------------------------------------------------

func newAIToolsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List available AI tools (for debugging)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			var data struct {
				Tools []struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description"`
					InputSchema map[string]interface{} `json:"input_schema"`
				} `json:"tools"`
				Count   int    `json:"count"`
				Version string `json:"version"`
			}
			resp, err := app.API.Do(ctx, "GET", "agent-chat/tools", nil, &data)
			if err != nil {
				return fmt.Errorf("list tools: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list tools: %s", resp.Status)
			}

			for _, t := range data.Tools {
				fmt.Printf("%s: %s\n", t.Name, t.Description)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Agents subcommand (was: prysm ai-agents)
// ---------------------------------------------------------------------------

func newAIAgentsSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Deploy and manage AI agents on clusters",
	}

	cmd.AddCommand(
		newAIAgentsListCmd(),
		newAIAgentsCreateCmd(),
		newAIAgentsDeployCmd(),
		newAIAgentsUndeployCmd(),
		newAIAgentsLogsCmd(),
		newAIAgentsStatusCmd(),
		newAIAgentsDeleteCmd(),
	)

	return cmd
}

func newAIAgentsListCmd() *cobra.Command {
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
				fmt.Println("No AI agents found. Create one with: prysm ai agents create")
				return nil
			}

			fmt.Println(style.Bold.Render("AI Agents"))
			fmt.Println(strings.Repeat("-", 110))
			fmt.Print(style.Bold.Render(fmt.Sprintf("%-5s %-20s %-14s %-14s %-12s %-10s %-30s\n",
				"ID", "NAME", "TYPE", "RUNTIME", "STATUS", "REPLICAS", "ENDPOINT")))
			fmt.Println(strings.Repeat("-", 110))

			for _, a := range agents {
				var statusStyle style.Style
				switch a.Status {
				case "active":
					statusStyle = style.Success
				case "error":
					statusStyle = style.Error
				case "disabled":
					statusStyle = style.MutedStyle
				default:
					statusStyle = style.Warning
				}

				endpoint := a.EndpointURL
				if len(endpoint) > 28 {
					endpoint = endpoint[:28] + ".."
				}

				fmt.Printf("%-5d %-20s %-14s %-14s ",
					a.ID,
					truncate(a.Name, 18),
					a.Type,
					a.Runtime,
				)
				fmt.Print(statusStyle.Render(fmt.Sprintf("%-12s", a.Status)))
				fmt.Printf(" %d/%-7d %-30s\n",
					a.ReadyReplicas, a.Replicas,
					endpoint,
				)
			}

			return nil
		},
	}
}

func newAIAgentsCreateCmd() *cobra.Command {
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

			fmt.Println(style.Success.Copy().Bold(true).Render(fmt.Sprintf("AI agent created: %s (ID: %d)", agent.Name, agent.ID)))
			fmt.Printf("Type: %s | Runtime: %s | Status: %s\n", agent.Type, agent.Runtime, agent.Status)
			fmt.Println("\nDeploy with: prysm ai agents deploy", agent.ID)

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

func newAIAgentsDeployCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "deploy [id]",
		Short: "Deploy an AI agent (or all with --all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("provide an agent ID or use --all")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if all {
				agents, err := app.API.ListAIAgents(ctx)
				if err != nil {
					return fmt.Errorf("list agents: %w", err)
				}

				var targets []api.AIAgent
				for _, a := range agents {
					if a.Status != "active" {
						targets = append(targets, a)
					}
				}

				if len(targets) == 0 {
					fmt.Println("All agents are already deployed.")
					return nil
				}

				taskNames, agentsByName := agentTaskList(targets)
				succeeded, failed, err := ui.RunBatch("Deploying agents", taskNames, func(name string) error {
					a := agentsByName[name]
					return app.API.DeployAIAgent(ctx, a.ID)
				})
				if err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d of %d agents failed to deploy", failed, succeeded+failed)
				}
				return nil
			}

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			if err := app.API.DeployAIAgent(ctx, id); err != nil {
				return fmt.Errorf("deploy AI agent: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Deployment initiated for agent %d", id)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Deploy all inactive agents")

	return cmd
}

func newAIAgentsUndeployCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "undeploy [id]",
		Short: "Undeploy an AI agent (or all with --all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("provide an agent ID or use --all")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if all {
				agents, err := app.API.ListAIAgents(ctx)
				if err != nil {
					return fmt.Errorf("list agents: %w", err)
				}

				var targets []api.AIAgent
				for _, a := range agents {
					if a.Status != "disabled" && a.Status != "pending" {
						targets = append(targets, a)
					}
				}

				if len(targets) == 0 {
					fmt.Println("No active agents to undeploy.")
					return nil
				}

				confirmed, err := ui.Confirm(fmt.Sprintf("Undeploy all %d active agents?", len(targets)))
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Println("Cancelled.")
					return nil
				}

				taskNames, agentsByName := agentTaskList(targets)
				succeeded, failed, err := ui.RunBatch("Undeploying agents", taskNames, func(name string) error {
					a := agentsByName[name]
					return app.API.UndeployAIAgent(ctx, a.ID)
				})
				if err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d of %d agents failed to undeploy", failed, succeeded+failed)
				}
				return nil
			}

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			if err := app.API.UndeployAIAgent(ctx, id); err != nil {
				return fmt.Errorf("undeploy AI agent: %w", err)
			}

			fmt.Println(style.Warning.Render(fmt.Sprintf("Agent %d undeployed", id)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Undeploy all active agents")

	return cmd
}

func newAIAgentsLogsCmd() *cobra.Command {
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

func newAIAgentsStatusCmd() *cobra.Command {
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

			fmt.Print(style.Bold.Render(fmt.Sprintf("AI Agent: %s (ID: %d)\n", agent.Name, agent.ID)))
			fmt.Println(strings.Repeat("-", 50))

			var statusStyle style.Style
			switch agent.Status {
			case "active":
				statusStyle = style.Success
			case "error":
				statusStyle = style.Error
			case "disabled":
				statusStyle = style.MutedStyle
			default:
				statusStyle = style.Warning
			}

			fmt.Printf("Type:           %s\n", agent.Type)
			fmt.Printf("Runtime:        %s\n", agent.Runtime)
			fmt.Print("Status:         ")
			fmt.Println(statusStyle.Render(agent.Status))
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

func newAIAgentsDeleteCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete an AI agent (or all with --all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("provide an agent ID or use --all")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if all {
				agents, err := app.API.ListAIAgents(ctx)
				if err != nil {
					return fmt.Errorf("list agents: %w", err)
				}

				if len(agents) == 0 {
					fmt.Println("No agents to delete.")
					return nil
				}

				confirmed, err := ui.Confirm(fmt.Sprintf("Delete all %d agents? This cannot be undone.", len(agents)))
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Println("Cancelled.")
					return nil
				}

				taskNames, agentsByName := agentTaskList(agents)
				succeeded, failed, err := ui.RunBatch("Deleting agents", taskNames, func(name string) error {
					a := agentsByName[name]
					return app.API.DeleteAIAgent(ctx, a.ID)
				})
				if err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d of %d agents failed to delete", failed, succeeded+failed)
				}
				return nil
			}

			id, err := api.ParseAIAgentID(args[0])
			if err != nil {
				return err
			}

			confirmed, err := ui.Confirm(fmt.Sprintf("Delete agent %d?", id))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled.")
				return nil
			}

			if err := app.API.DeleteAIAgent(ctx, id); err != nil {
				return fmt.Errorf("delete AI agent: %w", err)
			}

			fmt.Println(style.Error.Render(fmt.Sprintf("Agent %d deleted", id)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Delete all agents")

	return cmd
}

// ---------------------------------------------------------------------------
// Embeddings subcommand (new)
// ---------------------------------------------------------------------------

func newAIEmbeddingsSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "embeddings",
		Aliases: []string{"embed"},
		Short:   "Manage embedding models, collections, and indexing jobs",
	}

	cmd.AddCommand(
		newEmbedModelsCmd(),
		newEmbedSourcesCmd(),
		newEmbedCollectionsCmd(),
		newEmbedDeleteCollectionCmd(),
		newEmbedJobsCmd(),
		newEmbedJobCmd(),
		newEmbedIndexCmd(),
		newEmbedCancelCmd(),
		newEmbedQueryCmd(),
	)

	return cmd
}

func newEmbedModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List available embedding models",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			models, err := app.API.ListEmbedModels(ctx)
			if err != nil {
				return fmt.Errorf("list models: %w", err)
			}

			if len(models) == 0 {
				fmt.Println("No embedding models available.")
				return nil
			}

			fmt.Println(style.Bold.Render("Embedding Models"))
			fmt.Println(strings.Repeat("-", 80))
			fmt.Print(style.Bold.Render(fmt.Sprintf("%-28s %-12s %-10s %-8s %s\n",
				"NAME", "BACKEND", "DIMS", "HEALTH", "DATA TYPES")))
			fmt.Println(strings.Repeat("-", 80))

			for _, m := range models {
				healthStyle := style.Success
				health := "ready"
				if !m.Healthy {
					healthStyle = style.Error
					health = "down"
				}
				types := strings.Join(m.DataTypes, ", ")
				if types == "" {
					types = "-"
				}
				fmt.Printf("%-28s %-12s %-10d ", m.Name, m.Backend, m.Dimensions)
				fmt.Print(healthStyle.Render(fmt.Sprintf("%-8s", health)))
				fmt.Printf(" %s\n", types)
			}

			return nil
		},
	}
}

func newEmbedSourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "List indexable data sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			sources, err := app.API.ListEmbedSources(ctx)
			if err != nil {
				return fmt.Errorf("list sources: %w", err)
			}

			if len(sources) == 0 {
				fmt.Println("No indexable data sources.")
				return nil
			}

			fmt.Println(style.Bold.Render("Indexable Data Sources"))
			fmt.Println(strings.Repeat("-", 80))
			fmt.Print(style.Bold.Render(fmt.Sprintf("%-25s %-25s %-10s %s\n",
				"NAME", "TABLE", "ROWS", "DESCRIPTION")))
			fmt.Println(strings.Repeat("-", 80))

			for _, s := range sources {
				rows := "N/A"
				if s.RowCount >= 0 {
					rows = fmt.Sprintf("%d", s.RowCount)
				}
				desc := s.Description
				if len(desc) > 30 {
					desc = desc[:28] + ".."
				}
				fmt.Printf("%-25s %-25s %-10s %s\n", s.Name, s.Table, rows, desc)
			}

			return nil
		},
	}
}

func newEmbedCollectionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "collections",
		Short: "List embedding collections",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			collections, err := app.API.ListEmbedCollections(ctx)
			if err != nil {
				return fmt.Errorf("list collections: %w", err)
			}

			if len(collections) == 0 {
				fmt.Println("No embedding collections. Run an indexing job to create one.")
				return nil
			}

			fmt.Println(style.Bold.Render("Embedding Collections"))
			fmt.Println(strings.Repeat("-", 90))
			fmt.Print(style.Bold.Render(fmt.Sprintf("%-35s %-25s %-15s %-10s %s\n",
				"NAME", "MODEL", "SOURCE", "DIMS", "POINTS")))
			fmt.Println(strings.Repeat("-", 90))

			for _, c := range collections {
				fmt.Printf("%-35s %-25s %-15s %-10d %d\n",
					truncate(c.Name, 33), truncate(c.Model, 23), truncate(c.Source, 13),
					c.Dimensions, c.PointCount)
			}

			return nil
		},
	}
}

func newEmbedDeleteCollectionCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "delete-collection [name]",
		Short: "Delete an embedding collection (or all with --all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("provide a collection name or use --all")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if all {
				collections, err := app.API.ListEmbedCollections(ctx)
				if err != nil {
					return fmt.Errorf("list collections: %w", err)
				}

				if len(collections) == 0 {
					fmt.Println("No collections to delete.")
					return nil
				}

				confirmed, err := ui.Confirm(fmt.Sprintf("Delete all %d collections? This cannot be undone.", len(collections)))
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Println("Cancelled.")
					return nil
				}

				taskNames := make([]string, len(collections))
				for i, c := range collections {
					taskNames[i] = c.Name
				}

				succeeded, failed, err := ui.RunBatch("Deleting collections", taskNames, func(name string) error {
					return app.API.DeleteEmbedCollection(ctx, name)
				})
				if err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d of %d collections failed to delete", failed, succeeded+failed)
				}
				return nil
			}

			name := args[0]

			confirmed, err := ui.Confirm(fmt.Sprintf("Delete collection %q?", name))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled.")
				return nil
			}

			if err := app.API.DeleteEmbedCollection(ctx, name); err != nil {
				return fmt.Errorf("delete collection: %w", err)
			}

			fmt.Println(style.Warning.Render(fmt.Sprintf("Collection %q deleted", name)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Delete all collections")

	return cmd
}

func newEmbedJobsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "jobs",
		Short: "List embedding indexing jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			jobs, err := app.API.ListEmbedJobs(ctx)
			if err != nil {
				return fmt.Errorf("list jobs: %w", err)
			}

			if len(jobs) == 0 {
				fmt.Println("No embedding jobs. Start one with: prysm ai embeddings index")
				return nil
			}

			fmt.Println(style.Bold.Render("Embedding Jobs"))
			fmt.Println(strings.Repeat("-", 100))
			fmt.Print(style.Bold.Render(fmt.Sprintf("%-6s %-20s %-22s %-12s %-20s %s\n",
				"ID", "SOURCE", "MODEL", "STATUS", "PROGRESS", "CREATED")))
			fmt.Println(strings.Repeat("-", 100))

			for _, j := range jobs {
				var statusStyle style.Style
				switch j.Status {
				case "completed":
					statusStyle = style.Success
				case "failed":
					statusStyle = style.Error
				case "running":
					statusStyle = style.Info
				case "cancelled":
					statusStyle = style.Warning
				default:
					statusStyle = style.MutedStyle
				}

				progress := fmt.Sprintf("%d/%d", j.Processed, j.TotalRecords)
				if j.TotalRecords > 0 {
					pct := j.Processed * 100 / j.TotalRecords
					progress = fmt.Sprintf("%d/%d (%d%%)", j.Processed, j.TotalRecords, pct)
				}

				created := j.CreatedAt
				if len(created) > 19 {
					created = created[:19]
				}

				fmt.Printf("%-6d %-20s %-22s ",
					j.ID, truncate(j.DataSource, 18), truncate(j.ModelName, 20))
				fmt.Print(statusStyle.Render(fmt.Sprintf("%-12s", j.Status)))
				fmt.Printf(" %-20s %s\n", progress, created)
			}

			return nil
		},
	}
}

func newEmbedJobCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "job <id>",
		Short: "Show details for an embedding job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID: %w", err)
			}

			job, err := app.API.GetEmbedJob(ctx, id)
			if err != nil {
				return fmt.Errorf("get job: %w", err)
			}

			fmt.Print(style.Bold.Render(fmt.Sprintf("Embedding Job #%d\n", job.ID)))
			fmt.Println(strings.Repeat("-", 50))
			fmt.Printf("Data Source:    %s\n", job.DataSource)
			fmt.Printf("Model:          %s\n", job.ModelName)
			fmt.Printf("Collection:     %s\n", job.CollectionName)

			var statusStyle style.Style
			switch job.Status {
			case "completed":
				statusStyle = style.Success
			case "failed":
				statusStyle = style.Error
			case "running":
				statusStyle = style.Info
			default:
				statusStyle = style.MutedStyle
			}
			fmt.Print("Status:         ")
			fmt.Println(statusStyle.Render(job.Status))

			if job.TotalRecords > 0 {
				pct := job.Processed * 100 / job.TotalRecords
				fmt.Printf("Progress:       %d/%d (%d%%)\n", job.Processed, job.TotalRecords, pct)
			}
			if job.FailedRecords > 0 {
				fmt.Printf("Failed:         %s\n", style.Error.Render(fmt.Sprintf("%d records", job.FailedRecords)))
			}
			if job.ErrorMessage != "" {
				fmt.Printf("Error:          %s\n", style.Error.Render(job.ErrorMessage))
			}
			if job.StartedAt != "" {
				fmt.Printf("Started:        %s\n", job.StartedAt)
			}
			if job.CompletedAt != "" {
				fmt.Printf("Completed:      %s\n", job.CompletedAt)
			}
			fmt.Printf("Created:        %s\n", job.CreatedAt)

			return nil
		},
	}
}

func newEmbedIndexCmd() *cobra.Command {
	var source, model string

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Start an embedding indexing job",
		Long:  "Creates a new indexing job to embed a data source using the specified model.\nExample: prysm ai embeddings index --source audit_logs --model nomic-embed-text",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if source == "" || model == "" {
				return fmt.Errorf("both --source and --model are required")
			}

			job, err := app.API.CreateEmbedJob(ctx, api.EmbedJobCreateRequest{
				DataSource: source,
				ModelName:  model,
			})
			if err != nil {
				return fmt.Errorf("create indexing job: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Indexing job started (ID: %d)", job.ID)))
			fmt.Printf("Source: %s | Model: %s | Collection: %s\n", job.DataSource, job.ModelName, job.CollectionName)
			fmt.Printf("\nTrack progress: prysm ai embeddings job %d\n", job.ID)

			return nil
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "Data source to index (required)")
	cmd.Flags().StringVar(&model, "model", "", "Embedding model to use (required)")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("model")

	return cmd
}

func newEmbedCancelCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "cancel [job-id]",
		Short: "Cancel a running indexing job (or all with --all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return fmt.Errorf("provide a job ID or use --all")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if all {
				jobs, err := app.API.ListEmbedJobs(ctx)
				if err != nil {
					return fmt.Errorf("list jobs: %w", err)
				}

				var running []api.EmbedJob
				for _, j := range jobs {
					if j.Status == "running" || j.Status == "pending" {
						running = append(running, j)
					}
				}

				if len(running) == 0 {
					fmt.Println("No running jobs to cancel.")
					return nil
				}

				confirmed, err := ui.Confirm(fmt.Sprintf("Cancel all %d running jobs?", len(running)))
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Println("Cancelled.")
					return nil
				}

				taskNames := make([]string, len(running))
				jobsByName := make(map[string]api.EmbedJob)
				for i, j := range running {
					label := fmt.Sprintf("Job #%d (%s / %s)", j.ID, j.DataSource, j.ModelName)
					taskNames[i] = label
					jobsByName[label] = j
				}

				succeeded, failed, err := ui.RunBatch("Cancelling jobs", taskNames, func(name string) error {
					j := jobsByName[name]
					return app.API.CancelEmbedJob(ctx, j.ID)
				})
				if err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d of %d jobs failed to cancel", failed, succeeded+failed)
				}
				return nil
			}

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID: %w", err)
			}

			if err := app.API.CancelEmbedJob(ctx, id); err != nil {
				return fmt.Errorf("cancel job: %w", err)
			}

			fmt.Println(style.Warning.Render(fmt.Sprintf("Job %d cancelled", id)))
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Cancel all running jobs")

	return cmd
}

func newEmbedQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query [text]",
		Short: "Semantic search across embedded data",
		Long:  "Search across all embedding collections using natural language.\nExample: prysm ai embeddings query \"clusters with MFA disabled\"",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("provide a search query")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			var results []api.EmbedQueryResult
			if err := ui.WithSpinner("Searching...", func() error {
				var err error
				results, err = app.API.EmbedQuery(ctx, query)
				return err
			}); err != nil {
				return fmt.Errorf("query: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("No results found.")
				return nil
			}

			fmt.Println(style.Bold.Render(fmt.Sprintf("Results (%d)", len(results))))
			fmt.Println(strings.Repeat("-", 80))

			for i, r := range results {
				scoreStyle := style.MutedStyle
				if r.Score >= 0.8 {
					scoreStyle = style.Success
				} else if r.Score >= 0.6 {
					scoreStyle = style.Info
				}

				fmt.Printf("%d. ", i+1)
				fmt.Print(scoreStyle.Render(fmt.Sprintf("[%.2f]", r.Score)))
				fmt.Printf(" %s\n", style.MutedStyle.Render(fmt.Sprintf("(%s / %s)", r.Source, r.Collection)))
				fmt.Printf("   %s\n\n", r.Text)
			}

			return nil
		},
	}
}

// agentTaskList builds a label list and lookup map from a slice of agents.
func agentTaskList(agents []api.AIAgent) ([]string, map[string]api.AIAgent) {
	names := make([]string, len(agents))
	byName := make(map[string]api.AIAgent, len(agents))
	for i, a := range agents {
		label := fmt.Sprintf("%s (ID: %d)", a.Name, a.ID)
		names[i] = label
		byName[label] = a
	}
	return names, byName
}
