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

func newTeamCommand() *cobra.Command {
	teamCmd := &cobra.Command{
		Use:     "team",
		Aliases: []string{"members"},
		Short:   "Manage organization team members and invitations",
	}

	teamCmd.AddCommand(
		newTeamListCommand(),
		newTeamInviteCommand(),
		newTeamInvitationsCommand(),
		newTeamRevokeInviteCommand(),
		newTeamUpdateRoleCommand(),
		newTeamRemoveCommand(),
	)

	return teamCmd
}

func newTeamListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List team members",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			members, err := app.API.ListTeamMembers(ctx)
			if err != nil {
				return fmt.Errorf("list team members: %w", err)
			}

			if len(members) == 0 {
				fmt.Println(style.Warning.Render("No team members found."))
				return nil
			}

			fmt.Print(style.Bold.Render(fmt.Sprintf("%-6s %-20s %-30s %-10s %-10s\n", "ID", "NAME", "EMAIL", "ROLE", "STATUS")))
			fmt.Println(strings.Repeat("-", 78))

			for _, m := range members {
				roleStyle := style.MutedStyle
				if m.Role == "owner" || m.Role == "admin" {
					roleStyle = style.Info
				}
				fmt.Printf("%-6d %-20s %-30s ", m.ID, util.TruncateString(m.User.Name, 20), util.TruncateString(m.User.Email, 30))
				fmt.Print(roleStyle.Render(fmt.Sprintf("%-10s ", m.Role)))
				fmt.Printf("%-10s\n", m.Status)
			}
			return nil
		},
	}
}

func newTeamInviteCommand() *cobra.Command {
	var email, role string

	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Invite a new team member",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				return fmt.Errorf("--email is required")
			}
			if role == "" {
				return fmt.Errorf("--role is required")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.InviteTeamMember(ctx, email, role); err != nil {
				return fmt.Errorf("invite team member: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Invitation sent to %s with role %s.", email, role)))
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email address to invite")
	cmd.Flags().StringVar(&role, "role", "", "role to assign (e.g. admin, member, viewer)")
	return cmd
}

func newTeamInvitationsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "invitations",
		Short: "List pending invitations",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			invitations, err := app.API.ListInvitations(ctx)
			if err != nil {
				return fmt.Errorf("list invitations: %w", err)
			}

			if len(invitations) == 0 {
				fmt.Println(style.Warning.Render("No pending invitations."))
				return nil
			}

			fmt.Print(style.Bold.Render(fmt.Sprintf("%-6s %-30s %-10s %-10s %-20s\n", "ID", "EMAIL", "ROLE", "STATUS", "EXPIRES")))
			fmt.Println(strings.Repeat("-", 78))

			for _, inv := range invitations {
				fmt.Printf("%-6d %-30s %-10s %-10s %-20s\n",
					inv.ID,
					util.TruncateString(inv.Email, 30),
					inv.Role,
					inv.Status,
					inv.ExpiresAt.Format(time.DateOnly),
				)
			}
			return nil
		},
	}
}

func newTeamRevokeInviteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke-invite <invitation-id>",
		Short: "Revoke a pending invitation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := util.SafePathSegment(id); err != nil {
				return fmt.Errorf("invalid invitation ID: %w", err)
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.RevokeInvitation(ctx, id); err != nil {
				return fmt.Errorf("revoke invitation: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Invitation %s revoked.", id)))
			return nil
		},
	}
}

func newTeamUpdateRoleCommand() *cobra.Command {
	var role string

	cmd := &cobra.Command{
		Use:   "update-role <member-id>",
		Short: "Update a team member's role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := util.SafePathSegment(id); err != nil {
				return fmt.Errorf("invalid member ID: %w", err)
			}
			if role == "" {
				return fmt.Errorf("--role is required")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.UpdateMemberRole(ctx, id, role); err != nil {
				return fmt.Errorf("update member role: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Member %s role updated to %s.", id, role)))
			return nil
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "new role (e.g. admin, member, viewer)")
	return cmd
}

func newTeamRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <member-id>",
		Short: "Remove a team member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := util.SafePathSegment(id); err != nil {
				return fmt.Errorf("invalid member ID: %w", err)
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.RemoveMember(ctx, id); err != nil {
				return fmt.Errorf("remove member: %w", err)
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Member %s removed.", id)))
			return nil
		},
	}
}
