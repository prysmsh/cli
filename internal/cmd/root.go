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

func initApp(cmd *cobra.Command) error {
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
		// attach session token to API client if available
		if session, err := app.Sessions.Load(); err == nil && session != nil {
			if session.APIBaseURL != "" && !strings.EqualFold(session.APIBaseURL, app.Config.APIBaseURL) {
				app.Config.APIBaseURL = session.APIBaseURL
				app.API = api.NewClient(app.Config.APIBaseURL,
					api.WithTimeout(30*time.Second),
					api.WithUserAgent("prysm-cli/0.2"),
					api.WithDebug(app.Debug),
					api.WithHostOverride(app.HostOverride),
					api.WithInsecureSkipVerify(app.InsecureTLS),
					api.WithDialAddress(app.DialOverride),
				)
			}
			app.API.SetToken(session.Token)
		}
	}

	return nil
}

func printDebug(format string, args ...interface{}) {
	if app != nil && app.Debug {
		msg := fmt.Sprintf(format, args...)
		color.New(color.FgHiBlack).Fprintln(os.Stderr, "[debug]", msg)
	}
}
