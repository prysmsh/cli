package cmd

import (
	"errors"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/cmdutil"
	"github.com/warp-run/prysm-cli/internal/session"
	"github.com/warp-run/prysm-cli/internal/util"
)

func newLoginCommand() *cobra.Command {
	var (
		email      string
		password   string
		totp       string
		backupCode string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to the Prysm control plane",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if email == "" {
				var err error
				email, err = util.PromptInput("Email")
				if err != nil {
					return err
				}
			}
			email = strings.TrimSpace(email)
			if email == "" {
				return errors.New("email is required")
			}

			if password == "" {
				var err error
				password, err = util.PromptPassword("Password")
				if err != nil {
					return err
				}
			}

			req := api.LoginRequest{
				Email:      email,
				Password:   password,
				TOTPCode:   totp,
				BackupCode: backupCode,
			}

			// Create context with timeout and signal handling (60s for slow or local backends)
			ctx, cancel := cmdutil.ContextWithTimeout(cmd.Context(), cmdutil.LongTimeout)
			defer cancel()

			printDebug("Attempting login for %s", email)

			resp, err := app.API.Login(ctx, req)
			if err != nil {
				printDebug("Login failed: %v", err)
				return err
			}

			printDebug("Login successful for %s", resp.User.Email)

			sess := &session.Session{
				Token:         resp.Token,
				RefreshToken:  resp.RefreshToken,
				Email:         resp.User.Email,
				SessionID:     resp.SessionID,
				CSRFToken:     resp.CSRFToken,
				ExpiresAtUnix: resp.ExpiresAtUnix,
				User: session.SessionUser{
					ID:         resp.User.ID,
					Name:       resp.User.Name,
					Email:      resp.User.Email,
					Role:       resp.User.Role,
					MFAEnabled: resp.User.MFAEnabled,
				},
				Organization: session.SessionOrg{
					ID:   resp.Organization.ID,
					Name: resp.Organization.Name,
				},
				APIBaseURL:    app.Config.APIBaseURL,
				ComplianceURL: app.Config.ComplianceURL,
				DERPServerURL: app.Config.DERPServerURL,
				OutputFormat:  app.OutputFormat,
			}

			if err := app.Sessions.Save(sess); err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("✅ Login successful — welcome, %s (%s)\n", resp.User.Name, resp.User.Email)
			return nil
		},
	}

	cmd.Flags().StringVarP(&email, "email", "e", "", "email address")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password (not recommended to use via flag)")
	cmd.Flags().StringVar(&totp, "totp", "", "TOTP code for MFA")
	cmd.Flags().StringVar(&backupCode, "backup-code", "", "backup code for MFA")

	return cmd
}

// Deprecated: use util.PromptInput instead.
func promptInput(label string) (string, error) {
	return util.PromptInput(label)
}

// Deprecated: use util.PromptPassword instead.
func promptPassword(label string) (string, error) {
	return util.PromptPassword(label)
}
