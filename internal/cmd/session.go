package cmd

import (
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
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
	var useGitHub bool
	var useApple bool
	var useEmail bool
	var useDeviceCode bool

	refreshCmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh the current session by re-authenticating",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if useDeviceCode {
				if useGitHub || useApple || useEmail {
					return fmt.Errorf("--device-code cannot be combined with --github, --apple, or --email")
				}
				return runDeviceCodeLogin(cmd.Context(), app)
			}

			provider := ""
			if useGitHub {
				provider = "github"
			} else if useApple {
				provider = "apple"
			} else if useEmail {
				provider = "email"
			}

			return runOAuthLogin(cmd.Context(), app, provider)
		},
	}

	refreshCmd.Flags().BoolVar(&useGitHub, "github", false, "open GitHub sign-in directly")
	refreshCmd.Flags().BoolVar(&useApple, "apple", false, "open Apple sign-in directly")
	refreshCmd.Flags().BoolVar(&useEmail, "email", false, "open email/password sign-in")
	refreshCmd.Flags().BoolVar(&useDeviceCode, "device-code", false, "use device code flow for headless environments (SSH, containers)")

	return refreshCmd
}
