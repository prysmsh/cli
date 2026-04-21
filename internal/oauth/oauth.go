package oauth

import (
	"context"
	"fmt"
	"html"
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
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(renderLoginCompletePage(loginPageState{
				Failed: true,
				Reason: firstNonEmpty(q.Get("error_description"), q.Get("error"), "no token received"),
			})))
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
		w.Write([]byte(renderLoginCompletePage(loginPageState{
			Email:          res.Email,
			Name:           res.Name,
			OrgName:        res.OrgName,
			ApprovalStatus: res.ApprovalStatus,
		})))
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

// ── CLI login success / failure page ────────────────────────────────────────
//
// The OAuth flow ends with a redirect to http://127.0.0.1:<port>/callback, so
// this page is served by the CLI itself — not by the dashboard. That means:
//
//   - Everything must be inline (no external CSS, no bundled assets). The IBM
//     Plex fonts are loaded from Google Fonts with a system-font fallback, so
//     even offline the page still looks intentional.
//   - The page shows a friendly "you're done" state with whatever identity
//     info the backend passed through (email, name, org), and a clear path
//     back to the terminal.

type loginPageState struct {
	Email          string
	Name           string
	OrgName        string
	ApprovalStatus string
	Failed         bool
	Reason         string
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func renderLoginCompletePage(s loginPageState) string {
	var statusBanner, heroTitle, heroSub, footerCmd string
	if s.Failed {
		heroTitle = `Sign-in <em>didn&rsquo;t complete.</em>`
		reason := s.Reason
		if reason == "" {
			reason = "Unknown error"
		}
		heroSub = `We couldn&rsquo;t finish the hand-off. Return to the terminal and try <code>prysm login</code> again.`
		statusBanner = `<div class="banner err"><span class="bd"></span>ERROR · ` + html.EscapeString(reason) + `</div>`
		footerCmd = "prysm login"
	} else {
		heroTitle = `You&rsquo;re <em>signed in.</em>`
		heroSub = `Return to your terminal — the CLI has the token and is ready to go.`
		switch strings.ToLower(strings.TrimSpace(s.ApprovalStatus)) {
		case "pending":
			statusBanner = `<div class="banner warn"><span class="bd"></span>PENDING · your account is awaiting admin approval</div>`
		case "":
			/* omit */
		default:
			statusBanner = `<div class="banner ok"><span class="bd"></span>AUTH · token stored, session active</div>`
		}
		footerCmd = "prysm whoami"
	}

	// Identity row — only render what we actually have.
	var identity string
	if s.Email != "" || s.Name != "" || s.OrgName != "" {
		var rows []string
		if s.Name != "" {
			rows = append(rows, `<div class="kv"><span class="k">Name</span><span class="v">`+html.EscapeString(s.Name)+`</span></div>`)
		}
		if s.Email != "" {
			rows = append(rows, `<div class="kv"><span class="k">Email</span><span class="v mono">`+html.EscapeString(s.Email)+`</span></div>`)
		}
		if s.OrgName != "" {
			rows = append(rows, `<div class="kv"><span class="k">Org</span><span class="v mono">`+html.EscapeString(s.OrgName)+`</span></div>`)
		}
		identity = `<div class="identity">` + strings.Join(rows, "") + `</div>`
	}

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Prysm — Sign-in complete</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@300;400;500;600&family=IBM+Plex+Sans+Condensed:wght@300;400;500;600&family=IBM+Plex+Serif:ital,wght@1,300;1,400&display=swap" rel="stylesheet">
<style>
  :root {
    --paper:       #F8F6F1;
    --paper-2:     #F0EDE5;
    --surface:     #FFFFFF;
    --rule-faint:  #E0DBCE;
    --rule-mid:    #CBC4B3;
    --ink:         #0F0D08;
    --ink-2:       #2C2820;
    --ink-muted:   #6E6856;
    --ink-faint:   #A29B87;
    --copper:      #C8501D;
    --copper-soft: #E9A87F;
    --copper-tint: #F6E5D5;
    --ok:          #2F6B3E;
    --ok-tint:     #DFECE1;
    --warn:        #9E6A10;
    --warn-tint:   #F1E3C4;
    --danger:      #A63223;
    --danger-tint: #EED1C9;
    --sans: "IBM Plex Sans", ui-sans-serif, system-ui, sans-serif;
    --sans-cond: "IBM Plex Sans Condensed", "IBM Plex Sans", ui-sans-serif, sans-serif;
    --serif: "IBM Plex Serif", Georgia, serif;
    --mono: "IBM Plex Mono", ui-monospace, SFMono-Regular, monospace;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; }
  body {
    font-family: var(--sans);
    color: var(--ink);
    background:
      radial-gradient(900px 520px at 10% 110%, rgba(200, 80, 29, .08), transparent 60%),
      linear-gradient(180deg, var(--paper) 0%, var(--paper-2) 100%);
    min-height: 100vh;
    display: grid; place-items: center;
    padding: 32px 24px;
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
    letter-spacing: -0.005em;
  }
  body::before {
    content: ""; position: fixed; inset: 0; pointer-events: none;
    background-image: radial-gradient(var(--rule-faint) 1px, transparent 1.2px);
    background-size: 32px 32px;
    opacity: 0.3;
    mask-image: radial-gradient(ellipse 60% 65% at 50% 50%, black 0%, transparent 100%);
    -webkit-mask-image: radial-gradient(ellipse 60% 65% at 50% 50%, black 0%, transparent 100%);
  }

  .card {
    position: relative; z-index: 1;
    width: 100%; max-width: 560px;
    background: var(--surface);
    border: 1px solid var(--rule-faint);
    border-radius: 16px;
    padding: 44px 48px 36px;
    box-shadow:
      0 1px 2px rgba(26, 23, 18, .04),
      0 24px 60px rgba(26, 23, 18, .08);
  }

  .brand {
    display: flex; align-items: center; gap: 12px;
    margin-bottom: 32px;
  }
  .brand .lockup {
    width: 30px; height: 30px; border-radius: 6px;
    background: var(--ink); color: var(--copper-soft);
    display: grid; place-items: center;
    font-family: var(--sans-cond); font-weight: 600; font-size: 18px;
  }
  .brand .name {
    font-family: var(--sans-cond); font-size: 20px; font-weight: 500;
    letter-spacing: -0.015em; color: var(--ink);
  }
  .brand .name em {
    font-family: var(--serif); font-style: italic; font-weight: 300;
    color: var(--copper);
  }
  .brand .ref {
    margin-left: auto;
    font: 500 10px var(--mono); letter-spacing: 0.18em;
    color: var(--ink-faint);
  }

  .eyebrow {
    font: 500 11px var(--mono); letter-spacing: 0.22em; text-transform: uppercase;
    color: var(--copper);
    display: inline-flex; align-items: center; gap: 12px;
  }
  .eyebrow::before { content: ""; width: 22px; height: 1px; background: var(--copper); }

  h1.hero {
    font-family: var(--sans-cond);
    font-weight: 400;
    font-size: clamp(36px, 6vw, 48px);
    line-height: 1.02;
    letter-spacing: -0.03em;
    margin: 14px 0 12px;
    color: var(--ink);
  }
  h1.hero em {
    font-family: var(--serif); font-style: italic; font-weight: 300;
    color: var(--copper); letter-spacing: -0.018em;
  }
  .sub {
    font-size: 15.5px; color: var(--ink-2);
    line-height: 1.55; margin: 0 0 24px; max-width: 480px;
  }
  .sub code {
    font-family: var(--mono); font-size: 13.5px;
    padding: 2px 6px; background: var(--ink); color: var(--copper-soft);
    border-radius: 3px;
  }

  .banner {
    display: inline-flex; align-items: center; gap: 8px;
    font: 500 10.5px var(--mono); letter-spacing: 0.14em; text-transform: uppercase;
    padding: 5px 10px 5px 9px;
    border-radius: 999px;
    margin-bottom: 20px;
  }
  .banner .bd { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
  .banner.ok   { color: var(--ok);     background: var(--ok-tint); }
  .banner.warn { color: var(--warn);   background: var(--warn-tint); }
  .banner.err  { color: #fff;          background: var(--danger); }
  .banner.err .bd { background: #fff; }

  .identity {
    display: grid; gap: 10px;
    padding: 18px 20px;
    background: var(--paper);
    border: 1px solid var(--rule-faint);
    border-radius: 10px;
    margin: 4px 0 24px;
  }
  .identity .kv { display: flex; align-items: baseline; gap: 12px; font-size: 13.5px; }
  .identity .k {
    flex: 0 0 80px;
    font: 500 10.5px var(--mono); letter-spacing: 0.14em; text-transform: uppercase;
    color: var(--ink-faint);
  }
  .identity .v { color: var(--ink); }
  .identity .mono { font-family: var(--mono); font-size: 13px; }

  .terminal-cue {
    margin-top: 24px; padding-top: 22px;
    border-top: 1px solid var(--rule-faint);
    display: flex; align-items: center; gap: 14px;
    font-family: var(--mono); font-size: 12.5px;
    color: var(--ink-2);
  }
  .terminal-cue .cmd-block {
    display: inline-flex; align-items: center; gap: 10px;
    padding: 10px 14px;
    background: var(--ink); color: var(--paper);
    border-radius: 6px;
  }
  .terminal-cue .cmd-block .prompt { color: var(--copper-soft); }
  .terminal-cue .hint { color: var(--ink-muted); font-family: var(--sans); font-size: 13px; }

  .check {
    width: 36px; height: 36px; border-radius: 50%;
    background: var(--ok-tint); color: var(--ok);
    display: grid; place-items: center;
    margin-bottom: 18px;
    animation: pop .45s cubic-bezier(.17,.67,.44,1.41);
  }
  @keyframes pop {
    0%   { transform: scale(.3); opacity: 0; }
    100% { transform: scale(1);  opacity: 1; }
  }
  .check svg { width: 18px; height: 18px; }

  .x-mark {
    width: 36px; height: 36px; border-radius: 50%;
    background: var(--danger-tint); color: var(--danger);
    display: grid; place-items: center;
    margin-bottom: 18px;
  }
  .x-mark svg { width: 18px; height: 18px; }

  /* Stagger entrance */
  .card > * { opacity: 0; transform: translateY(6px); animation: rise .5s ease-out forwards; }
  .card > *:nth-child(1){ animation-delay: .00s; }
  .card > *:nth-child(2){ animation-delay: .08s; }
  .card > *:nth-child(3){ animation-delay: .14s; }
  .card > *:nth-child(4){ animation-delay: .2s; }
  .card > *:nth-child(5){ animation-delay: .26s; }
  .card > *:nth-child(6){ animation-delay: .32s; }
  .card > *:nth-child(7){ animation-delay: .38s; }
  @keyframes rise { to { opacity: 1; transform: translateY(0); } }

  ::selection { background: var(--copper); color: var(--paper); }
</style>
</head>
<body>
  <main class="card">
    <div class="brand">
      <div class="lockup">P</div>
      <span class="name">Prysm<em>/</em></span>
      <span class="ref">CLI · 127.0.0.1</span>
    </div>

    <div class="eyebrow">Sign-in complete</div>

    ` + statusIcon(s.Failed) + `

    <h1 class="hero">` + heroTitle + `</h1>
    <p class="sub">` + heroSub + `</p>

    ` + statusBanner + `

    ` + identity + `

    <div class="terminal-cue">
      <div class="cmd-block">
        <span class="prompt">$</span>
        <span>` + footerCmd + `</span>
      </div>
      <span class="hint">You can close this window.</span>
    </div>
  </main>
  <script>
    // Try to auto-close after a short delay. Most browsers block window.close
    // for windows they didn't open programmatically, so this is best-effort.
    setTimeout(function(){ try { window.close(); } catch (e) {} }, 4000);
  </script>
</body>
</html>`
}

func statusIcon(failed bool) string {
	if failed {
		return `<div class="x-mark"><svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg></div>`
	}
	return `<div class="check"><svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8.5l3 3 6-7"/></svg></div>`
}
