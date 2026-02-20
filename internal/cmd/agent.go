package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func newAgentCommand() *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Engineering agent: ask questions about clusters, security, and compliance",
	}

	agentCmd.AddCommand(
		newAgentAskCommand(),
		newAgentConversationsCommand(),
		newAgentToolsCommand(),
	)

	return agentCmd
}

// agentAsk creates a conversation, sends the message, runs the agent, and prints the reply.
func newAgentAskCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ask [message]",
		Short: "Send a message to the engineering agent and print the reply",
		Long:  "Creates a conversation, sends your message, runs the agent (tools), and prints the assistant reply. Example: prysm agent ask \"List my clusters\"",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			message := strings.TrimSpace(strings.Join(args, " "))
			if message == "" {
				return fmt.Errorf("provide a message, e.g. prysm agent ask \"List my clusters\"")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			// Create conversation
			var createResp struct {
				ID        uint   `json:"id"`
				Title     string `json:"title"`
				CreatedAt string `json:"created_at"`
			}
			resp, err := app.API.Do(ctx, "POST", "agent/conversations", nil, &createResp)
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

			// Add message and run agent
			var msgResp struct {
				Messages []struct {
					ID      uint   `json:"id"`
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			resp, err = app.API.Do(ctx, "POST", fmt.Sprintf("agent/conversations/%d/messages", convID), map[string]interface{}{
				"content":   message,
				"run_agent": true,
			}, &msgResp)
			if err != nil {
				return fmt.Errorf("send message: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("send message: %s", resp.Status)
			}

			// Print assistant reply (last message with role assistant)
			for i := len(msgResp.Messages) - 1; i >= 0; i-- {
				m := msgResp.Messages[i]
				if m.Role == "assistant" {
					fmt.Println(m.Content)
					return nil
				}
			}
			fmt.Fprintln(os.Stderr, color.New(color.FgYellow).Sprint("No assistant reply in response."))
			return nil
		},
	}
}

func newAgentConversationsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "conversations",
		Short: "List agent conversations",
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
			resp, err := app.API.Do(ctx, "GET", "agent/conversations", nil, &data)
			if err != nil {
				return fmt.Errorf("list conversations: %w", err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				return fmt.Errorf("list conversations: %s", resp.Status)
			}

			if len(data.Conversations) == 0 {
				fmt.Println("No conversations. Use `prysm agent ask \"...\"` to start one.")
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

func newAgentToolsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List available agent tools (for debugging)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			var data struct {
				Tools   []struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description"`
					InputSchema map[string]interface{} `json:"input_schema"`
				} `json:"tools"`
				Count   int    `json:"count"`
				Version string `json:"version"`
			}
			resp, err := app.API.Do(ctx, "GET", "agent/tools", nil, &data)
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
