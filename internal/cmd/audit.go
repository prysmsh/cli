package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newAuditCommand() *cobra.Command {
	auditCmd := &cobra.Command{
		Use:   "audit",
		Short: "Generate compliance evidence and reports",
	}

	auditCmd.AddCommand(newAuditFrameworksCommand())
	return auditCmd
}

func newAuditFrameworksCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "frameworks",
		Short: "List compliance frameworks available to your organization",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			base := strings.TrimRight(app.Config.ComplianceURL, "/")
			if base == "" {
				return fmt.Errorf("compliance API URL not configured")
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			url := base + "/frameworks"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Accept", "application/json")

			if sess, _ := app.Sessions.Load(); sess != nil && sess.Token != "" {
				req.Header.Set("Authorization", "Bearer "+sess.Token)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("fetch frameworks: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				return fmt.Errorf("compliance service error: %s", resp.Status)
			}

			var payload interface{}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				return fmt.Errorf("decode compliance response: %w", err)
			}

			frameworks := extractFrameworks(payload)
			if len(frameworks) == 0 {
				fmt.Println(style.Warning.Render("No compliance frameworks available."))
				return nil
			}

			headers := []string{"FRAMEWORK", "VERSION", "STATUS"}
			rows := make([][]string, 0, len(frameworks))
			for _, fw := range frameworks {
				rows = append(rows, []string{fw.Name, fw.Version, fw.Status})
			}
			ui.PrintTable(headers, rows)
			return nil
		},
	}
}

type frameworkInfo struct {
	Name    string
	Version string
	Status  string
}

func extractFrameworks(value interface{}) []frameworkInfo {
	var frameworks []frameworkInfo

	switch v := value.(type) {
	case map[string]interface{}:
		if list, ok := v["frameworks"]; ok {
			return extractFrameworks(list)
		}
	case []interface{}:
		for _, item := range v {
			switch fw := item.(type) {
			case map[string]interface{}:
				frameworks = append(frameworks, frameworkInfo{
					Name:    getStringValue(fw["name"]),
					Version: getStringValue(fw["version"]),
					Status:  getStringValue(fw["status"], "active"),
				})
			}
		}
	}

	return frameworks
}

func getStringValue(value interface{}, fallback ...string) string {
	if len(fallback) > 0 {
		switch v := value.(type) {
		case nil:
			return fallback[0]
		case string:
			if v == "" {
				return fallback[0]
			}
			return v
		default:
			return fallback[0]
		}
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}
