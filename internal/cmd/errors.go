package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Patterns for cobra's built-in validation errors.
var (
	reExactArgs    = regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)$`)
	reMinArgs      = regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)$`)
	reMaxArgs      = regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)$`)
	reRangeArgs    = regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)$`)
	reInvalidArg   = regexp.MustCompile(`^invalid argument "([^"]*)" for "([^"]*)"$`)
	reUnknownFlag  = regexp.MustCompile(`^unknown flag: --(\S+)$`)
	reUnknownShort = regexp.MustCompile(`^unknown shorthand flag: '(\S+)' in -(\S+)$`)
	reUnknownCmd   = regexp.MustCompile(`^unknown command "([^"]*)" for "([^"]*)"$`)
	reIsArgError   = []*regexp.Regexp{reExactArgs, reMinArgs, reMaxArgs, reRangeArgs, reInvalidArg}
)

// isArgValidationError returns true if the error looks like a cobra arg validation error.
func isArgValidationError(msg string) bool {
	for _, re := range reIsArgError {
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

// friendlyError rewrites cobra's terse validation errors into helpful messages.
func friendlyError(err error) error {
	msg := err.Error()

	// "accepts 1 arg(s), received 0"
	if m := reExactArgs.FindStringSubmatch(msg); m != nil {
		expected, got := m[1], m[2]
		if got == "0" {
			return fmt.Errorf("this command requires %s argument(s) — none were provided", expected)
		}
		return fmt.Errorf("this command requires exactly %s argument(s), but %s were provided", expected, got)
	}

	// "requires at least N arg(s), only received M"
	if m := reMinArgs.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("this command requires at least %s argument(s), but only %s were provided", m[1], m[2])
	}

	// "accepts at most N arg(s), received M"
	if m := reMaxArgs.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("this command accepts at most %s argument(s), but %s were provided", m[1], m[2])
	}

	// "accepts between N and M arg(s), received K"
	if m := reRangeArgs.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("this command requires %s to %s argument(s), but %s were provided", m[1], m[2], m[3])
	}

	// "invalid argument "foo" for "prysm bar""
	if m := reInvalidArg.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("invalid argument %q for %q", m[1], m[2])
	}

	// "unknown flag: --foo"
	if m := reUnknownFlag.FindStringSubmatch(msg); m != nil {
		flag := m[1]
		if suggestion := suggestFlag(flag); suggestion != "" {
			return fmt.Errorf("unknown flag --%s — did you mean --%s?", flag, suggestion)
		}
		return fmt.Errorf("unknown flag --%s", flag)
	}

	// "unknown shorthand flag: 'x' in -x"
	if m := reUnknownShort.FindStringSubmatch(msg); m != nil {
		return fmt.Errorf("unknown flag -%s", m[1])
	}

	// "unknown command "foo" for "prysm""
	if m := reUnknownCmd.FindStringSubmatch(msg); m != nil {
		unknown, parent := m[1], m[2]
		if suggestion := suggestCommand(unknown, parent); suggestion != "" {
			return fmt.Errorf("unknown command %q — did you mean %q?\n\n  Run `%s --help` to see available commands", unknown, suggestion, parent)
		}
		return fmt.Errorf("unknown command %q\n\n  Run `%s --help` to see available commands", unknown, parent)
	}

	// API auth errors: suggest re-login so the user knows what to do
	if strings.Contains(msg, "api error") && (strings.Contains(msg, "Invalid token") || strings.Contains(msg, "401") || strings.Contains(msg, "Unauthorized")) {
		return fmt.Errorf("%s — run `prysm login` or `prysm session refresh` to authenticate", msg)
	}

	return err
}

// wrapArgsWithHelp wraps a cobra.PositionalArgs validator so that on failure
// it prints the command's usage before returning the friendly error.
func wrapArgsWithHelp(original cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := original(cmd, args); err != nil {
			msg := err.Error()
			if isArgValidationError(msg) {
				w := cmd.ErrOrStderr()
				fmt.Fprintln(w)
				fmt.Fprintf(w, "Usage:\n  %s\n", cmd.UseLine())
				if cmd.HasAvailableSubCommands() {
					fmt.Fprintf(w, "\nAvailable Commands:\n")
					for _, c := range cmd.Commands() {
						if c.IsAvailableCommand() {
							fmt.Fprintf(w, "  %-16s %s\n", c.Name(), c.Short)
						}
					}
				}
				if cmd.ValidArgs != nil {
					fmt.Fprintf(w, "\nValid arguments: %s\n", strings.Join(cmd.ValidArgs, ", "))
				}
				if cmd.Example != "" {
					fmt.Fprintf(w, "\nExamples:\n%s\n", cmd.Example)
				}
				fmt.Fprintln(w)
			}
			return friendlyError(err)
		}
		return nil
	}
}

// patchArgsValidators walks the command tree and wraps all Args validators
// so they print help + a friendly error instead of cobra's terse message.
func patchArgsValidators(cmd *cobra.Command) {
	if cmd.Args != nil {
		cmd.Args = wrapArgsWithHelp(cmd.Args)
	}
	for _, child := range cmd.Commands() {
		patchArgsValidators(child)
	}
}

// suggestCommand finds the closest command name using simple prefix/substring matching.
func suggestCommand(unknown, parent string) string {
	parentCmd, _, _ := rootCmd.Find([]string{parent})
	if parentCmd == nil {
		parentCmd = rootCmd
	}

	unknown = strings.ToLower(unknown)
	var best string
	bestScore := 0

	for _, c := range parentCmd.Commands() {
		if !c.IsAvailableCommand() {
			continue
		}
		name := strings.ToLower(c.Name())
		score := 0

		// Exact prefix match is strong signal
		if strings.HasPrefix(name, unknown) || strings.HasPrefix(unknown, name) {
			score = 3
		} else if strings.Contains(name, unknown) || strings.Contains(unknown, name) {
			score = 2
		} else if levenshtein(name, unknown) <= 2 {
			score = 1
		}

		if score > bestScore {
			bestScore = score
			best = c.Name()
		}
	}

	// Also check aliases
	for _, c := range parentCmd.Commands() {
		if !c.IsAvailableCommand() {
			continue
		}
		for _, alias := range c.Aliases {
			alias = strings.ToLower(alias)
			score := 0
			if strings.HasPrefix(alias, unknown) || strings.HasPrefix(unknown, alias) {
				score = 3
			} else if levenshtein(alias, unknown) <= 2 {
				score = 1
			}
			if score > bestScore {
				bestScore = score
				best = c.Name()
			}
		}
	}

	return best
}

// suggestFlag finds the closest flag name.
func suggestFlag(unknown string) string {
	unknown = strings.ToLower(unknown)
	var best string
	bestDist := 3 // only suggest if distance <= 2

	rootCmd.Flags().VisitAll(func(f *pflag.Flag) {
		name := strings.ToLower(f.Name)
		d := levenshtein(name, unknown)
		if d < bestDist {
			bestDist = d
			best = f.Name
		}
	})

	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// addSuggestionHandlers walks the command tree and adds "did you mean?" RunE
// to every parent command that has subcommands but no RunE of its own.
func addSuggestionHandlers(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		addSuggestionHandlers(child)
	}

	if !cmd.HasSubCommands() || cmd.RunE != nil {
		return
	}

	fullName := cmd.CommandPath()
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) > 0 {
			unknown := args[0]
			if suggestion := suggestSubcommand(unknown, c); suggestion != "" {
				return fmt.Errorf("unknown command %q for %q — did you mean %q?\n\n  Run `%s --help` to see available commands", unknown, fullName, suggestion, fullName)
			}
			return fmt.Errorf("unknown command %q for %q\n\n  Run `%s --help` to see available commands", unknown, fullName, fullName)
		}
		return c.Help()
	}
}

// suggestSubcommand finds the closest subcommand of parent for the unknown input.
func suggestSubcommand(unknown string, parent *cobra.Command) string {
	unknown = strings.ToLower(unknown)
	var best string
	bestScore := 0

	for _, c := range parent.Commands() {
		if !c.IsAvailableCommand() {
			continue
		}
		name := strings.ToLower(c.Name())
		score := 0

		if strings.HasPrefix(name, unknown) || strings.HasPrefix(unknown, name) {
			score = 3
		} else if strings.Contains(name, unknown) || strings.Contains(unknown, name) {
			score = 2
		} else if levenshtein(name, unknown) <= 2 {
			score = 1
		}

		if score > bestScore {
			bestScore = score
			best = c.Name()
		}

		for _, alias := range c.Aliases {
			alias = strings.ToLower(alias)
			aScore := 0
			if strings.HasPrefix(alias, unknown) || strings.HasPrefix(unknown, alias) {
				aScore = 3
			} else if levenshtein(alias, unknown) <= 2 {
				aScore = 1
			}
			if aScore > bestScore {
				bestScore = aScore
				best = c.Name()
			}
		}
	}

	return best
}
