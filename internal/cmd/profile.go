package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/util"
)

func newProfileCommand() *cobra.Command {
	profileCmd := &cobra.Command{
		Use:   "profile",
		Short: "View and manage your user profile",
		RunE:  runProfileShow,
	}

	profileCmd.AddCommand(
		newProfileUpdateCommand(),
		newProfilePasswordCommand(),
	)

	return profileCmd
}

func runProfileShow(cmd *cobra.Command, args []string) error {
	app := MustApp()
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	profile, err := app.API.GetProfile(ctx)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	u := profile.User
	fmt.Println(style.Bold.Render("User Profile"))
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("ID:             %d\n", u.ID)
	fmt.Printf("Name:           %s\n", u.Name)
	fmt.Printf("Email:          %s\n", u.Email)
	fmt.Printf("Role:           %s\n", u.Role)
	fmt.Printf("Email Verified: %v\n", u.EmailVerified)
	fmt.Printf("MFA Enabled:    %v\n", u.MFAEnabled)
	fmt.Printf("Status:         %s\n", u.ApprovalStatus)

	if len(profile.Organizations) > 0 {
		fmt.Println()
		fmt.Println(style.Bold.Render("Organizations"))
		fmt.Println(strings.Repeat("-", 40))
		for _, org := range profile.Organizations {
			fmt.Printf("  %s (ID: %d, role: %s)\n", org.Name, org.ID, org.Role)
		}
	}

	return nil
}

func newProfileUpdateCommand() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update your display name",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.UpdateProfile(ctx, name); err != nil {
				return fmt.Errorf("update profile: %w", err)
			}

			fmt.Println(style.Success.Render("Profile updated."))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "new display name")
	return cmd
}

func newProfilePasswordCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "password",
		Short: "Change your password",
		RunE: func(cmd *cobra.Command, args []string) error {
			currentPw, err := util.PromptPassword("Current password")
			if err != nil {
				return fmt.Errorf("read current password: %w", err)
			}
			newPw, err := util.PromptPassword("New password")
			if err != nil {
				return fmt.Errorf("read new password: %w", err)
			}
			confirmPw, err := util.PromptPassword("Confirm new password")
			if err != nil {
				return fmt.Errorf("read password confirmation: %w", err)
			}

			if newPw != confirmPw {
				return fmt.Errorf("passwords do not match")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.ChangePassword(ctx, currentPw, newPw); err != nil {
				return fmt.Errorf("change password: %w", err)
			}

			fmt.Println(style.Success.Render("Password changed."))
			return nil
		},
	}
}
