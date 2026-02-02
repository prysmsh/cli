package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/session"
)

func newSessionCommand() *cobra.Command {
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage authentication sessions",
	}

	sessionCmd.AddCommand(
		newSessionStatusCommand(),
		newSessionRefreshCommand(),
	)

	return sessionCmd
}

func newSessionStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show information about the current session",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			sess, err := app.Sessions.Load()
			if err != nil {
				return err
			}
			if sess == nil {
				color.New(color.FgYellow).Println("No active session detected. Run `prysm login` to authenticate.")
				return nil
			}

			expiry := sess.ExpiresAt()
			statusColor := color.New(color.FgGreen)
			if sess.IsExpired(5 * time.Minute) {
				statusColor = color.New(color.FgRed)
			}

			fmt.Printf("Identity: %s (%s)\n", sess.User.Name, sess.Email)
			fmt.Printf("Organization: %s (ID %d)\n", sess.Organization.Name, sess.Organization.ID)
			fmt.Printf("Session ID: %s\n", sess.SessionID)
			fmt.Printf("API Endpoint: %s\n", sess.APIBaseURL)
			fmt.Printf("DERP Relay: %s\n", sess.DERPServerURL)
			fmt.Printf("Issued: %s\n", sess.SavedAt.Format(time.RFC3339))
			if !expiry.IsZero() {
				statusColor.Printf("Expires: %s\n", expiry.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func newSessionRefreshCommand() *cobra.Command {
	var password string
	var totp string
	var backup string

	refreshCmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh the current session by re-authenticating",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			sess, err := app.Sessions.Load()
			if err != nil {
				return err
			}
			if sess == nil {
				return fmt.Errorf("no active session; run `prysm login`")
			}

			if password == "" {
				password, err = promptPassword("Password")
				if err != nil {
					return err
				}
			}

			req := api.LoginRequest{
				Email:      sess.Email,
				Password:   password,
				TOTPCode:   totp,
				BackupCode: backup,
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := app.API.Login(ctx, req)
			if err != nil {
				return err
			}

			newSess := *sess
			newSess.Token = resp.Token
			newSess.RefreshToken = resp.RefreshToken
			newSess.ExpiresAtUnix = resp.ExpiresAtUnix
			newSess.SessionID = resp.SessionID
			newSess.CSRFToken = resp.CSRFToken
			newSess.User = session.SessionUser{
				ID:         resp.User.ID,
				Name:       resp.User.Name,
				Email:      resp.User.Email,
				Role:       resp.User.Role,
				MFAEnabled: resp.User.MFAEnabled,
			}
			newSess.Organization = session.SessionOrg{
				ID:   resp.Organization.ID,
				Name: resp.Organization.Name,
			}

			if err := app.Sessions.Save(&newSess); err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("✅ Session refreshed — expires %s\n", newSess.ExpiresAt().Format(time.RFC3339))
			return nil
		},
	}

	refreshCmd.Flags().StringVarP(&password, "password", "p", "", "password")
	refreshCmd.Flags().StringVar(&totp, "totp", "", "TOTP code")
	refreshCmd.Flags().StringVar(&backup, "backup-code", "", "backup code")

	return refreshCmd
}
