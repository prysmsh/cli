package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/warp-run/prysm-cli/internal/api"
	"github.com/warp-run/prysm-cli/internal/config"
	"github.com/warp-run/prysm-cli/internal/session"
)

var (
	rootCmd = &cobra.Command{
		Use:           "prysm",
		Short:         "Prysm zero-trust infrastructure access CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initApp(cmd)
		},
	}

	cfgFile        string
	activeProfile  string
	overrideAPI    string
	overrideComp   string
	overrideDERP   string
	overrideFormat string
	overrideHost   string
	overrideDial   string
	debugEnabled   bool
	insecureTLS    bool

	appOnce sync.Once
	app     *App
)

var version = "dev"

// App carries global CLI state shared across commands.
type App struct {
	Config       *config.Config
	Sessions     *session.Store
	API          *api.Client
	OutputFormat string
	Debug        bool
	HostOverride string
	InsecureTLS  bool
	DialOverride string
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// MustApp returns the initialized application context.
func MustApp() *App {
	if app == nil {
		panic("cli not initialized")
	}
	return app
}

func init() {
	cobra.OnInitialize(func() {
		color.NoColor = false
	})

	rootCmd.Version = version
	rootCmd.SetVersionTemplate("{{.Name}} version {{.Version}}\n")

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $PRYSM_HOME/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&activeProfile, "profile", "default", "configuration profile")
	rootCmd.PersistentFlags().StringVar(&overrideAPI, "api-url", "", "override API base URL")
	rootCmd.PersistentFlags().StringVar(&overrideHost, "api-host", "", "override Host header when connecting to the API")
	rootCmd.PersistentFlags().StringVar(&overrideDial, "api-connect", "", "override network address when connecting to the API (e.g. 127.0.0.1:8444)")
	rootCmd.PersistentFlags().StringVar(&overrideComp, "compliance-url", "", "override compliance API URL")
	rootCmd.PersistentFlags().StringVar(&overrideDERP, "derp-url", "", "override DERP relay URL")
	rootCmd.PersistentFlags().StringVar(&overrideFormat, "format", "", "set default output format")
	rootCmd.PersistentFlags().BoolVar(&debugEnabled, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&insecureTLS, "insecure", false, "skip TLS certificate verification when connecting to the API")

	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))

	rootCmd.AddCommand(
		newCompletionCommand(),
		newLoginCommand(),
		newLogoutCommand(),
		newSessionCommand(),
		newConnectCommand(),
		newCredentialCommand(),
		newMeshCommand(),
		newTunnelCommand(),
		newAuditCommand(),
		newAgentCommand(),
		newClustersCommand(),
		newSecurityCommand(),
		newHoneypotsCommand(),
	)
}

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion code for bash or zsh.

To load in current session:
  . <(prysm completion bash)   # bash
  . <(prysm completion zsh)    # zsh

To enable permanently, add to ~/.bashrc or ~/.zshrc:
  if command -v prysm &>/dev/null; then eval "$(prysm completion bash)" fi
  if command -v prysm &>/dev/null; then eval "$(prysm completion zsh)" fi`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh"},
		Args:                  cobra.ExactValidArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
}

// isCompletionCommand returns true if the user is running a shell completion
// subcommand. We skip app init (config, session) for completion since it's not needed.
func isCompletionCommand() bool {
	for _, arg := range os.Args[1:] {
		if arg == "completion" {
			return true
		}
		if len(arg) > 0 && arg[0] != '-' {
			return false // first non-flag is the command name; not completion
		}
	}
	return false
}

func initApp(cmd *cobra.Command) error {
	if isCompletionCommand() {
		return nil
	}
	var initErr error
	appOnce.Do(func() {
		cfgPath := cfgFile
		if cfgPath == "" {
			home, err := config.DefaultHomeDir()
			if err != nil {
				initErr = fmt.Errorf("determine config directory: %w", err)
				return
			}
			cfgPath = filepath.Join(home, "config.yaml")
		}

		cfg, err := config.Load(cfgPath, activeProfile)
		if err != nil {
			initErr = err
			return
		}

		if overrideAPI != "" {
			cfg.APIBaseURL = strings.TrimRight(overrideAPI, "/")
		}
		if overrideComp != "" {
			cfg.ComplianceURL = strings.TrimRight(overrideComp, "/")
		}
		if overrideDERP != "" {
			cfg.DERPServerURL = strings.TrimRight(overrideDERP, "/")
		}
		if overrideFormat != "" {
			cfg.OutputFormat = overrideFormat
		}
		hostOverride := strings.TrimSpace(overrideHost)
		dialOverride := strings.TrimSpace(overrideDial)
		if cfg.HomeDir == "" {
			cfg.HomeDir, _ = config.DefaultHomeDir()
		}

		if err := os.MkdirAll(cfg.HomeDir, 0o700); err != nil {
			initErr = fmt.Errorf("ensure prysm home: %w", err)
			return
		}

		sessionStore := session.NewStore(filepath.Join(cfg.HomeDir, "session.json"))
		apiClient := api.NewClient(cfg.APIBaseURL,
			api.WithTimeout(30*time.Second),
			api.WithUserAgent("prysm-cli/0.2"),
			api.WithDebug(debugEnabled),
			api.WithHostOverride(hostOverride),
			api.WithInsecureSkipVerify(insecureTLS),
			api.WithDialAddress(dialOverride),
		)

		app = &App{
			Config:       cfg,
			Sessions:     sessionStore,
			API:          apiClient,
			OutputFormat: cfg.OutputFormat,
			Debug:        debugEnabled,
			HostOverride: hostOverride,
			InsecureTLS:  insecureTLS,
			DialOverride: dialOverride,
		}
	})

	if initErr != nil {
		return initErr
	}

	if app == nil {
		return fmt.Errorf("failed to initialize cli")
	}

	if cmd.Name() != "login" {
		sess, err := app.Sessions.Load()
		if err == nil && sess != nil {
			if sess.APIBaseURL != "" && !strings.EqualFold(sess.APIBaseURL, app.Config.APIBaseURL) {
				app.Config.APIBaseURL = sess.APIBaseURL
				app.API = api.NewClient(app.Config.APIBaseURL,
					api.WithTimeout(30*time.Second),
					api.WithUserAgent("prysm-cli/0.2"),
					api.WithDebug(app.Debug),
					api.WithHostOverride(app.HostOverride),
					api.WithInsecureSkipVerify(app.InsecureTLS),
					api.WithDialAddress(app.DialOverride),
				)
			}
			app.API.SetToken(sess.Token)
		}
	}

	return nil
}

func printDebug(format string, args ...interface{}) {
	debug := (app != nil && app.Debug) || os.Getenv("PRYSM_DEBUG") == "1" || os.Getenv("PRYSM_DEBUG") == "true"
	if debug {
		msg := fmt.Sprintf(format, args...)
		color.New(color.FgHiBlack).Fprintln(os.Stderr, "[debug]", msg)
	}
}
