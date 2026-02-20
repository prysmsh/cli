package cmd

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
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
				color.New(color.FgYellow).Println("No active session. Run `prysm login` to authenticate.")
				return nil
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.Logout(ctx); err != nil {
				var apiErr *api.APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
					color.New(color.FgYellow).Printf("Logout warning: %s\n", apiErr.Error())
				} else {
					return err
				}
			}

			if err := app.Sessions.Clear(); err != nil {
				return err
			}

			color.New(color.FgGreen).Println("🔒 Session revoked. Access tokens destroyed.")
			return nil
		},
	}
}
