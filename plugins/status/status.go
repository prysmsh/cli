// Package status implements the builtin "status" plugin for CLI and API health.
package status

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prysmsh/cli/internal/plugin"
)

// StatusOutput is the structured API for the status command (e.g. -o json).
type StatusOutput struct {
	LoggedIn   bool   `json:"logged_in"`
	UserEmail  string `json:"user_email,omitempty"`
	UserName   string `json:"user_name,omitempty"`
	OrgID      uint64 `json:"organization_id,omitempty"`
	OrgName    string `json:"organization_name,omitempty"`
	APIBaseURL string `json:"api_base_url,omitempty"`
	APIReachable bool `json:"api_reachable,omitempty"`
	DERPURL    string `json:"derp_url,omitempty"`
	Error      string `json:"error,omitempty"`
}

// StatusPlugin is a builtin plugin that shows login state and API reachability.
type StatusPlugin struct {
	host plugin.HostServices
}

// New creates a new status plugin. Pass nil for host if registering eagerly; call SetHost before Execute.
func New(host plugin.HostServices) *StatusPlugin {
	return &StatusPlugin{host: host}
}

// SetHost sets the host services used by this plugin.
func (p *StatusPlugin) SetHost(host plugin.HostServices) {
	p.host = host
}

// Manifest returns the plugin's metadata and command tree.
func (p *StatusPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "status",
		Version:     "0.1.0",
		Description: "Show login state and API reachability",
		Commands: []plugin.CommandSpec{
			{
				Name:  "status",
				Short: "Show login state, current org, and API status",
				Long:  "Prints whether you are logged in, current organization, and whether the API is reachable.",
			},
		},
	}
}

// Execute runs the status command. When req.OutputFormat is "json", writes StatusOutput to Stdout.
func (p *StatusPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	auth, err := p.host.GetAuthContext(ctx)
	if err != nil {
		return p.outputNotLoggedIn(ctx, req.OutputFormat, err.Error())
	}

	statusCode, body, apiErr := p.host.APIRequest(ctx, "GET", "/profile", nil)
	var profile struct {
		User struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &profile)
	}

	if apiErr != nil {
		return p.outputAPIError(ctx, req.OutputFormat, auth, apiErr.Error())
	}
	if statusCode != 200 {
		return p.outputAPIError(ctx, req.OutputFormat, auth, fmt.Sprintf("API returned %d", statusCode))
	}

	cfg, _ := p.host.GetConfig(ctx)
	out := StatusOutput{
		LoggedIn:      true,
		UserEmail:     auth.UserEmail,
		UserName:      profile.User.Name,
		OrgID:         auth.OrgID,
		OrgName:       auth.OrgName,
		APIBaseURL:    auth.APIBaseURL,
		APIReachable:  true,
	}
	if cfg != nil && cfg.DERPURL != "" {
		out.DERPURL = cfg.DERPURL
	}

	if req.OutputFormat == "json" {
		b, _ := json.MarshalIndent(out, "", "  ")
		return plugin.ExecuteResponse{ExitCode: 0, Stdout: string(b) + "\n"}
	}

	// Human-readable output
	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Logged in")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	userLine := auth.UserEmail
	if profile.User.Name != "" {
		userLine = profile.User.Name + " <" + auth.UserEmail + ">"
	}
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  User:   %s", userLine))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Org:    %s (id: %d)", auth.OrgName, auth.OrgID))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  API:    %s", auth.APIBaseURL))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  API:    reachable")
	if out.DERPURL != "" {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  DERP:   %s", out.DERPURL))
	}
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	return plugin.ExecuteResponse{ExitCode: 0}
}

func (p *StatusPlugin) outputNotLoggedIn(ctx context.Context, format, errMsg string) plugin.ExecuteResponse {
	if format == "json" {
		cfg, _ := p.host.GetConfig(ctx)
		out := StatusOutput{LoggedIn: false, Error: errMsg}
		if cfg != nil {
			out.APIBaseURL = cfg.APIBaseURL
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return plugin.ExecuteResponse{ExitCode: 1, Error: errMsg, Stdout: string(b) + "\n"}
	}
	_ = p.host.Log(ctx, plugin.LogLevelWarning, "Not logged in — run `prysm login` first.")
	cfg, _ := p.host.GetConfig(ctx)
	if cfg != nil && cfg.APIBaseURL != "" {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "API URL: "+cfg.APIBaseURL)
	}
	return plugin.ExecuteResponse{ExitCode: 1, Error: errMsg}
}

func (p *StatusPlugin) outputAPIError(ctx context.Context, format string, auth *plugin.AuthContext, errMsg string) plugin.ExecuteResponse {
	if format == "json" {
		out := StatusOutput{
			LoggedIn:     true,
			UserEmail:    auth.UserEmail,
			OrgID:        auth.OrgID,
			OrgName:      auth.OrgName,
			APIBaseURL:   auth.APIBaseURL,
			APIReachable: false,
			Error:        errMsg,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return plugin.ExecuteResponse{ExitCode: 1, Error: errMsg, Stdout: string(b) + "\n"}
	}
	_ = p.host.Log(ctx, plugin.LogLevelError, "API request failed: "+errMsg)
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("Logged in as %s (org: %s)", auth.UserEmail, auth.OrgName))
	return plugin.ExecuteResponse{ExitCode: 1, Error: errMsg}
}
