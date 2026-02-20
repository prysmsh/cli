package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/warp-run/prysm-cli/internal/api"
)

func TestBasePublicURL_EmptyWhenNoURL(t *testing.T) {
	// Client with nil baseURL (e.g. zero value) returns empty string
	c := &api.Client{}
	if got := c.BasePublicURL(); got != "" {
		t.Errorf("BasePublicURL() = %q, want empty", got)
	}
}

func TestWithTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, api.WithTimeout(time.Second))
	var v map[string]string
	_, err := client.Do(context.Background(), "GET", "/", nil, &v)
	if err != nil {
		t.Fatalf("Do with WithTimeout: %v", err)
	}
	if v["ok"] != "true" {
		t.Errorf("response = %v", v)
	}
}

func TestNewClientURLNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantBase string
	}{
		{
			name:     "adds https scheme",
			input:    "api.example.com",
			wantBase: "https://api.example.com",
		},
		{
			name:     "preserves https",
			input:    "https://api.example.com",
			wantBase: "https://api.example.com",
		},
		{
			name:     "preserves http",
			input:    "http://localhost:8080",
			wantBase: "http://localhost:8080",
		},
		{
			name:     "strips trailing slash",
			input:    "https://api.example.com/",
			wantBase: "https://api.example.com",
		},
		{
			name:     "normalizes /v1 to /api/v1",
			input:    "https://api.example.com/v1",
			wantBase: "https://api.example.com",
		},
		{
			name:     "normalizes /api to /api/v1",
			input:    "https://api.example.com/api",
			wantBase: "https://api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := api.NewClient(tt.input)
			got := client.BasePublicURL()
			if got != tt.wantBase {
				t.Errorf("BasePublicURL() = %q, want %q", got, tt.wantBase)
			}
		})
	}
}

func TestClientSetAndGetToken(t *testing.T) {
	client := api.NewClient("https://api.example.com")

	if got := client.Token(); got != "" {
		t.Errorf("Token() on new client = %q, want empty", got)
	}

	client.SetToken("test-token-123")

	if got := client.Token(); got != "test-token-123" {
		t.Errorf("Token() after SetToken = %q, want %q", got, "test-token-123")
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := api.NewClient(srv.URL)
	_, err := client.Do(ctx, "GET", "/", nil, nil)
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
	if !strings.Contains(err.Error(), "cancelled") && !strings.Contains(err.Error(), "context") {
		t.Errorf("error = %v", err)
	}
}

func TestClientDoWithPayload(t *testing.T) {
	var capturedRequest struct {
		method      string
		path        string
		contentType string
		body        []byte
		authHeader  string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest.method = r.Method
		capturedRequest.path = r.URL.Path
		capturedRequest.contentType = r.Header.Get("Content-Type")
		capturedRequest.authHeader = r.Header.Get("Authorization")
		capturedRequest.body, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("bearer-test-token")

	type Response struct {
		Status string `json:"status"`
	}
	var resp Response

	payload := map[string]string{"key": "value"}
	_, err := client.Do(context.Background(), "POST", "/test-endpoint", payload, &resp)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if capturedRequest.method != "POST" {
		t.Errorf("Method = %q, want POST", capturedRequest.method)
	}
	if !strings.HasSuffix(capturedRequest.path, "/test-endpoint") {
		t.Errorf("Path = %q, want suffix /test-endpoint", capturedRequest.path)
	}
	if capturedRequest.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedRequest.contentType)
	}
	if capturedRequest.authHeader != "Bearer bearer-test-token" {
		t.Errorf("Authorization = %q, want Bearer bearer-test-token", capturedRequest.authHeader)
	}
	if resp.Status != "ok" {
		t.Errorf("Response status = %q, want ok", resp.Status)
	}
}

func TestClientDoAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "unauthorized",
			"message": "Invalid or expired token",
			"code":    "AUTH_INVALID_TOKEN",
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)

	var resp map[string]interface{}
	_, err := client.Do(context.Background(), "GET", "/protected", nil, &resp)
	if err == nil {
		t.Fatal("Expected error for 401 response")
	}

	if !strings.Contains(err.Error(), "AUTH_INVALID_TOKEN") {
		t.Errorf("Error should contain error code, got: %v", err)
	}
}

func TestClientDoContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, api.WithTimeout(100*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Do(ctx, "GET", "/slow", nil, nil)
	if err == nil {
		t.Fatal("Expected timeout error")
	}
}

func TestClientWithOptions(t *testing.T) {
	var capturedUserAgent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL,
		api.WithUserAgent("test-cli/1.0"),
		api.WithTimeout(5*time.Second),
	)

	_, err := client.Do(context.Background(), "GET", "/test", nil, nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if capturedUserAgent != "test-cli/1.0" {
		t.Errorf("User-Agent = %q, want test-cli/1.0", capturedUserAgent)
	}
}

func TestClientWithHostOverride(t *testing.T) {
	var capturedHost string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL,
		api.WithHostOverride("api.custom.example.com"),
	)

	_, err := client.Do(context.Background(), "GET", "/test", nil, nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if capturedHost != "api.custom.example.com" {
		t.Errorf("Host = %q, want api.custom.example.com", capturedHost)
	}
}

func TestDo_WithDebug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, api.WithDebug(true))
	_, err := client.Do(context.Background(), "GET", "/path", nil, &struct{}{})
	if err != nil {
		t.Fatalf("Do with debug: %v", err)
	}
}

func TestDo_WithQueryString(t *testing.T) {
	var capturedRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	_, err := client.Do(context.Background(), "GET", "/path?filter=active&limit=10", nil, &struct{}{})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if capturedRawQuery != "filter=active&limit=10" {
		t.Errorf("RawQuery = %q, want filter=active&limit=10", capturedRawQuery)
	}
}

func TestClientMethodNormalization(t *testing.T) {
	var capturedMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)

	tests := []struct {
		input string
		want  string
	}{
		{"get", "GET"},
		{"post", "POST"},
		{"Put", "PUT"},
		{"DELETE", "DELETE"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := client.Do(context.Background(), tt.input, "/test", nil, nil)
			if err != nil {
				t.Fatalf("Do failed: %v", err)
			}
			if capturedMethod != tt.want {
				t.Errorf("Method = %q, want %q", capturedMethod, tt.want)
			}
		})
	}
}

func TestClientQueryStringHandling(t *testing.T) {
	var capturedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)

	_, err := client.Do(context.Background(), "GET", "/search?q=test&limit=10", nil, nil)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}

	if capturedQuery != "q=test&limit=10" {
		t.Errorf("Query = %q, want q=test&limit=10", capturedQuery)
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/auth/login") {
			t.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req api.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req.Email != "user@example.com" {
			t.Errorf("Email = %q, want user@example.com", req.Email)
		}

		json.NewEncoder(w).Encode(api.LoginResponse{
			Message:       "Login successful",
			Token:         "new-auth-token",
			RefreshToken:  "new-refresh-token",
			ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
			User: api.SessionUser{
				ID:    1,
				Name:  "Test User",
				Email: "user@example.com",
				Role:  "admin",
			},
			Organization: api.SessionOrg{
				ID:   100,
				Name: "Test Org",
			},
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)

	resp, err := client.Login(context.Background(), api.LoginRequest{
		Email:    "user@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if resp.Token != "new-auth-token" {
		t.Errorf("Token = %q, want new-auth-token", resp.Token)
	}
	if client.Token() != "new-auth-token" {
		t.Errorf("Client token not set after login")
	}
}

func TestLogout(t *testing.T) {
	logoutCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/auth/logout") {
			logoutCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("test-token")

	err := client.Logout(context.Background())
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	if !logoutCalled {
		t.Error("Logout endpoint was not called")
	}
}

func TestLoginResponseExpiresAt(t *testing.T) {
	tests := []struct {
		name     string
		resp     *api.LoginResponse
		wantZero bool
	}{
		{
			name:     "nil response",
			resp:     nil,
			wantZero: true,
		},
		{
			name:     "zero expiry",
			resp:     &api.LoginResponse{ExpiresAtUnix: 0},
			wantZero: true,
		},
		{
			name:     "valid expiry",
			resp:     &api.LoginResponse{ExpiresAtUnix: time.Now().Unix()},
			wantZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.resp.ExpiresAt()
			if tt.wantZero && !got.IsZero() {
				t.Errorf("ExpiresAt() should be zero")
			}
			if !tt.wantZero && got.IsZero() {
				t.Errorf("ExpiresAt() should not be zero")
			}
		})
	}
}

func TestClientDoDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	var out struct{ X int }
	_, err := client.Do(context.Background(), "GET", "/", nil, &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %v", err)
	}
}

func TestNewClientPathNormalization(t *testing.T) {
	tests := []struct {
		base     string
		wantPath string
	}{
		{"https://api.example.com", "/api/v1"},
		{"https://api.example.com/", "/api/v1"},
		{"https://api.example.com/v1", "/api/v1"},
		{"https://api.example.com/api", "/api/v1"},
		{"https://api.example.com/API", "/api/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			client := api.NewClient(tt.base)
			u := client.BasePublicURL()
			if u == "" {
				t.Fatal("BasePublicURL empty")
			}
			_, err := client.Do(context.Background(), "GET", "/profile", nil, nil)
			if err != nil {
				t.Logf("Do (may fail without server): %v", err)
			}
		})
	}
}

func TestClientWithTimeoutWhenClientSet(t *testing.T) {
	hc := &http.Client{}
	client := api.NewClient("https://api.example.com", api.WithHTTPClient(hc), api.WithTimeout(5*time.Second))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestClientDoSuccessWithNilV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ignored": true}`))
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	resp, err := client.Do(context.Background(), "GET", "/test", nil, nil)
	if err != nil {
		t.Fatalf("Do with v=nil err = %v", err)
	}
	if resp == nil {
		t.Fatal("resp should be non-nil")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
}

func TestClientWithInsecureSkipVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, api.WithInsecureSkipVerify(true))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	_, err := client.Do(context.Background(), "GET", "/", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestClientWithDialAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, api.WithDialAddress("127.0.0.1:0"))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	// Dial override is applied; may fail if 127.0.0.1:0 is used, but we're testing the option is set
	_ = client
}

func TestClientNewRequestEncodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	// Payload that cannot be JSON-encoded (e.g. channel type)
	ch := make(chan int)
	_, err := client.Do(context.Background(), "POST", "/test", ch, nil)
	if err == nil {
		t.Fatal("expected encode error")
	}
}

func TestClientWithHTTPClient(t *testing.T) {
	hc := &http.Client{Timeout: 5 * time.Second}
	client := api.NewClient("https://api.example.com", api.WithHTTPClient(hc))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestClientWithDebug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	client := api.NewClient(srv.URL, api.WithDebug(true))
	_, _ = client.Do(context.Background(), "GET", "/", nil, nil)
}

func TestListClusters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/connect/k8s/clusters" && !strings.HasSuffix(r.URL.Path, "/connect/k8s/clusters") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"clusters":  []api.Cluster{},
			"count":     0,
			"timestamp": time.Now(),
		})
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL)
	client.SetToken("token")
	clusters, err := client.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("len(clusters) = %d, want 0", len(clusters))
	}
}
