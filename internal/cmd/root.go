package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/config"
	"github.com/prysmsh/cli/internal/plugin"
	"github.com/prysmsh/cli/internal/session"
	"github.com/prysmsh/cli/internal/style"
	exitplugin "github.com/prysmsh/cli/plugins/exit"
	"github.com/prysmsh/cli/plugins/onboard"
	"github.com/prysmsh/cli/plugins/status"
	vaultplugin "github.com/prysmsh/cli/plugins/vault"
)

var (
	rootCmd = &cobra.Command{
		Use:              "prysm",
		Short:            "Prysm zero-trust infrastructure access CLI",
		SilenceUsage:     true,
		SilenceErrors:    true,
		TraverseChildren: true,
	}

	cfgFile        string
	activeProfile  string
	overrideAPI    string
	overrideComp   string
	overrideDERP   string
	overrideFormat string
	overrideHost   string
	overrideDial   string
	overrideToken  string
	debugEnabled   bool
	insecureTLS    bool

	appOnce       sync.Once
	app           *App
	pluginMgr     *plugin.Manager
	onboardPlugin *onboard.OnboardPlugin
	statusPlugin  *status.StatusPlugin
	exitPlugin    *exitplugin.ExitPlugin
	vaultPlugin   *vaultplugin.VaultPlugin
)

var version = "dev"

// commandGroup assigns root subcommands to sections on the default menu (no -h).
// Commands not in the map are listed under "Other".
var commandGroup = map[string]string{
	"login":      "Get started",
	"install":    "Get started",
	"connect":    "Get started",
	"hosts":      "Infrastructure",
	"clusters":   "Infrastructure",
	"ssh":        "Infrastructure",
	"tunnel":     "Infrastructure",
	"mesh":       "Infrastructure",
	"credential": "Infrastructure",
	"docker":     "Infrastructure",
	"security":   "Security",
	"vault":      "Security",
	"audit":      "Security",
	"honeypots":  "Security",
	"sessions":   "Security",
	"session":    "Account",
	"request":    "Account",
	"logout":     "Account",
	"team":       "Account",
	"profile":    "Account",
	"ai":         "Tools",
	"status":     "Tools",
	"diagnose":   "Tools",
	"plugin":     "Tools",
	"update":     "Tools",
	"completion": "Tools",
}

// menuGroupOrder is the display order of groups on the default menu.
var menuGroupOrder = []string{
	"Get started",
	"Infrastructure",
	"Security",
	"Account",
	"Tools",
	"Other",
}

// menuOrder controls the display order of commands within each group.
// Lower values appear first. Commands not listed default to 50.
var menuOrder = map[string]int{
	"login": 1, "install": 2, "connect": 3,
	"clusters": 1, "hosts": 2, "ssh": 3, "tunnel": 4, "mesh": 5, "credential": 6, "docker": 7,
	"security": 1, "vault": 2, "audit": 3, "sessions": 4, "honeypots": 5,
	"session": 1, "request": 2, "logout": 3, "team": 4, "profile": 5,
	"ai": 1, "diagnose": 2, "status": 3, "update": 4, "plugin": 5, "completion": 6,
}

// menuShortDesc overrides command.Short for the default help menu to keep it tight.
var menuShortDesc = map[string]string{
	"login":      "Sign in to Prysm",
	"connect":    "Get kubeconfig for a cluster",
	"clusters":   "List, onboard, and manage clusters",
	"ssh":        "Open policy-checked SSH access",
	"tunnel":     "Create secure TCP tunnels",
	"mesh":       "Join the DERP mesh network",
	"credential": "Emit credentials for kubectl",
	"docker":     "Configure Docker contexts",
	"security":   "Security events and compliance",
	"vault":      "Embedded secrets engine",
	"audit":      "Audit logs and compliance reports",
	"sessions":   "List and replay access sessions",
	"honeypots":  "Manage honeypot tokens",
	"session":    "Show current session",
	"request":    "Create and review access requests",
	"logout":     "Sign out and purge credentials",
	"team":       "Manage team members",
	"profile":    "View and update your profile",
	"ai":         "AI assistant, agents, and embeddings",
	"diagnose":   "Run network and access diagnostics",
	"status":     "System health check",
	"hosts":      "Manage standalone hosts",
	"install":    "Install agent on a remote host via SSH",
	"update":     "Update the CLI",
	"plugin":     "Manage CLI plugins",
	"completion": "Generate shell completions",
}

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
	defer func() {
		if pluginMgr != nil {
			pluginMgr.Shutdown()
		}
	}()
	err := rootCmd.Execute()
	if err != nil {
		return friendlyError(err)
	}
	return nil
}

// MustApp returns the initialized application context.
func MustApp() *App {
	if app == nil {
		panic("cli not initialized")
	}
	return app
}

func init() {
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			unknown := args[0]
			if suggestion := suggestCommand(unknown, "prysm"); suggestion != "" {
				return fmt.Errorf("unknown command %q — did you mean %q?\n\n  Run `prysm --help` to see available commands", unknown, suggestion)
			}
			return fmt.Errorf("unknown command %q\n\n  Run `prysm --help` to see available commands", unknown)
		}
		cmd.Help()
		return nil
	}

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		return initApp(cmd)
	}

	rootCmd.Version = version
	rootCmd.SetVersionTemplate(style.RenderVersion(rootCmd.Name(), version) + "\n")

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $PRYSM_HOME/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&activeProfile, "profile", "default", "configuration profile")
	rootCmd.PersistentFlags().StringVar(&overrideAPI, "api-url", "", "override API base URL")
	rootCmd.PersistentFlags().StringVar(&overrideHost, "api-host", "", "override Host header when connecting to the API")
	rootCmd.PersistentFlags().StringVar(&overrideDial, "api-connect", "", "override network address when connecting to the API (e.g. 127.0.0.1:8444)")
	rootCmd.PersistentFlags().StringVar(&overrideComp, "compliance-url", "", "override compliance API URL")
	rootCmd.PersistentFlags().StringVar(&overrideDERP, "derp-url", "", "override DERP relay URL")
	rootCmd.PersistentFlags().StringVar(&overrideFormat, "format", "", "set default output format")
	rootCmd.PersistentFlags().StringVar(&overrideToken, "token", "", "authentication token (overrides session; can also use PRYSM_TOKEN env var)")
	rootCmd.PersistentFlags().BoolVar(&debugEnabled, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&insecureTLS, "insecure", false, "skip TLS certificate verification when connecting to the API")

	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))

	clustersCmd := newClustersCommand()
	meshCmd := newMeshCommand()

	rootCmd.AddCommand(
		newCompletionCommand(),
		newLoginCommand(),
		newLogoutCommand(),
		newSessionCommand(),
		newRequestCommand(),
		newSessionsCommand(),
		newConnectCommand(),
		newSSHCommand(),
		newCredentialCommand(),
		meshCmd,
		newTunnelCommand(),
		newDiagnoseCommand(),
		newAuditCommand(),
		newAICommand(),
		clustersCmd,
		newSecurityCommand(),
		newHoneypotsCommand(),
		newPluginCommand(),
		newDockerCommand(),
		newHostsCommand(),
		newInstallCommand(),
		newUpdateCommand(),
		newTeamCommand(),
		newProfileCommand(),
		newLogFilterCommand(),
	)

	// Register exit plugin commands under "mesh exit" (use, off, status).
	exitPlugin = exitplugin.New(nil)
	var meshExitCmd *cobra.Command
	for _, sub := range meshCmd.Commands() {
		if sub.Name() == "exit" {
			meshExitCmd = sub
			break
		}
	}
	if meshExitCmd != nil {
		for _, spec := range exitPlugin.Manifest().Commands {
			meshExitCmd.AddCommand(plugin.BuildCobraCommand(spec, exitPlugin, pluginRequestOptions()))
		}
	}

	// Register builtin plugin commands eagerly so Cobra can route them.
	// Host services are set later in initPluginManager (PersistentPreRunE).
	onboardPlugin = onboard.New(nil)
	manifest := onboardPlugin.Manifest()

	// Add onboard subcommands directly under "clusters" (primary path: prysm clusters onboard kube).
	for _, spec := range manifest.Commands {
		clustersCmd.AddCommand(plugin.BuildCobraCommand(spec, onboardPlugin, pluginRequestOptions()))
	}

	// Keep top-level "prysm onboard" as a hidden alias for backwards compatibility.
	for _, spec := range manifest.Commands {
		aliasCmd := plugin.BuildCobraCommand(spec, onboardPlugin, pluginRequestOptions())
		aliasCmd.Hidden = true
		rootCmd.AddCommand(aliasCmd)
	}
	statusPlugin = status.New(nil)
	for _, spec := range statusPlugin.Manifest().Commands {
		rootCmd.AddCommand(plugin.BuildCobraCommand(spec, statusPlugin, pluginRequestOptions()))
	}
	vaultPlugin = vaultplugin.New(nil)
	for _, spec := range vaultPlugin.Manifest().Commands {
		rootCmd.AddCommand(plugin.BuildCobraCommand(spec, vaultPlugin, pluginRequestOptions()))
	}

	rootCmd.SetHelpFunc(styledRootHelpFunc)

	// Walk the command tree: add "did you mean?" for unknown subcommands,
	// and wrap arg validators to show help + friendly errors.
	addSuggestionHandlers(rootCmd)
	patchArgsValidators(rootCmd)
}

// styledRootHelpFunc prints styled help for the root command and any parent
// command that has subcommands. Leaf commands fall back to cobra's default.
func styledRootHelpFunc(cmd *cobra.Command, args []string) {
	if cmd != rootCmd && !cmd.HasSubCommands() {
		// Leaf command — use cobra's default usage output.
		cmd.Usage()
		return
	}

	out := cmd.OutOrStdout()
	if out == nil {
		out = os.Stdout
	}

	// For non-root parent commands, render a styled subcommand listing.
	if cmd != rootCmd {
		styledSubcommandHelp(cmd, out)
		return
	}

	showFullHelp := false
	for _, a := range os.Args[1:] {
		if a == "-h" || a == "--help" {
			showFullHelp = true
			break
		}
	}

	commands := cmd.Commands()

	// Compute column width from visible command names.
	var maxNameLen int
	for _, c := range commands {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		if n := len(c.Name()); n > maxNameLen {
			maxNameLen = n
		}
	}
	if maxNameLen < 12 {
		maxNameLen = 12
	}

	if showFullHelp {
		// Full help (-h/--help): plain title, usage, grouped commands, then flags.
		fmt.Fprintln(out, style.Title.Render(cmd.Short))
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Bold.Render("Usage:"))
		fmt.Fprintf(out, "  %s [command] [flags]\n", cmd.Name())
		fmt.Fprintln(out)

		byGroup, groupOrder := bucketCommands(commands)
		for _, groupTitle := range groupOrder {
			groupCmds := byGroup[groupTitle]
			if len(groupCmds) == 0 {
				continue
			}
			fmt.Fprintln(out, style.Bold.Render(groupTitle+":"))
			for _, c := range groupCmds {
				fmt.Fprintf(out, "  %-*s  %s\n", maxNameLen, c.Name(), c.Short)
			}
			fmt.Fprintln(out)
		}

		// Flags
		fmt.Fprintln(out, style.Bold.Render("Flags:"))
		cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
			usage := f.Usage
			if f.DefValue != "" && f.DefValue != "false" {
				usage += fmt.Sprintf(" (default %q)", f.DefValue)
			}
			sh := "--" + f.Name
			if f.Shorthand != "" && f.Shorthand != f.Name {
				sh = fmt.Sprintf("-%s, --%s", f.Shorthand, f.Name)
			}
			fmt.Fprintf(out, "  %-24s  %s\n", sh, usage)
		})
		fmt.Fprintln(out)

		fmt.Fprintln(out, style.MutedStyle.Render(`Use "prysm [command] --help" for more information about a command.`))
		return
	}

	// Default menu (no -h): welcome header, grouped commands, footer hints.
	header := style.Title.Render("Prysm") + "\n" +
		style.Tagline.Render("Zero-trust infrastructure access") + "  " +
		style.MutedStyle.Render("v"+version)
	fmt.Fprintln(out, style.WelcomeBox.Render(header))

	byGroup, _ := bucketCommands(commands)

	for _, groupTitle := range menuGroupOrder {
		groupCmds := byGroup[groupTitle]
		if len(groupCmds) == 0 {
			continue
		}
		fmt.Fprintf(out, "  %s\n", style.SectionHeader.Render(strings.ToUpper(groupTitle)))
		for _, c := range groupCmds {
			desc := menuShortDesc[c.Name()]
			if desc == "" {
				desc = c.Short
			}
			nameStyled := style.Info.Render(c.Name())
			pad := maxNameLen - lipgloss.Width(nameStyled)
			if pad < 0 {
				pad = 0
			}
			fmt.Fprintf(out, "    %s%s  %s\n", nameStyled, strings.Repeat(" ", pad), style.MutedStyle.Render(desc))
		}
		fmt.Fprintln(out)
	}

	// Footer hints
	hintCol := 24
	fmt.Fprintf(out, "  %-*s %s\n", hintCol, style.HintKey.Render("prysm login"), style.MutedStyle.Render("Sign in to get started"))
	fmt.Fprintf(out, "  %-*s %s\n", hintCol, style.HintKey.Render("prysm ssh onboard"), style.MutedStyle.Render("Onboard an SSH host"))
	fmt.Fprintf(out, "  %-*s %s\n", hintCol, style.HintKey.Render("prysm <cmd> --help"), style.MutedStyle.Render("Details for any command"))
	fmt.Fprintln(out)
}

// styledSubcommandHelp renders styled help for a non-root parent command.
func styledSubcommandHelp(cmd *cobra.Command, out io.Writer) {
	// Build the full command path (e.g. "prysm connect")
	cmdPath := cmd.CommandPath()

	fmt.Fprintln(out, style.Title.Render(cmd.Short))
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Bold.Render("Usage:"))
	fmt.Fprintf(out, "  %s [command] [flags]\n", cmdPath)
	fmt.Fprintln(out)

	commands := cmd.Commands()

	var maxNameLen int
	for _, c := range commands {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		if n := len(c.Name()); n > maxNameLen {
			maxNameLen = n
		}
	}
	if maxNameLen < 12 {
		maxNameLen = 12
	}

	fmt.Fprintln(out, style.Bold.Render("Commands:"))
	for _, c := range commands {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		nameStyled := style.Info.Render(c.Name())
		pad := maxNameLen - lipgloss.Width(nameStyled)
		if pad < 0 {
			pad = 0
		}
		fmt.Fprintf(out, "    %s%s  %s\n", nameStyled, strings.Repeat(" ", pad), style.MutedStyle.Render(c.Short))
	}
	fmt.Fprintln(out)

	// Show local flags if any
	localFlags := cmd.LocalNonPersistentFlags()
	if localFlags.HasFlags() {
		fmt.Fprintln(out, style.Bold.Render("Flags:"))
		localFlags.VisitAll(func(f *pflag.Flag) {
			usage := f.Usage
			if f.DefValue != "" && f.DefValue != "false" {
				usage += fmt.Sprintf(" (default %q)", f.DefValue)
			}
			sh := "--" + f.Name
			if f.Shorthand != "" && f.Shorthand != f.Name {
				sh = fmt.Sprintf("-%s, --%s", f.Shorthand, f.Name)
			}
			fmt.Fprintf(out, "  %-24s  %s\n", sh, usage)
		})
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, style.MutedStyle.Render(fmt.Sprintf(`Use "%s [command] --help" for more information about a command.`, cmdPath)))
}

// bucketCommands groups visible commands by their commandGroup assignment,
// sorted within each group by menuOrder (then alphabetically).
func bucketCommands(commands []*cobra.Command) (map[string][]*cobra.Command, []string) {
	byGroup := make(map[string][]*cobra.Command)
	for _, c := range commands {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		group := commandGroup[c.Name()]
		if group == "" {
			group = "Other"
		}
		byGroup[group] = append(byGroup[group], c)
	}
	for g, cmds := range byGroup {
		sort.Slice(cmds, func(i, j int) bool {
			oi, oj := menuOrder[cmds[i].Name()], menuOrder[cmds[j].Name()]
			if oi == 0 {
				oi = 50
			}
			if oj == 0 {
				oj = 50
			}
			if oi != oj {
				return oi < oj
			}
			return cmds[i].Name() < cmds[j].Name()
		})
		byGroup[g] = cmds
	}
	return byGroup, menuGroupOrder
}

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion code for bash, zsh, or fish.

When called without arguments, detects your current shell automatically.

To load in current session:
  . <(prysm completion bash)   # bash
  . <(prysm completion zsh)    # zsh
  prysm completion fish | source  # fish

To enable permanently, add to ~/.bashrc, ~/.zshrc, or fish config:
  if command -v prysm &>/dev/null; then eval "$(prysm completion bash)" fi
  if command -v prysm &>/dev/null; then eval "$(prysm completion zsh)" fi
  prysm completion fish > ~/.config/fish/completions/prysm.fish`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish"},
		Args:                  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := ""
			if len(args) > 0 {
				shell = args[0]
			} else {
				// Auto-detect from $SHELL
				shellPath := os.Getenv("SHELL")
				if strings.HasSuffix(shellPath, "/zsh") {
					shell = "zsh"
				} else if strings.HasSuffix(shellPath, "/bash") {
					shell = "bash"
				} else if strings.HasSuffix(shellPath, "/fish") {
					shell = "fish"
				} else {
					return fmt.Errorf("could not detect shell from $SHELL=%q — specify bash, zsh, or fish explicitly", shellPath)
				}
				fmt.Fprintf(os.Stderr, "%s\n", style.MutedStyle.Render(fmt.Sprintf("Detected shell: %s", shell)))
			}

			switch shell {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			default:
				return fmt.Errorf("unsupported shell %q — supported: bash, zsh, fish", shell)
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
		if err := validateAPIBaseURLSecurity(cfg.APIBaseURL); err != nil {
			initErr = err
			return
		}
		if err := validateInsecureTLSUsage(cfg.APIBaseURL, insecureTLS); err != nil {
			initErr = err
			return
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
			api.WithUserAgent("Prysm-CLI/2.5"),
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
		// Token precedence: --token flag > PRYSM_TOKEN env > session file
		token := overrideToken
		if token == "" {
			token = os.Getenv("PRYSM_TOKEN")
		}

		if token != "" {
			// Use explicit token from flag or env var
			app.API.SetToken(token)
		} else {
			// Fall back to session file
			sess, err := app.Sessions.Load()
			if err == nil && sess != nil {
				if overrideAPI == "" && sess.APIBaseURL != "" && !strings.EqualFold(sess.APIBaseURL, app.Config.APIBaseURL) {
					if err := validateAPIBaseURLSecurity(sess.APIBaseURL); err != nil {
						return err
					}
					if err := validateInsecureTLSUsage(sess.APIBaseURL, app.InsecureTLS); err != nil {
						return err
					}
					app.Config.APIBaseURL = sess.APIBaseURL
					app.API = api.NewClient(app.Config.APIBaseURL,
						api.WithTimeout(30*time.Second),
						api.WithUserAgent("Prysm-CLI/2.5"),
						api.WithDebug(app.Debug),
						api.WithHostOverride(app.HostOverride),
						api.WithInsecureSkipVerify(app.InsecureTLS),
						api.WithDialAddress(app.DialOverride),
					)
				}
				// Auto-refresh if session is expired but we have a refresh token
				if sess.IsExpired(0) && sess.RefreshToken != "" {
					refreshCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
					refreshResp, refreshErr := app.API.RefreshSession(refreshCtx, sess.RefreshToken)
					cancel()
					if refreshErr == nil && refreshResp != nil {
						sess.Token = refreshResp.Token
						if refreshResp.ExpiresAtUnix > 0 {
							sess.ExpiresAtUnix = refreshResp.ExpiresAtUnix
						}
						if refreshResp.RefreshToken != "" {
							sess.RefreshToken = refreshResp.RefreshToken
						}
						if saveErr := app.Sessions.Save(sess); saveErr == nil {
							printDebug("Session auto-refreshed using refresh token")
						}
					}
				}
				app.API.SetToken(sess.Token)
			}
		}
	}

	// Initialize plugin system (only once, after app is ready)
	initPluginManager()

	return nil
}

func validateAPIBaseURLSecurity(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid API URL %q: %w", raw, err)
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return nil
	}

	if isLoopbackHost(u.Hostname()) {
		return nil
	}

	return fmt.Errorf("insecure API URL %q: plaintext http is only allowed for localhost/loopback; use https", raw)
}

func validateInsecureTLSUsage(raw string, insecure bool) error {
	if !insecure {
		return nil
	}

	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid API URL %q: %w", raw, err)
	}
	if isLoopbackHost(u.Hostname()) {
		return nil
	}

	return fmt.Errorf("--insecure is only allowed for localhost/loopback API URLs")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// pluginRequestOptions returns ExecuteRequest fields (format, debug) from the current app.
// Used so plugin commands get structured output (e.g. json) when -o json or config format is set.
func pluginRequestOptions() plugin.RequestOptions {
	return func() plugin.ExecuteRequest {
		a := MustApp()
		return plugin.ExecuteRequest{
			OutputFormat: a.OutputFormat,
			Debug:        a.Debug,
		}
	}
}

// initPluginManager initialises host services on builtin plugins (whose Cobra
// commands were already registered in init()) and discovers external plugins.
func initPluginManager() {
	if pluginMgr != nil {
		return
	}

	appCtx := &plugin.AppContext{
		Config:   app.Config,
		Sessions: app.Sessions,
		API:      app.API,
		Format:   app.OutputFormat,
		Debug:    app.Debug,
	}
	hostSvc := plugin.NewBuiltinHostServices(appCtx)

	// Wire host services into the eagerly-created builtin plugins.
	onboardPlugin.SetHost(hostSvc)
	statusPlugin.SetHost(hostSvc)
	exitPlugin.SetHost(hostSvc)
	vaultPlugin.SetHost(hostSvc)

	pluginMgr = plugin.NewManager(hostSvc, app.Config.HomeDir, app.Debug)
	pluginMgr.RegisterBuiltin("onboard", onboardPlugin)
	pluginMgr.RegisterBuiltin("status", statusPlugin)
	pluginMgr.RegisterBuiltin("exit", exitPlugin)
	pluginMgr.RegisterBuiltin("vault", vaultPlugin)

	// Discover and register external plugins
	pluginMgr.DiscoverExternalPlugins()
	pluginMgr.RegisterCommands(rootCmd)
}

func printDebug(format string, args ...interface{}) {
	debug := (app != nil && app.Debug) || os.Getenv("PRYSM_DEBUG") == "1" || os.Getenv("PRYSM_DEBUG") == "true"
	if debug {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(os.Stderr, style.MutedStyle.Render("[debug] "+msg))
	}
}
