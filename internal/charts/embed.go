// Package charts embeds the Prysm Helm charts into the CLI binary.
package charts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:agent
var agentChart embed.FS

// ExtractAgentChart writes the embedded agent chart to a temp directory
// and returns the chart path and the cleanup directory.
// The caller should defer os.RemoveAll(cleanupDir).
func ExtractAgentChart() (chartPath string, cleanupDir string, err error) {
	tmpDir, err := os.MkdirTemp("", "prysm-chart-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}

	err = fs.WalkDir(agentChart, "agent", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		target := filepath.Join(tmpDir, path)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, readErr := agentChart.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read embedded %s: %w", path, readErr)
		}

		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("extract chart: %w", err)
	}

	return filepath.Join(tmpDir, "agent"), tmpDir, nil
}
