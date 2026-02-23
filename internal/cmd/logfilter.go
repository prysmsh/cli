package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/style"
)

func newLogFilterCommand() *cobra.Command {
	logFilterCmd := &cobra.Command{
		Use:   "log-filter",
		Short: "Manage log filter training labels and train the ship/drop model",
	}

	labelsCmd := &cobra.Command{
		Use:   "labels",
		Short: "List, add, or export labels for training",
	}
	labelsCmd.AddCommand(
		newLogFilterLabelsListCommand(),
		newLogFilterLabelsAddCommand(),
		newLogFilterLabelsExportCommand(),
	)
	logFilterCmd.AddCommand(labelsCmd)
	logFilterCmd.AddCommand(newLogFilterTrainCommand())

	return logFilterCmd
}

func newLogFilterLabelsListCommand() *cobra.Command {
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List labels for your organization",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			resp, err := app.API.LogFilterLabelsList(ctx, limit, offset)
			if err != nil {
				return fmt.Errorf("list labels: %w", err)
			}

			fmt.Println(style.Bold.Render("Log filter labels"))
			fmt.Printf("Total: %d (showing %d)\n\n", resp.Total, len(resp.Labels))
			for _, l := range resp.Labels {
				fmt.Printf("  %s  %s\n", l.Label, truncateMsg(l.Message, 60))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "Max labels to return")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset for pagination")
	return cmd
}

func newLogFilterLabelsAddCommand() *cobra.Command {
	var message, label, source string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a single label (message + ship or drop)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return fmt.Errorf("--message is required")
			}
			label = strings.TrimSpace(strings.ToLower(label))
			if label != "ship" && label != "drop" {
				return fmt.Errorf("--label must be 'ship' or 'drop'")
			}
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.LogFilterLabelCreate(ctx, message, label, source); err != nil {
				return fmt.Errorf("add label: %w", err)
			}
			fmt.Println("Label added.")
			return nil
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "Log message text")
	cmd.Flags().StringVar(&label, "label", "ship", "Label: ship or drop")
	cmd.Flags().StringVar(&source, "source", "cli", "Source (e.g. cli, manual)")
	return cmd
}

func newLogFilterLabelsExportCommand() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export all labels to a JSON file (for training)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if out == "" {
				out = "log-filter-labels.json"
			}
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := app.API.LogFilterLabelsExport(ctx)
			if err != nil {
				return fmt.Errorf("export labels: %w", err)
			}
			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			defer f.Close()
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			if err := enc.Encode(map[string]interface{}{"labels": resp.Labels}); err != nil {
				return fmt.Errorf("write JSON: %w", err)
			}
			fmt.Printf("Exported %d labels to %s\n", len(resp.Labels), out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "output", "log-filter-labels.json", "Output file path")
	return cmd
}

func newLogFilterTrainCommand() *cobra.Command {
	var output, scriptDir string
	var minSamples int
	cmd := &cobra.Command{
		Use:   "train",
		Short: "Export labels and run the training script (produces model.joblib)",
		Long:  "Exports labels from the API to a temp file, then runs the Python training script if found. Otherwise exports to log-filter-labels.json and prints instructions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			resp, err := app.API.LogFilterLabelsExport(ctx)
			if err != nil {
				return fmt.Errorf("export labels: %w", err)
			}
			if len(resp.Labels) < minSamples {
				return fmt.Errorf("need at least %d labels to train, have %d; add more via UI or CLI", minSamples, len(resp.Labels))
			}

			tmpFile, err := os.CreateTemp("", "prysm-log-filter-labels-*.json")
			if err != nil {
				return fmt.Errorf("create temp file: %w", err)
			}
			tmpPath := tmpFile.Name()
			defer os.Remove(tmpPath)
			enc := json.NewEncoder(tmpFile)
			if err := enc.Encode(map[string]interface{}{"labels": resp.Labels}); err != nil {
				tmpFile.Close()
				return fmt.Errorf("write labels: %w", err)
			}
			if err := tmpFile.Close(); err != nil {
				return err
			}

			// Prefer script in scriptDir, then PRYSM_HOME/scripts/log_filter_train, then cwd
			trainPy := ""
			for _, d := range []string{scriptDir, filepath.Join(os.Getenv("PRYSM_HOME"), "scripts", "log_filter_train"), "scripts/log_filter_train", "."} {
				if d == "" {
					continue
				}
				p := filepath.Join(d, "train.py")
				if _, err := os.Stat(p); err == nil {
					trainPy = p
					break
				}
			}

			if trainPy != "" {
				// Run Python script
				pyCmd := exec.CommandContext(ctx, "python3", trainPy, "--input", tmpPath, "--output", output, "--min-samples", fmt.Sprintf("%d", minSamples))
				pyCmd.Stdout = os.Stdout
				pyCmd.Stderr = os.Stderr
				if err := pyCmd.Run(); err != nil {
					return fmt.Errorf("training script failed: %w", err)
				}
				fmt.Printf("Model saved to %s. Run 'python3 serve.py --model %s' to serve it.\n", output, output)
				return nil
			}

			// No script found: write labels to cwd and print instructions
			cwdFile := "log-filter-labels.json"
			if err := os.WriteFile(cwdFile, mustMarshal(map[string]interface{}{"labels": resp.Labels}), 0644); err != nil {
				return fmt.Errorf("write %s: %w", cwdFile, err)
			}
			fmt.Println(style.Bold.Render("Training script not found."))
			fmt.Printf("Labels exported to %s (%d samples).\n", cwdFile, len(resp.Labels))
			fmt.Println("Run training manually:")
			fmt.Printf("  python3 train.py --input %s --output %s --min-samples %d\n", cwdFile, output, minSamples)
			fmt.Println("Then serve the model:")
			fmt.Printf("  python3 serve.py --model %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "model.joblib", "Output model path")
	cmd.Flags().StringVar(&scriptDir, "script-dir", "", "Path to scripts/log_filter_train (default: PRYSM_HOME/scripts/log_filter_train or cwd)")
	cmd.Flags().IntVar(&minSamples, "min-samples", 50, "Minimum labels required to train")
	return cmd
}

func truncateMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}
