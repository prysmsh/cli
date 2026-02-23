package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
)

type diagnoseCheck struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type diagnoseReport struct {
	Category    string          `json:"category"`
	OK          bool            `json:"ok"`
	GeneratedAt time.Time       `json:"generated_at"`
	Checks      []diagnoseCheck `json:"checks"`
}

func newDiagnoseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Run diagnostics for connectivity and access workflows",
	}
	cmd.AddCommand(
		newDiagnoseNetworkCommand(),
		newDiagnoseAccessCommand(),
	)
	return cmd
}

func newDiagnoseNetworkCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "network",
		Short: "Diagnose control-plane and network connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := runNetworkDiagnostics(cmd.Context())
			if wantsJSONOutput(outputFormat) {
				if err := writeJSON(report); err != nil {
					return err
				}
			} else {
				printDiagnoseReport(report)
			}
			if !report.OK {
				return errors.New("network diagnostics failed")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	return cmd
}

func newDiagnoseAccessCommand() *cobra.Command {
	var (
		outputFormat string
		target       string
		reason       string
	)

	cmd := &cobra.Command{
		Use:   "access",
		Short: "Diagnose access workflow APIs (requests, sessions, SSH policy)",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := runAccessDiagnostics(cmd.Context(), strings.TrimSpace(target), strings.TrimSpace(reason))
			if wantsJSONOutput(outputFormat) {
				if err := writeJSON(report); err != nil {
					return err
				}
			} else {
				printDiagnoseReport(report)
			}
			if !report.OK {
				return errors.New("access diagnostics failed")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json)")
	cmd.Flags().StringVar(&target, "target", "", "optional SSH target to validate policy checks (e.g. user@host)")
	cmd.Flags().StringVar(&reason, "reason", "diagnose access", "reason used when --target is set")
	return cmd
}

func runNetworkDiagnostics(parentCtx context.Context) diagnoseReport {
	app := MustApp()
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	report := diagnoseReport{
		Category:    "network",
		GeneratedAt: time.Now().UTC(),
		Checks:      make([]diagnoseCheck, 0, 6),
	}
	failed := false

	var sessTokenPresent bool
	sess, sessErr := app.Sessions.Load()
	switch {
	case sessErr != nil:
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session", Status: "fail", Detail: sessErr.Error()})
	case sess == nil:
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session", Status: "fail", Detail: "no active session; run prysm login"})
	default:
		sessTokenPresent = strings.TrimSpace(sess.Token) != ""
		expiry := sess.ExpiresAt()
		detail := "session loaded"
		if !expiry.IsZero() {
			detail = "expires " + expiry.UTC().Format(time.RFC3339)
		}
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session", Status: "pass", Detail: detail})
	}

	apiStart := time.Now()
	if _, err := app.API.GetProfile(ctx); err != nil {
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "api_profile",
			Status:    "fail",
			Detail:    err.Error(),
			LatencyMS: time.Since(apiStart).Milliseconds(),
		})
	} else {
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "api_profile",
			Status:    "pass",
			LatencyMS: time.Since(apiStart).Milliseconds(),
		})
	}

	clusterStart := time.Now()
	if _, err := app.API.ListClusters(ctx); err != nil {
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "cluster_listing",
			Status:    "fail",
			Detail:    err.Error(),
			LatencyMS: time.Since(clusterStart).Milliseconds(),
		})
	} else {
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "cluster_listing",
			Status:    "pass",
			LatencyMS: time.Since(clusterStart).Milliseconds(),
		})
	}

	relay := ""
	if app.Config != nil {
		relay = strings.TrimSpace(app.Config.DERPServerURL)
	}
	if relay == "" && sess != nil {
		relay = strings.TrimSpace(sess.DERPServerURL)
	}

	if relay == "" {
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{Name: "derp_config", Status: "fail", Detail: "DERP relay URL not configured"})
	} else {
		parsed, err := url.Parse(relay)
		if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
			failed = true
			report.Checks = append(report.Checks, diagnoseCheck{Name: "derp_config", Status: "fail", Detail: "invalid DERP URL: " + relay})
		} else {
			lookupCtx, lookupCancel := context.WithTimeout(ctx, 5*time.Second)
			ips, lookupErr := net.DefaultResolver.LookupHost(lookupCtx, parsed.Hostname())
			lookupCancel()
			if lookupErr != nil {
				failed = true
				report.Checks = append(report.Checks, diagnoseCheck{
					Name:   "derp_dns",
					Status: "fail",
					Detail: lookupErr.Error(),
				})
			} else {
				detail := parsed.Hostname()
				if len(ips) > 0 {
					detail = detail + " -> " + ips[0]
				}
				report.Checks = append(report.Checks, diagnoseCheck{Name: "derp_dns", Status: "pass", Detail: detail})
			}
		}
	}

	if !sessTokenPresent {
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session_token", Status: "fail", Detail: "session token missing"})
	} else {
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session_token", Status: "pass"})
	}

	report.OK = !failed
	return report
}

func runAccessDiagnostics(parentCtx context.Context, target string, reason string) diagnoseReport {
	app := MustApp()
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	report := diagnoseReport{
		Category:    "access",
		GeneratedAt: time.Now().UTC(),
		Checks:      make([]diagnoseCheck, 0, 6),
	}
	failed := false

	sess, sessErr := app.Sessions.Load()
	switch {
	case sessErr != nil:
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session", Status: "fail", Detail: sessErr.Error()})
	case sess == nil:
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session", Status: "fail", Detail: "no active session; run prysm login"})
	default:
		report.Checks = append(report.Checks, diagnoseCheck{Name: "session", Status: "pass"})
	}

	reqStart := time.Now()
	if _, err := app.API.ListAccessRequests(ctx, api.AccessRequestListOptions{Limit: 1}); err != nil {
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "requests_api",
			Status:    "fail",
			Detail:    err.Error(),
			LatencyMS: time.Since(reqStart).Milliseconds(),
		})
	} else {
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "requests_api",
			Status:    "pass",
			LatencyMS: time.Since(reqStart).Milliseconds(),
		})
	}

	sessionsStart := time.Now()
	if _, err := app.API.ListAccessSessions(ctx, api.AccessSessionListOptions{Limit: 1}); err != nil {
		failed = true
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "sessions_api",
			Status:    "fail",
			Detail:    err.Error(),
			LatencyMS: time.Since(sessionsStart).Milliseconds(),
		})
	} else {
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:      "sessions_api",
			Status:    "pass",
			LatencyMS: time.Since(sessionsStart).Milliseconds(),
		})
	}

	if target == "" {
		report.Checks = append(report.Checks, diagnoseCheck{
			Name:   "ssh_policy",
			Status: "skip",
			Detail: "set --target to verify SSH policy path",
		})
	} else {
		sshStart := time.Now()
		if _, err := app.API.ConnectSSH(ctx, api.SSHConnectRequest{
			Target: target,
			Reason: firstNonEmpty(reason, "diagnose access"),
			DryRun: true,
		}); err != nil {
			failed = true
			report.Checks = append(report.Checks, diagnoseCheck{
				Name:      "ssh_policy",
				Status:    "fail",
				Detail:    err.Error(),
				LatencyMS: time.Since(sshStart).Milliseconds(),
			})
		} else {
			report.Checks = append(report.Checks, diagnoseCheck{
				Name:      "ssh_policy",
				Status:    "pass",
				LatencyMS: time.Since(sshStart).Milliseconds(),
			})
		}
	}

	report.OK = !failed
	return report
}

func printDiagnoseReport(report diagnoseReport) {
	title := fmt.Sprintf("Diagnostics: %s", report.Category)
	if report.OK {
		fmt.Println(style.Success.Render(title))
	} else {
		fmt.Println(style.Error.Render(title))
	}
	fmt.Printf("Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))

	for _, check := range report.Checks {
		label := check.Name
		switch check.Status {
		case "pass":
			label = style.Success.Render("PASS") + " " + label
		case "fail":
			label = style.Error.Render("FAIL") + " " + label
		default:
			label = style.Warning.Render(strings.ToUpper(check.Status)) + " " + label
		}

		if check.LatencyMS > 0 {
			fmt.Printf("  %s (%dms)", label, check.LatencyMS)
		} else {
			fmt.Printf("  %s", label)
		}
		if strings.TrimSpace(check.Detail) != "" {
			fmt.Printf(" - %s", check.Detail)
		}
		fmt.Println()
	}
}
