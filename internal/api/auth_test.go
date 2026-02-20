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

func TestLogin_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_credentials", "message": "Bad email or password"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, err := client.Login(context.Background(), api.LoginRequest{Email: "u@e.com", Password: "wrong"})
	if err == nil {
		t.Fatal("expected error on login failure")
	}
	if client.Token() != "" {
		t.Error("token should not be set on login failure")
	}
}

func TestGetProfile_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	_, err := client.GetProfile(context.Background())
	if err == nil {
		t.Fatal("expected error when GetProfile returns 500")
	}
}

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

func TestRequestDeviceCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/auth/device/code") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.DeviceCodeResponse{
			DeviceCode:              "dc-123",
			UserCode:                "ABCD-1234",
			VerificationURI:         "https://auth.example.com/device",
			VerificationURIComplete: "https://auth.example.com/device?code=ABCD-1234",
			ExpiresIn:                900,
			Interval:                5,
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	resp, err := client.RequestDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if resp.DeviceCode != "dc-123" || resp.UserCode != "ABCD-1234" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestPollDeviceToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/auth/device/token") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.DeviceTokenResponse{
			Token:     "access-token-123",
			ExpiresAt: 1234567890,
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	resp, err := client.PollDeviceToken(context.Background(), "dc-123")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if resp.Token != "access-token-123" {
		t.Errorf("Token = %q", resp.Token)
	}
}

func TestPollDeviceTokenErrorInBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(api.DeviceTokenResponse{
			Error: "authorization_pending",
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	resp, err := client.PollDeviceToken(context.Background(), "dc-123")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if resp.Error != "authorization_pending" {
		t.Errorf("Error = %q", resp.Error)
	}
}

func TestPollDeviceTokenContextCancelled(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := api.NewClient(srv.URL)
	_, err := client.PollDeviceToken(ctx, "dc-123")
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
	if ctx.Err() == nil {
		t.Error("context should be cancelled")
	}
}

func TestPollDeviceTokenDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, err := client.PollDeviceToken(context.Background(), "dc-123")
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %v", err)
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
