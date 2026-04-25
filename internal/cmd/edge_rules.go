package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

// --- prysm edge rule (singular) ---

func newEdgeRuleCommand() *cobra.Command {
	ruleCmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage individual WAF rules",
	}

	ruleCmd.AddCommand(
		newEdgeRuleAddCommand(),
		newEdgeRuleRmCommand(),
	)

	return ruleCmd
}

func newEdgeRuleAddCommand() *cobra.Command {
	var name, match, action string
	var priority int

	cmd := &cobra.Command{
		Use:   "add <domain>",
		Short: "Add a WAF rule to a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			rule, err := app.API.CreateEdgeRule(ctx, domain.ID, name, match, action, priority, nil)
			if err != nil {
				return fmt.Errorf("create rule: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s Rule %q added to %s (priority %d)\n",
				style.Success.Render("ok:"), rule.Name, domain.Domain, rule.Priority)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "rule name")
	cmd.Flags().StringVar(&match, "match", "", "match expression")
	cmd.Flags().StringVar(&action, "action", "", "action: block, allow, rate_limit, redirect, challenge, log")
	cmd.Flags().IntVar(&priority, "priority", 100, "priority (lower = evaluated first)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("match")
	_ = cmd.MarkFlagRequired("action")
	return cmd
}

func newEdgeRuleRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <domain> <rule-name>",
		Short: "Remove a WAF rule from a domain",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			rules, err := app.API.ListEdgeRules(ctx, domain.ID)
			if err != nil {
				return fmt.Errorf("list rules: %w", err)
			}

			ruleName := strings.ToLower(args[1])
			for _, r := range rules {
				if strings.ToLower(r.Name) == ruleName {
					if err := app.API.DeleteEdgeRule(ctx, domain.ID, r.ID); err != nil {
						return fmt.Errorf("delete rule: %w", err)
					}
					fmt.Fprintf(os.Stderr, "%s Rule %q removed from %s\n",
						style.Success.Render("ok:"), r.Name, domain.Domain)
					return nil
				}
			}
			return fmt.Errorf("rule %q not found on %s", args[1], domain.Domain)
		},
	}
}

// --- prysm edge rules (plural) ---

func newEdgeRulesCommand() *cobra.Command {
	rulesCmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage WAF rule sets",
	}

	rulesCmd.AddCommand(
		newEdgeRulesListCommand(),
		newEdgeRulesApplyCommand(),
		newEdgeRulesExportCommand(),
	)

	return rulesCmd
}

func newEdgeRulesListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list <domain>",
		Short: "List WAF rules for a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			rules, err := app.API.ListEdgeRules(ctx, domain.ID)
			if err != nil {
				return fmt.Errorf("list rules: %w", err)
			}

			if len(rules) == 0 {
				fmt.Fprintln(os.Stderr, "No rules configured.")
				return nil
			}

			headers := []string{"NAME", "PRIORITY", "MATCH", "ACTION", "ENABLED"}
			data := make([][]string, len(rules))
			for i, r := range rules {
				enabled := "yes"
				if !r.Enabled {
					enabled = "no"
				}
				match := r.MatchExpression
				if len(match) > 50 {
					match = match[:47] + "..."
				}
				data[i] = []string{r.Name, fmt.Sprintf("%d", r.Priority), match, r.Action, enabled}
			}
			ui.PrintTable(headers, data)
			return nil
		},
	}
}

type rulesFile struct {
	Rules []ruleEntry `yaml:"rules" json:"rules"`
}

type ruleEntry struct {
	Name     string `yaml:"name" json:"name"`
	Match    string `yaml:"match" json:"match"`
	Action   string `yaml:"action" json:"action"`
	Priority int    `yaml:"priority" json:"priority"`
	Rate     string `yaml:"rate,omitempty" json:"rate,omitempty"`
	Target   string `yaml:"target,omitempty" json:"target,omitempty"`
}

func newEdgeRulesApplyCommand() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "apply <domain>",
		Short: "Apply WAF rules from a YAML file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}

			var rf rulesFile
			if err := yaml.Unmarshal(data, &rf); err != nil {
				return fmt.Errorf("parse YAML: %w", err)
			}

			for _, entry := range rf.Rules {
				var actionConfig map[string]interface{}
				if entry.Rate != "" || entry.Target != "" {
					actionConfig = make(map[string]interface{})
					if entry.Rate != "" {
						actionConfig["rate"] = entry.Rate
					}
					if entry.Target != "" {
						actionConfig["target"] = entry.Target
					}
				}

				_, err := app.API.CreateEdgeRule(ctx, domain.ID, entry.Name, entry.Match, entry.Action, entry.Priority, actionConfig)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  FAIL %s: %v\n", entry.Name, err)
					continue
				}
				fmt.Fprintf(os.Stderr, "  %s %s\n", style.Success.Render("ok"), entry.Name)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "YAML rules file")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newEdgeRulesExportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export <domain>",
		Short: "Export WAF rules as YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			domain, err := resolveEdgeDomain(ctx, app, args[0])
			if err != nil {
				return err
			}

			rules, err := app.API.ListEdgeRules(ctx, domain.ID)
			if err != nil {
				return fmt.Errorf("list rules: %w", err)
			}

			entries := make([]ruleEntry, len(rules))
			for i, r := range rules {
				entries[i] = ruleEntry{
					Name:     r.Name,
					Match:    r.MatchExpression,
					Action:   r.Action,
					Priority: r.Priority,
				}
			}

			out, _ := yaml.Marshal(rulesFile{Rules: entries})
			fmt.Fprint(os.Stdout, string(out))
			return nil
		},
	}
}
