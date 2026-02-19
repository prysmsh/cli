package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/config"
	"github.com/warp-run/prysm-cli/internal/session"
)

// AppContext holds references to the CLI app state needed by host services.
type AppContext struct {
	Config   *config.Config
	Sessions *session.Store
	API      *api.Client
	Format   string
	Debug    bool
}

// BuiltinHostServices implements HostServices backed by the CLI's own App state.
// Used by builtin plugins to call host services in-process without gRPC overhead.
type BuiltinHostServices struct {
	app *AppContext
}

// NewBuiltinHostServices creates a HostServices backed by the given app context.
func NewBuiltinHostServices(app *AppContext) *BuiltinHostServices {
	return &BuiltinHostServices{app: app}
}

// GetAuthContext returns the current authenticated user's context.
func (h *BuiltinHostServices) GetAuthContext(ctx context.Context) (*AuthContext, error) {
	sess, err := h.app.Sessions.Load()
	if err != nil || sess == nil {
		return nil, fmt.Errorf("not logged in — run `prysm login` first")
	}
	return &AuthContext{
		Token:      sess.Token,
		OrgID:      uint64(sess.Organization.ID),
		OrgName:    sess.Organization.Name,
		UserID:     uint64(sess.User.ID),
		UserEmail:  sess.User.Email,
		APIBaseURL: h.app.Config.APIBaseURL,
	}, nil
}

// APIRequest proxies an HTTP request through the host's authenticated API client.
func (h *BuiltinHostServices) APIRequest(ctx context.Context, method, endpoint string, body []byte) (int, []byte, error) {
	// Pass body as json.RawMessage so api.Client.Do encodes it as-is
	// (not as a Go struct). A nil interface{} signals no body.
	var payload interface{}
	if len(body) > 0 {
		payload = json.RawMessage(body)
	}

	var result json.RawMessage
	resp, err := h.app.API.Do(ctx, method, endpoint, payload, &result)
	if err != nil {
		// Even on API errors, we may have status code info
		if apiErr, ok := err.(*api.APIError); ok {
			return apiErr.StatusCode, []byte(apiErr.Message), nil
		}
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := json.Marshal(result)
	return resp.StatusCode, respBody, nil
}

// GetConfig returns the current CLI configuration.
func (h *BuiltinHostServices) GetConfig(ctx context.Context) (*HostConfig, error) {
	return &HostConfig{
		APIBaseURL:   h.app.Config.APIBaseURL,
		DERPURL:      h.app.Config.DERPServerURL,
		HomeDir:      h.app.Config.HomeDir,
		OutputFormat: h.app.Format,
	}, nil
}

// Log writes a message to the terminal with the appropriate formatting.
func (h *BuiltinHostServices) Log(ctx context.Context, level LogLevel, message string) error {
	switch level {
	case LogLevelSuccess:
		color.New(color.FgGreen).Fprintln(os.Stderr, message)
	case LogLevelWarning:
		color.New(color.FgYellow).Fprintln(os.Stderr, message)
	case LogLevelError:
		color.New(color.FgRed).Fprintln(os.Stderr, message)
	case LogLevelDebug:
		if h.app.Debug {
			color.New(color.FgHiBlack).Fprintln(os.Stderr, "[debug]", message)
		}
	case LogLevelPlain:
		fmt.Fprintln(os.Stdout, message)
	default: // Info
		color.New(color.FgCyan).Fprintln(os.Stderr, message)
	}
	return nil
}

// PromptInput reads a line from the terminal. If isSecret is true, input is masked.
func (h *BuiltinHostServices) PromptInput(ctx context.Context, label string, isSecret bool) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	if isSecret {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr) // newline after hidden input
		return string(b), err
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	return "", fmt.Errorf("no input")
}

// PromptConfirm asks a yes/no question and returns the answer.
func (h *BuiltinHostServices) PromptConfirm(ctx context.Context, label string) (bool, error) {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", label)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes", nil
	}
	return false, nil
}

// doAPIRaw is a helper to make raw HTTP requests through the API client.
func (h *BuiltinHostServices) doAPIRaw(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	var result json.RawMessage
	resp, err := h.app.API.Do(ctx, method, endpoint, body, &result)
	return resp, err
}
