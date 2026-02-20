package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warp-run/prysm-cli/internal/api"
)

func TestGetProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/profile") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.ProfileResponse{
			User: api.ProfileUser{
				ID:            1,
				Name:          "Test User",
				Email:         "user@example.com",
				Role:          "admin",
				EmailVerified: true,
				MFAEnabled:    false,
			},
			Organizations:  []api.ProfileOrg{{ID: 100, Name: "Org", Role: "admin", Status: "active"}},
			ApprovalStatus: "approved",
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	resp, err := client.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if resp.User.Email != "user@example.com" {
		t.Errorf("User.Email = %q", resp.User.Email)
	}
	if len(resp.Organizations) != 1 || resp.Organizations[0].Name != "Org" {
		t.Errorf("Organizations = %v", resp.Organizations)
	}
}

func TestGetDERPTunnelToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/auth/derp-tunnel-token") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.DERPTunnelTokenResponse{
			Token:     "derp-jwt-token",
			ExpiresAt: "2025-12-31T00:00:00Z",
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")

	// With device_id
	resp, err := client.GetDERPTunnelToken(context.Background(), "device-123")
	if err != nil {
		t.Fatalf("GetDERPTunnelToken: %v", err)
	}
	if resp.Token != "derp-jwt-token" {
		t.Errorf("Token = %q", resp.Token)
	}

	// Without device_id
	resp2, err := client.GetDERPTunnelToken(context.Background(), "")
	if err != nil {
		t.Fatalf("GetDERPTunnelToken(empty): %v", err)
	}
	if resp2.Token != "derp-jwt-token" {
		t.Errorf("Token = %q", resp2.Token)
	}
}
