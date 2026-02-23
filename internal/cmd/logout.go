package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
)

func newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the current session and purge local credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			sess, err := app.Sessions.Load()
			if err != nil {
				return err
			}
			if sess == nil {
				fmt.Println(style.Warning.Render("No active session. Run `prysm login` to authenticate."))
				return nil
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.Logout(ctx); err != nil {
				var apiErr *api.APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
					fmt.Println(style.Warning.Render(fmt.Sprintf("Logout warning: %s", apiErr.Error())))
				} else {
					return err
				}
			}

			if err := app.Sessions.Clear(); err != nil {
				return err
			}

			fmt.Println(style.Success.Render("🔒 Session revoked. Access tokens destroyed."))
			return nil
		},
	}
}
