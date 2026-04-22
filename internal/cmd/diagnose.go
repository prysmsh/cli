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
		Short: "Run network and mesh diagnostics",
	}
	cmd.AddCommand(
		newDiagnoseNetworkCommand(),
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
