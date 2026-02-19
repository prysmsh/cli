// Package plugin implements a Hashicorp-style plugin system for the Prysm CLI.
// Plugins can be builtin (in-process) or external (gRPC over stdio).
package plugin

import "context"

// Plugin is the interface that all plugins (builtin and external) implement.
type Plugin interface {
	// Manifest returns the plugin's metadata and command tree.
	Manifest() Manifest
	// Execute runs a plugin command with the given request.
	Execute(ctx context.Context, req ExecuteRequest) ExecuteResponse
}

// Manifest describes a plugin's metadata and command structure.
type Manifest struct {
	Name        string
	Version     string
	Description string
	Commands    []CommandSpec
}

// CommandSpec describes a command or subcommand tree exposed by a plugin.
type CommandSpec struct {
	Name               string
	Short              string
	Long               string
	Subcommands        []CommandSpec
	DisableFlagParsing bool // pass all args (including --flags) raw to Execute
}

// ExecuteRequest contains the arguments for a plugin command invocation.
type ExecuteRequest struct {
	Args         []string
	Env          map[string]string
	WorkingDir   string
	OutputFormat string
	Debug        bool
}

// ExecuteResponse contains the result of a plugin command invocation.
type ExecuteResponse struct {
	ExitCode int
	Error    string
	Stdout   string
}

// HostServices is the interface that the CLI host provides to plugins.
// Builtin plugins call these methods directly; external plugins call them via gRPC.
type HostServices interface {
	GetAuthContext(ctx context.Context) (*AuthContext, error)
	APIRequest(ctx context.Context, method, endpoint string, body []byte) (int, []byte, error)
	GetConfig(ctx context.Context) (*HostConfig, error)
	Log(ctx context.Context, level LogLevel, message string) error
	PromptInput(ctx context.Context, label string, isSecret bool) (string, error)
	PromptConfirm(ctx context.Context, label string) (bool, error)
}

// AuthContext contains the authenticated user's context from the CLI session.
type AuthContext struct {
	Token      string
	OrgID      uint64
	OrgName    string
	UserID     uint64
	UserEmail  string
	APIBaseURL string
}

// HostConfig contains the CLI's current configuration.
type HostConfig struct {
	APIBaseURL   string
	DERPURL      string
	HomeDir      string
	OutputFormat string
}

// LogLevel represents output severity for plugin log messages.
type LogLevel int

const (
	LogLevelInfo    LogLevel = 1
	LogLevelSuccess LogLevel = 2
	LogLevelWarning LogLevel = 3
	LogLevelError   LogLevel = 4
	LogLevelDebug   LogLevel = 5
	LogLevelPlain   LogLevel = 6
)
