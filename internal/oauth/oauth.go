package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// CallbackResult holds the OAuth callback data from the backend redirect.
type CallbackResult struct {
	Token          string
	ExpiresAtUnix  int64
	CSRFToken      string
	UserID         int64
	Email          string
	Name           string
	OrgID          int64
	OrgName        string
	ApprovalStatus string
}

// LoginWithBrowser starts a local HTTP server, opens the browser to the OAuth URL,
// and waits for the callback with the token. Returns the callback result or an error.
func LoginWithBrowser(ctx context.Context, apiBaseURL, provider string) (*CallbackResult, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	authURL := strings.TrimSuffix(apiBaseURL, "/") + "/auth/" + provider + "?redirect_uri=" + url.QueryEscape(redirectURI)

	resultCh := make(chan *CallbackResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		token := q.Get("token")
		if token == "" {
			errCh <- fmt.Errorf("no token in callback")
			http.Error(w, "Login failed: no token received.", http.StatusBadRequest)
			return
		}
		expiresAt, _ := strconv.ParseInt(q.Get("expires_at"), 10, 64)
		res := &CallbackResult{
			Token:          token,
			ExpiresAtUnix:  expiresAt,
			CSRFToken:      q.Get("csrf_token"),
			Email:          q.Get("email"),
			Name:           q.Get("name"),
			ApprovalStatus: q.Get("approval_status"),
		}
		res.UserID, _ = strconv.ParseInt(q.Get("user_id"), 10, 64)
		res.OrgID, _ = strconv.ParseInt(q.Get("org_id"), 10, 64)
		res.OrgName = q.Get("org_name")

		resultCh <- res
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Prysm Login</title></head><body>
<h2>Login successful</h2>
<p>You can close this window and return to the terminal.</p>
</body></html>`))
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()

	if err := openBrowser(authURL); err != nil {
		_ = srv.Shutdown(ctx)
		return nil, fmt.Errorf("open browser: %w", err)
	}

	select {
	case res := <-resultCh:
		_ = srv.Shutdown(ctx)
		return res, nil
	case err := <-errCh:
		_ = srv.Shutdown(ctx)
		return nil, err
	case <-ctx.Done():
		_ = srv.Shutdown(ctx)
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		_ = srv.Shutdown(ctx)
		return nil, fmt.Errorf("login timed out after 5 minutes")
	}
}

func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return fmt.Errorf("cannot open browser on %s", runtime.GOOS)
	}
	return cmd.Start()
}
