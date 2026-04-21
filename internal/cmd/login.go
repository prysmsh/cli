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

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/session"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

const oauthCallbackPort = 4208

// printLoginWelcome prints the post-login success banner plus a short
// "what now" hint pointing at the core expose flow. The goal is to cut the
// dead zone between authentication and the first meaningful action.
func printLoginWelcome(name, email string) {
	fmt.Println(style.Success.Render(fmt.Sprintf("Login successful — welcome, %s (%s)", name, email)))
	fmt.Println()
	fmt.Println(style.MutedStyle.Render("  Try it:    prysm tunnel expose 8080 --public"))
	fmt.Println(style.MutedStyle.Render("  Docs:      prysm --help"))
	fmt.Println()
}

// isSSHSession returns true when the process is running inside an SSH session
// (SSH_CONNECTION or SSH_CLIENT are set by sshd). Used to auto-select device-code
// flow when no browser is available.
func isSSHSession() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != ""
}

func newLoginCommand() *cobra.Command {
	var (
		useGitHub     bool
		useApple      bool
		useEmail      bool
		useDeviceCode bool
		password      string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to the Prysm control plane",
		Long:  "Opens the browser to sign in. Defaults to the web login page; use --github or --apple for direct OAuth, --email for email/password, or --device-code for headless environments.\n\nFor scripted/CI use: prysm login --email --password <password>",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if useDeviceCode {
				if useGitHub || useApple || useEmail {
					return fmt.Errorf("--device-code cannot be combined with --github, --apple, or --email")
				}
				return runDeviceCodeLogin(cmd.Context(), app)
			}

			// Direct email+password login (non-interactive)
			// Strip backslash escapes from password — zsh history expansion
			// often causes \! to appear when users pass passwords containing !
			password = strings.ReplaceAll(password, `\!`, `!`)
			if password != "" {
				emailAddr := os.Getenv("PRYSM_EMAIL")
				if emailAddr == "" {
					// Check if positional arg provided: prysm login --password xxx user@example.com
					if len(args) > 0 {
						emailAddr = args[0]
					}
				}
				if emailAddr == "" {
					return fmt.Errorf("--password requires an email address: prysm login --password <pwd> <email>, or set PRYSM_EMAIL")
				}
				return runPasswordLogin(cmd.Context(), app, emailAddr, password)
			}

			provider := ""
			if useGitHub {
				provider = "github"
			} else if useApple {
				provider = "apple"
			} else if useEmail {
				provider = "email"
			}

			// In SSH there is no browser; use device-code unless an explicit provider was set.
			if provider == "" && isSSHSession() {
				return runDeviceCodeLogin(cmd.Context(), app)
			}
			return runOAuthLogin(cmd.Context(), app, provider)
		},
	}

	cmd.Flags().BoolVar(&useGitHub, "github", false, "open GitHub sign-in directly")
	cmd.Flags().BoolVar(&useApple, "apple", false, "open Apple sign-in directly")
	cmd.Flags().BoolVar(&useEmail, "email", false, "open email/password sign-in")
	cmd.Flags().BoolVar(&useDeviceCode, "device-code", false, "use device code flow for headless environments (SSH, containers)")
	cmd.Flags().StringVar(&password, "password", "", "password for email/password login (use with --email; for CI/scripts)")

	return cmd
}

// runPasswordLogin performs direct email+password authentication (no browser required).
func runPasswordLogin(ctx context.Context, app *App, email, password string) error {
	loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if app.Debug {
		fmt.Fprintf(os.Stderr, "[debug] login email=%q\n", email)
	}
	var loginResp *api.LoginResponse
	if err := ui.WithSpinner("Signing in...", func() error {
		var err error
		loginResp, err = app.API.Login(loginCtx, api.LoginRequest{
			Email:    email,
			Password: password,
		})
		return err
	}); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	orgID := int64(0)
	orgName := ""
	if loginResp.Organization.ID != 0 {
		orgID = loginResp.Organization.ID
		orgName = loginResp.Organization.Name
	}

	sess := &session.Session{
		Token:         loginResp.Token,
		RefreshToken:  loginResp.RefreshToken,
		Email:         loginResp.User.Email,
		ExpiresAtUnix: loginResp.ExpiresAtUnix,
		User: session.SessionUser{
			ID:         loginResp.User.ID,
			Name:       loginResp.User.Name,
			Email:      loginResp.User.Email,
			Role:       loginResp.User.Role,
			MFAEnabled: loginResp.User.MFAEnabled,
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

	printLoginWelcome(loginResp.User.Name, loginResp.User.Email)
	return nil
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
		token        string
		expiresAt    int64
		refreshToken string
		err          error
	}
	done := make(chan result, 1)

	// Start local callback server
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		printDebug("OAuth callback received (state=%s, has_token=%v, has_code=%v)", q.Get("state"), q.Get("token") != "", q.Get("code") != "")
		callbackState := q.Get("state")
		if callbackState != state {
			http.Error(w, "Invalid state parameter (possible CSRF)", http.StatusBadRequest)
			done <- result{err: errors.New("OAuth state mismatch - possible CSRF attack")}
			return
		}

		token := q.Get("token")
		expStr := q.Get("expires_at")
		refreshToken := q.Get("refresh_token")

		// Backend sends a short-lived code instead of the token directly.
		// Exchange it for real credentials via the backend API.
		if token == "" {
			code := q.Get("code")
			if code == "" {
				http.Error(w, "Missing token or code", http.StatusBadRequest)
				done <- result{err: errors.New("callback missing token and code")}
				return
			}
			exchResp, err := app.API.ExchangeCLICode(r.Context(), code)
			if err != nil {
				http.Error(w, "Code exchange failed", http.StatusInternalServerError)
				done <- result{err: fmt.Errorf("exchange CLI auth code: %w", err)}
				return
			}
			token = exchResp.Token
			expStr = fmt.Sprintf("%d", exchResp.ExpiresAt)
			refreshToken = exchResp.RefreshToken
		}

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
		w.Write([]byte(oauthSuccessPage))
		done <- result{token: token, expiresAt: expiresAt, refreshToken: refreshToken}
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

	providerLabel := ""
	switch provider {
	case "apple":
		providerLabel = " with Apple"
	case "email":
		providerLabel = " with email"
	case "github":
		providerLabel = " with GitHub"
	case "google":
		providerLabel = " with Google"
	case "microsoftonline":
		providerLabel = " with Microsoft"
	}

	fmt.Fprintln(os.Stderr)
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintln(os.Stderr, style.Warning.Render("  Could not open browser automatically."))
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  Open this URL to sign in"+providerLabel+":")
		fmt.Fprintln(os.Stderr, "  "+style.Info.Render(authURL))
	} else {
		fmt.Fprintln(os.Stderr, style.Success.Render("  Browser opened")+style.MutedStyle.Render(" — complete sign-in"+providerLabel+" in the browser"))
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, style.MutedStyle.Render("  If it didn't open: "+authURL))
	}
	fmt.Fprintln(os.Stderr)

	timeout := 5 * time.Minute
	printDebug("Waiting for OAuth callback (timeout %v)...", timeout)

	// Spinner while waiting for browser callback
	var callbackRes result
	_ = ui.WithSpinner("Waiting for sign-in...", func() error {
		select {
		case r := <-done:
			callbackRes = r
		case <-time.After(timeout):
			callbackRes = result{err: fmt.Errorf("login timed out after %v — complete sign-in in the browser, or ensure localhost:%d is reachable", timeout, oauthCallbackPort)}
		case <-ctx.Done():
			callbackRes = result{err: ctx.Err()}
		}
		return nil
	})

	if callbackRes.err != nil {
		return callbackRes.err
	}

	// Fetch profile to get user/org info
	app.API.SetToken(callbackRes.token)
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
		Token:         callbackRes.token,
		RefreshToken:  callbackRes.refreshToken,
		Email:         profile.User.Email,
		ExpiresAtUnix: callbackRes.expiresAt,
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
	printLoginWelcome(profile.User.Name, profile.User.Email)
	return nil
}

// runDeviceCodeLogin performs the OAuth Device Authorization Grant flow (RFC 8628).
// This is designed for headless environments where a browser cannot be opened locally.
func runDeviceCodeLogin(ctx context.Context, app *App) error {
	printDebug("Starting device code login flow")

	dcResp, err := app.API.RequestDeviceCode(ctx)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode >= 500 || strings.Contains(strings.ToLower(apiErr.Message), "failed to create device code")) {
			return fmt.Errorf("request device code: %w — this is usually a temporary server issue; try again in a few minutes or use `prysm login` in a browser", err)
		}
		return fmt.Errorf("request device code: %w", err)
	}
	printDebug("Device code response: user_code=%s, expires_in=%d, interval=%d", dcResp.UserCode, dcResp.ExpiresIn, dcResp.Interval)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, style.Info.Render("To sign in, open this URL on any device:"))
	fmt.Fprintf(os.Stderr, "\n    %s\n\n", dcResp.VerificationURI)
	fmt.Fprintln(os.Stderr, style.Info.Render("Then enter the code:"))
	fmt.Fprint(os.Stderr, style.Code.Render("\n    "+dcResp.UserCode+"\n\n"))

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

	return ui.WithSpinner(fmt.Sprintf("Waiting for authorization... (expires in %d minutes)", int(expiresIn.Minutes())), func() error {
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
						RefreshToken:  tokenResp.RefreshToken,
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
					printLoginWelcome(profile.User.Name, profile.User.Email)
					return nil

				case "authorization_pending":
					continue

				case "slow_down":
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
	})
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

// oauthSuccessPage is served after a successful OAuth callback.
// It clears the token from the URL bar and shows a styled confirmation.
const oauthSuccessPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Prysm — Signed in</title>
<script>if(window.history.replaceState)window.history.replaceState({},"","/");</script>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
display:flex;align-items:center;justify-content:center;min-height:100vh;
background:#0f0f13;color:#e4e4e7}
.card{text-align:center;padding:3rem 2.5rem;border-radius:16px;
background:#18181b;border:1px solid #27272a;max-width:420px;width:90%}
.icon{font-size:3rem;margin-bottom:1rem}
h1{font-size:1.25rem;font-weight:600;margin-bottom:.5rem;color:#f4f4f5}
p{color:#a1a1aa;font-size:.9rem;line-height:1.5}
.brand{color:#818cf8;font-weight:600}
.hint{margin-top:1.5rem;padding:.75rem 1rem;border-radius:8px;
background:#1e1e24;font-family:"SF Mono",Menlo,monospace;font-size:.8rem;color:#71717a}
</style>
</head>
<body>
<div class="card">
  <div class="icon">&#x2714;&#xFE0F;</div>
  <h1>Signed in to <span class="brand">Prysm</span></h1>
  <p>You can close this tab and return to your terminal.</p>
  <div class="hint">The CLI session is now active.</div>
</div>
</body>
</html>`

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
