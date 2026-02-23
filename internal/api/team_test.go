package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/api"
)

func TestListTeamMembers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/team/members") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"members": []map[string]interface{}{
				{"id": 1, "role": "admin", "user": map[string]interface{}{"id": 10, "name": "Alice", "email": "alice@test.com"}},
				{"id": 2, "role": "member", "user": map[string]interface{}{"id": 11, "name": "Bob", "email": "bob@test.com"}},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	members, err := client.ListTeamMembers(context.Background())
	if err != nil {
		t.Fatalf("ListTeamMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("len = %d, want 2", len(members))
	}
	if members[0].User.Email != "alice@test.com" {
		t.Errorf("members[0].User.Email = %q", members[0].User.Email)
	}
}

func TestListTeamMembers_NilMembers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	members, err := client.ListTeamMembers(context.Background())
	if err != nil {
		t.Fatalf("ListTeamMembers: %v", err)
	}
	if members == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(members) != 0 {
		t.Errorf("len = %d, want 0", len(members))
	}
}

func TestListTeamMembers_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, err := client.ListTeamMembers(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInviteTeamMember(t *testing.T) {
	var gotEmail, gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/team/invite") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotEmail = body.Email
		gotRole = body.Role
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.InviteTeamMember(context.Background(), "new@test.com", "member")
	if err != nil {
		t.Fatalf("InviteTeamMember: %v", err)
	}
	if gotEmail != "new@test.com" {
		t.Errorf("email = %q", gotEmail)
	}
	if gotRole != "member" {
		t.Errorf("role = %q", gotRole)
	}
}

func TestInviteTeamMember_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"already invited"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.InviteTeamMember(context.Background(), "dup@test.com", "member")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListInvitations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/team/invitations") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invitations": []map[string]interface{}{
				{"id": 1, "email": "invited@test.com", "role": "member", "status": "pending"},
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	invitations, err := client.ListInvitations(context.Background())
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	if len(invitations) != 1 {
		t.Fatalf("len = %d, want 1", len(invitations))
	}
	if invitations[0].Email != "invited@test.com" {
		t.Errorf("Email = %q", invitations[0].Email)
	}
}

func TestListInvitations_NilInvitations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	invitations, err := client.ListInvitations(context.Background())
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	if invitations == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(invitations) != 0 {
		t.Errorf("len = %d, want 0", len(invitations))
	}
}

func TestRevokeInvitation(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != "DELETE" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.RevokeInvitation(context.Background(), "42")
	if err != nil {
		t.Fatalf("RevokeInvitation: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/team/invitations/42") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestUpdateMemberRole(t *testing.T) {
	var gotPath, gotRole string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != "PUT" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Role string `json:"role"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotRole = body.Role
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.UpdateMemberRole(context.Background(), "5", "admin")
	if err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/team/members/5") {
		t.Errorf("path = %q", gotPath)
	}
	if gotRole != "admin" {
		t.Errorf("role = %q", gotRole)
	}
}

func TestRemoveMember(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.RemoveMember(context.Background(), "7")
	if err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %q", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/team/members/7") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestUpdateProfile(t *testing.T) {
	var gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/profile") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotName = body.Name
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.UpdateProfile(context.Background(), "New Name")
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if gotName != "New Name" {
		t.Errorf("name = %q", gotName)
	}
}

func TestChangePassword(t *testing.T) {
	var gotCurrent, gotNew string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || !strings.HasSuffix(r.URL.Path, "/profile/password") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotCurrent = body.CurrentPassword
		gotNew = body.NewPassword
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.ChangePassword(context.Background(), "old-pass", "new-pass")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if gotCurrent != "old-pass" {
		t.Errorf("current = %q", gotCurrent)
	}
	if gotNew != "new-pass" {
		t.Errorf("new = %q", gotNew)
	}
}

func TestChangePassword_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"password too weak"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.ChangePassword(context.Background(), "old", "123")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/auth/refresh") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "new-token",
			"expires_at": 9999999999,
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	resp, err := client.RefreshSession(context.Background(), "refresh-token-1")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if resp.Token != "new-token" {
		t.Errorf("Token = %q", resp.Token)
	}
	if resp.ExpiresAtUnix != 9999999999 {
		t.Errorf("ExpiresAtUnix = %d", resp.ExpiresAtUnix)
	}
}

func TestRefreshSession_EmptyRefreshToken(t *testing.T) {
	client := api.NewClient("http://unused")
	_, err := client.RefreshSession(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty refresh token")
	}
}

func TestRefreshSession_MissingTokenInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "",
			"expires_at": 0,
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	_, err := client.RefreshSession(context.Background(), "some-refresh-token")
	if err == nil {
		t.Fatal("expected error for missing token in response")
	}
}

func TestRefreshSession_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid refresh token"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	_, err := client.RefreshSession(context.Background(), "expired-token")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDisconnectMeshNode(t *testing.T) {
	var gotMethod string
	var gotPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DisconnectMeshNode(context.Background(), "dev-42")
	if err != nil {
		t.Fatalf("DisconnectMeshNode: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPayload["device_id"] != "dev-42" {
		t.Errorf("device_id = %v", gotPayload["device_id"])
	}
	if gotPayload["status"] != "disconnected" {
		t.Errorf("status = %v", gotPayload["status"])
	}
}

func TestDisconnectCluster(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DisconnectCluster(context.Background(), 99)
	if err != nil {
		t.Fatalf("DisconnectCluster: %v", err)
	}
	if !strings.Contains(gotPath, "/clusters/99/disconnect") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDeleteCluster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || !strings.Contains(r.URL.Path, "/clusters/55") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DeleteCluster(context.Background(), 55)
	if err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}
}

func TestDeleteCluster_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"cluster is still connected"}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	err := client.DeleteCluster(context.Background(), 55)
	if err == nil {
		t.Fatal("expected error")
	}
}
