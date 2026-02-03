package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/cmdutil"
	"github.com/warp-run/prysm-cli/internal/session"
	"github.com/warp-run/prysm-cli/internal/util"
)

const oauthCallbackPort = 4208

func newLoginCommand() *cobra.Command {
	var (
		email      string
		password   string
		totp       string
		backupCode string
		useGitHub  bool
		useApple   bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to the Prysm control plane",
		Long:  "Authenticate with email/password or via OAuth (GitHub, Apple). Use --github or --apple to sign in with your GitHub or Apple ID.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()

			if useGitHub {
				return runOAuthLogin(cmd.Context(), app, "github")
			}
			if useApple {
				return runOAuthLogin(cmd.Context(), app, "apple")
			}

			if email == "" {
				var err error
				email, err = util.PromptInput("Email")
				if err != nil {
					return err
				}
			}
			email = strings.TrimSpace(email)
			if email == "" {
				return errors.New("email is required")
			}

			if password == "" {
				var err error
				password, err = util.PromptPassword("Password")
				if err != nil {
					return err
				}
			}

			req := api.LoginRequest{
				Email:      email,
				Password:   password,
				TOTPCode:   totp,
				BackupCode: backupCode,
			}

			ctx, cancel := cmdutil.ContextWithTimeout(cmd.Context(), cmdutil.LongTimeout)
			defer cancel()

			printDebug("Attempting login for %s", email)

			resp, err := app.API.Login(ctx, req)
			if err != nil {
				printDebug("Login failed: %v", err)
				return err
			}

			printDebug("Login successful for %s", resp.User.Email)

			sess := &session.Session{
				Token:         resp.Token,
				RefreshToken:  resp.RefreshToken,
				Email:         resp.User.Email,
				SessionID:     resp.SessionID,
				CSRFToken:     resp.CSRFToken,
				ExpiresAtUnix: resp.ExpiresAtUnix,
				User: session.SessionUser{
					ID:         resp.User.ID,
					Name:       resp.User.Name,
					Email:      resp.User.Email,
					Role:       resp.User.Role,
					MFAEnabled: resp.User.MFAEnabled,
				},
				Organization: session.SessionOrg{
					ID:   resp.Organization.ID,
					Name: resp.Organization.Name,
				},
				APIBaseURL:    app.Config.APIBaseURL,
				ComplianceURL: app.Config.ComplianceURL,
				DERPServerURL: app.Config.DERPServerURL,
				OutputFormat:  app.OutputFormat,
			}

			if err := app.Sessions.Save(sess); err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("✅ Login successful — welcome, %s (%s)\n", resp.User.Name, resp.User.Email)
			return nil
		},
	}

	cmd.Flags().StringVarP(&email, "email", "e", "", "email address")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password (not recommended to use via flag)")
	cmd.Flags().StringVar(&totp, "totp", "", "TOTP code for MFA")
	cmd.Flags().StringVar(&backupCode, "backup-code", "", "backup code for MFA")
	cmd.Flags().BoolVar(&useGitHub, "github", false, "sign in with GitHub")
	cmd.Flags().BoolVar(&useApple, "apple", false, "sign in with Apple ID")

	return cmd
}

// runOAuthLogin performs OAuth login via browser and local callback server.
func runOAuthLogin(ctx context.Context, app *App, provider string) error {
	baseURL := strings.TrimSuffix(app.Config.APIBaseURL, "/")
	if !strings.Contains(baseURL, "/api/v1") {
		baseURL = baseURL + "/api/v1"
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/oauth/callback", oauthCallbackPort)
	authURL := fmt.Sprintf("%s/auth/%s?redirect_uri=%s", baseURL, provider, url.QueryEscape(redirectURI))

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
		token := r.URL.Query().Get("token")
		expStr := r.URL.Query().Get("expires_at")
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

	providerDisplay := provider
	if provider == "apple" {
		providerDisplay = "Apple"
	} else if provider == "google" {
		providerDisplay = "Google"
	} else if provider == "github" {
		providerDisplay = "GitHub"
	} else if provider == "microsoftonline" {
		providerDisplay = "Microsoft"
	}
	color.New(color.FgCyan).Printf("Opening browser to sign in with %s...\n", providerDisplay)
	if err := openBrowser(authURL); err != nil {
		printDebug("Could not open browser: %v", err)
		fmt.Fprintf(color.Error, "Please open this URL in your browser:\n%s\n", authURL)
	}

	// Wait for callback or context cancel
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
	case <-ctx.Done():
		return ctx.Err()
	}
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

// Deprecated: use util.PromptInput instead.
func promptInput(label string) (string, error) {
	return util.PromptInput(label)
}

// Deprecated: use util.PromptPassword instead.
func promptPassword(label string) (string, error) {
	return util.PromptPassword(label)
}
