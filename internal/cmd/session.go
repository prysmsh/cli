package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
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
				fmt.Println(style.Warning.Render("No active session detected. Run `prysm login` to authenticate."))
				return nil
			}

			expiry := sess.ExpiresAt()
			expired := sess.IsExpired(0)
			nearExpiry := sess.IsExpired(5 * time.Minute)
			statusStyle := style.Success
			if nearExpiry {
				statusStyle = style.Error
			}

			fmt.Printf("Identity: %s (%s)\n", sess.User.Name, sess.Email)
			fmt.Printf("Organization: %s (ID %d)\n", sess.Organization.Name, sess.Organization.ID)
			fmt.Printf("Session ID: %s\n", sess.SessionID)
			fmt.Printf("API Endpoint: %s\n", sess.APIBaseURL)
			fmt.Printf("DERP Relay: %s\n", sess.DERPServerURL)
			fmt.Printf("Issued: %s\n", sess.SavedAt.Format(time.RFC3339))
			if !expiry.IsZero() {
				fmt.Print(statusStyle.Render(fmt.Sprintf("Expires: %s\n", expiry.Format(time.RFC3339))))
			}
			if expired {
				fmt.Println(style.Error.Render("Session expired. Run `prysm login` to re-authenticate."))
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

			// In SSH there is no browser; use device-code unless an explicit provider was set.
			if provider == "" && isSSHSession() {
				return runDeviceCodeLogin(cmd.Context(), app)
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
