package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/session"
)

const oauthCallbackPort = 4208

func newLoginCommand() *cobra.Command {
	var (
		useGitHub     bool
		useApple      bool
		useEmail      bool
		useDeviceCode bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to the Prysm control plane",
		Long:  "Opens the browser to sign in. Defaults to the web login page; use --github or --apple for direct OAuth, --email for email/password, or --device-code for headless environments.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if useDeviceCode {
				if useGitHub || useApple || useEmail {
					return fmt.Errorf("--device-code cannot be combined with --github, --apple, or --email")
				}
				return runDeviceCodeLogin(cmd.Context(), app)
			}

			provider := ""
			if useGitHub {
				provider = "github"
			} else if useApple {
				provider = "apple"
			} else if useEmail {
				provider = "email"
			}

			return runOAuthLogin(cmd.Context(), app, provider)
		},
	}

	cmd.Flags().BoolVar(&useGitHub, "github", false, "open GitHub sign-in directly")
	cmd.Flags().BoolVar(&useApple, "apple", false, "open Apple sign-in directly")
	cmd.Flags().BoolVar(&useEmail, "email", false, "open email/password sign-in")
	cmd.Flags().BoolVar(&useDeviceCode, "device-code", false, "use device code flow for headless environments (SSH, containers)")

	return cmd
}

// runOAuthLogin performs OAuth login via browser and local callback server.
func runOAuthLogin(ctx context.Context, app *App, provider string) error {
	baseURL := strings.TrimSuffix(app.Config.APIBaseURL, "/")
	if !strings.Contains(baseURL, "/api/v1") {
		baseURL = baseURL + "/api/v1"
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/oauth/callback", oauthCallbackPort)
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("generate OAuth state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	var authURL string
	if provider == "" {
		// Default: open web login page so user can choose GitHub, Google, email, etc.
		appURL := getAppLoginURL(baseURL)
		authURL = fmt.Sprintf("%s/login?redirect_uri=%s&state=%s", appURL, url.QueryEscape(redirectURI), url.QueryEscape(state))
		provider = "web" // for message display
	} else if provider == "email" {
		// Email: backend redirects to frontend with provider=email
		authURL = fmt.Sprintf("%s/auth/email?redirect_uri=%s&state=%s", baseURL, url.QueryEscape(redirectURI), url.QueryEscape(state))
	} else {
		// Explicit OAuth: github, apple, etc.
		authURL = fmt.Sprintf("%s/auth/%s?redirect_uri=%s&state=%s", baseURL, provider, url.QueryEscape(redirectURI), url.QueryEscape(state))
	}

	// Channel to receive token from callback
	type result struct {
		token     string
		expiresAt int64
		err      error
	}
	done := make(chan result, 1)

	// Start local callback server
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		printDebug("OAuth callback received (state=%s, has_token=%v)", q.Get("state"), q.Get("token") != "")
		callbackState := q.Get("state")
		if callbackState != state {
			http.Error(w, "Invalid state parameter (possible CSRF)", http.StatusBadRequest)
			done <- result{err: errors.New("OAuth state mismatch - possible CSRF attack")}
			return
		}
		token := q.Get("token")
		expStr := q.Get("expires_at")
		if token == "" || expStr == "" {
			http.Error(w, "Missing token or expires_at", http.StatusBadRequest)
			done <- result{err: errors.New("callback missing token or expires_at")}
			return
		}
		var expiresAt int64
		if _, err := fmt.Sscanf(expStr, "%d", &expiresAt); err != nil {
			expiresAt = 0
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><p>Login successful! You can close this window and return to the terminal.</p></body></html>`))
		done <- result{token: token, expiresAt: expiresAt}
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", oauthCallbackPort))
	if err != nil {
		return fmt.Errorf("start callback server: %w", err)
	}
	defer listener.Close()

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()
	defer srv.Shutdown(context.Background())

	printDebug("Callback server listening on http://127.0.0.1:%d/oauth/callback", oauthCallbackPort)
	printDebug("Auth URL: %s", authURL)

	msg := "Opening browser to sign in..."
	if provider == "web" {
		msg = "Opening browser to sign in..."
	} else if provider == "apple" {
		msg = "Opening browser to sign in with Apple..."
	} else if provider == "email" {
		msg = "Opening browser to sign in with email/password..."
	} else if provider == "github" {
		msg = "Opening browser to sign in with GitHub..."
	} else if provider == "google" {
		msg = "Opening browser to sign in with Google..."
	} else if provider == "microsoftonline" {
		msg = "Opening browser to sign in with Microsoft..."
	}
	color.New(color.FgCyan).Println(msg)
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(color.Error, "Please open this URL in your browser:\n%s\n", authURL)
	} else {
		fmt.Fprintf(color.Error, "Complete sign-in in the browser. If it didn't open: %s\n", authURL)
	}
	timeout := 5 * time.Minute
	printDebug("Waiting for OAuth callback (timeout %v)...", timeout)

	// Print periodic "still waiting" so user knows the process is alive
	stopCh := make(chan struct{})
	defer close(stopCh)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				fmt.Fprintf(color.Error, "Still waiting for sign-in... Complete the flow in your browser.\n")
			}
		}
	}()

	// Wait for callback, context cancel, or timeout
	select {
	case res := <-done:
		if res.err != nil {
			return res.err
		}
		// Fetch profile to get user/org info
		app.API.SetToken(res.token)
		profileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		profile, err := app.API.GetProfile(profileCtx)
		if err != nil {
			return fmt.Errorf("fetch profile after login: %w", err)
		}
		orgID := int64(0)
		orgName := ""
		if len(profile.Organizations) > 0 {
			orgID = profile.Organizations[0].ID
			orgName = profile.Organizations[0].Name
		}
		sess := &session.Session{
			Token:        res.token,
			Email:        profile.User.Email,
			ExpiresAtUnix: res.expiresAt,
			User: session.SessionUser{
				ID:         profile.User.ID,
				Name:       profile.User.Name,
				Email:      profile.User.Email,
				Role:       profile.User.Role,
				MFAEnabled: profile.User.MFAEnabled,
			},
			Organization: session.SessionOrg{
				ID:   orgID,
				Name: orgName,
			},
			APIBaseURL:    app.Config.APIBaseURL,
			ComplianceURL: app.Config.ComplianceURL,
			DERPServerURL: app.Config.DERPServerURL,
			OutputFormat:  app.OutputFormat,
		}
		if err := app.Sessions.Save(sess); err != nil {
			return err
		}
		color.New(color.FgGreen).Printf("✅ Login successful — welcome, %s (%s)\n", profile.User.Name, profile.User.Email)
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("login timed out after %v — complete sign-in in the browser, or ensure localhost:%d is reachable", timeout, oauthCallbackPort)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runDeviceCodeLogin performs the OAuth Device Authorization Grant flow (RFC 8628).
// This is designed for headless environments where a browser cannot be opened locally.
func runDeviceCodeLogin(ctx context.Context, app *App) error {
	printDebug("Starting device code login flow")

	dcResp, err := app.API.RequestDeviceCode(ctx)
	if err != nil {
		return fmt.Errorf("request device code: %w", err)
	}
	printDebug("Device code response: user_code=%s, expires_in=%d, interval=%d", dcResp.UserCode, dcResp.ExpiresIn, dcResp.Interval)

	fmt.Fprintln(color.Error)
	color.New(color.FgCyan).Fprintln(color.Error, "To sign in, open this URL on any device:")
	fmt.Fprintf(color.Error, "\n    %s\n\n", dcResp.VerificationURI)
	color.New(color.FgCyan).Fprintln(color.Error, "Then enter the code:")
	color.New(color.FgWhite, color.Bold).Fprintf(color.Error, "\n    %s\n\n", dcResp.UserCode)

	// Best-effort: try to open the browser to the pre-filled URL.
	if dcResp.VerificationURIComplete != "" {
		_ = openBrowser(dcResp.VerificationURIComplete)
	}

	interval := time.Duration(dcResp.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	expiresIn := time.Duration(dcResp.ExpiresIn) * time.Second
	if expiresIn == 0 {
		expiresIn = 15 * time.Minute
	}

	fmt.Fprintf(color.Error, "Waiting for authorization... (expires in %d minutes)\n", int(expiresIn.Minutes()))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := time.After(expiresIn)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("device code expired — please run `prysm login --device-code` again")
		case <-ticker.C:
			printDebug("Polling device token (interval=%v)", interval)
			tokenResp, err := app.API.PollDeviceToken(ctx, dcResp.DeviceCode)
			if err != nil {
				return fmt.Errorf("poll device token: %w", err)
			}

			switch tokenResp.Error {
			case "":
				// Success — save session using the same pattern as runOAuthLogin.
				app.API.SetToken(tokenResp.Token)
				profileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				profile, err := app.API.GetProfile(profileCtx)
				if err != nil {
					return fmt.Errorf("fetch profile after login: %w", err)
				}
				orgID := int64(0)
				orgName := ""
				if len(profile.Organizations) > 0 {
					orgID = profile.Organizations[0].ID
					orgName = profile.Organizations[0].Name
				}
				sess := &session.Session{
					Token:         tokenResp.Token,
					Email:         profile.User.Email,
					ExpiresAtUnix: tokenResp.ExpiresAt,
					User: session.SessionUser{
						ID:         profile.User.ID,
						Name:       profile.User.Name,
						Email:      profile.User.Email,
						Role:       profile.User.Role,
						MFAEnabled: profile.User.MFAEnabled,
					},
					Organization: session.SessionOrg{
						ID:   orgID,
						Name: orgName,
					},
					APIBaseURL:    app.Config.APIBaseURL,
					ComplianceURL: app.Config.ComplianceURL,
					DERPServerURL: app.Config.DERPServerURL,
					OutputFormat:  app.OutputFormat,
				}
				if err := app.Sessions.Save(sess); err != nil {
					return err
				}
				color.New(color.FgGreen).Printf("✅ Login successful — welcome, %s (%s)\n", profile.User.Name, profile.User.Email)
				return nil

			case "authorization_pending":
				// Expected — keep polling.
				continue

			case "slow_down":
				// Server asked us to back off — increase interval by 5s.
				interval += 5 * time.Second
				ticker.Stop()
				ticker = time.NewTicker(interval)
				printDebug("Slowing down poll interval to %v", interval)
				continue

			case "access_denied":
				return fmt.Errorf("authorization denied — the request was rejected")

			case "expired_token":
				return fmt.Errorf("device code expired — please run `prysm login --device-code` again")

			default:
				return fmt.Errorf("device authorization failed: %s", tokenResp.Error)
			}
		}
	}
}

// getAppLoginURL returns the web app URL for login (e.g. https://app.prysm.sh).
func getAppLoginURL(apiBaseURL string) string {
	if u := strings.TrimSpace(os.Getenv("PRYSM_APP_URL")); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	if strings.Contains(apiBaseURL, "api.prysm.sh") {
		return "https://app.prysm.sh"
	}
	return "https://app.prysm.sh"
}

func openBrowser(u string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	case "darwin":
		return exec.Command("open", u).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}
