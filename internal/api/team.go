package api

import (
	"context"
	"fmt"
	"time"
)

// TeamMember represents an organization member returned by the backend.
type TeamMember struct {
	ID             uint      `json:"id"`
	OrganizationID uint      `json:"organization_id"`
	UserID         uint      `json:"user_id"`
	User           TeamUser  `json:"user"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	InvitedBy      uint      `json:"invited_by"`
	InvitedAt      *string   `json:"invited_at"`
	JoinedAt       *string   `json:"joined_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TeamUser is the nested user info inside a TeamMember.
type TeamUser struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// TeamInvitation represents a pending team invitation.
type TeamInvitation struct {
	ID             uint      `json:"id"`
	OrganizationID uint      `json:"organization_id"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	InvitedBy      uint      `json:"invited_by"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListTeamMembers returns all active members of the current organization.
func (c *Client) ListTeamMembers(ctx context.Context) ([]TeamMember, error) {
	var resp struct {
		Members []TeamMember `json:"members"`
	}
	if _, err := c.Do(ctx, "GET", "/team/members", nil, &resp); err != nil {
		return nil, err
	}
	if resp.Members == nil {
		return []TeamMember{}, nil
	}
	return resp.Members, nil
}

// InviteTeamMember sends an invitation to the given email with the specified role.
func (c *Client) InviteTeamMember(ctx context.Context, email, role string) error {
	payload := struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}{Email: email, Role: role}

	if _, err := c.Do(ctx, "POST", "/team/invite", payload, nil); err != nil {
		return err
	}
	return nil
}

// ListInvitations returns pending invitations for the current organization.
func (c *Client) ListInvitations(ctx context.Context) ([]TeamInvitation, error) {
	var resp struct {
		Invitations []TeamInvitation `json:"invitations"`
	}
	if _, err := c.Do(ctx, "GET", "/team/invitations", nil, &resp); err != nil {
		return nil, err
	}
	if resp.Invitations == nil {
		return []TeamInvitation{}, nil
	}
	return resp.Invitations, nil
}

// RevokeInvitation cancels a pending invitation by ID.
func (c *Client) RevokeInvitation(ctx context.Context, id string) error {
	endpoint := fmt.Sprintf("/team/invitations/%s", id)
	_, err := c.Do(ctx, "DELETE", endpoint, nil, nil)
	return err
}

// UpdateMemberRole changes the role of an organization member.
func (c *Client) UpdateMemberRole(ctx context.Context, id, role string) error {
	payload := struct {
		Role string `json:"role"`
	}{Role: role}
	endpoint := fmt.Sprintf("/team/members/%s", id)
	_, err := c.Do(ctx, "PUT", endpoint, payload, nil)
	return err
}

// RemoveMember removes a member from the organization.
func (c *Client) RemoveMember(ctx context.Context, id string) error {
	endpoint := fmt.Sprintf("/team/members/%s", id)
	_, err := c.Do(ctx, "DELETE", endpoint, nil, nil)
	return err
}

// UpdateProfile updates the current user's display name.
func (c *Client) UpdateProfile(ctx context.Context, name string) error {
	payload := struct {
		Name string `json:"name"`
	}{Name: name}
	_, err := c.Do(ctx, "PUT", "/profile", payload, nil)
	return err
}

// ChangePassword changes the current user's password.
func (c *Client) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	payload := struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}{CurrentPassword: currentPassword, NewPassword: newPassword}
	_, err := c.Do(ctx, "PUT", "/profile/password", payload, nil)
	return err
}
