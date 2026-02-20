package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

const pluginPrefix = "prysm-plugin-"

// DiscoveredPlugin holds metadata about an external plugin binary found on disk.
type DiscoveredPlugin struct {
	Name string // plugin name without prefix (e.g., "terraform")
	Path string // absolute path to binary
}

// DiscoverExternal scans known directories for external plugin binaries.
// It looks for executables matching `prysm-plugin-*` in:
//  1. $PRYSM_HOME/plugins/
//  2. Each directory in $PATH
func DiscoverExternal(homeDir string) []DiscoveredPlugin {
	var found []DiscoveredPlugin
	seen := make(map[string]bool)

	// 1. Check $PRYSM_HOME/plugins/
	pluginsDir := filepath.Join(homeDir, "plugins")
	scanDir(pluginsDir, &found, seen)

	// 2. Check $PATH
	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	for _, dir := range pathDirs {
		scanDir(dir, &found, seen)
	}

	return found
}

func scanDir(dir string, found *[]DiscoveredPlugin, seen map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, pluginPrefix) {
			continue
		}

		pluginName := strings.TrimPrefix(name, pluginPrefix)
		if pluginName == "" || seen[pluginName] {
			continue
		}

		fullPath := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}
		// Must be executable
		if info.Mode()&0o111 == 0 {
			continue
		}

		seen[pluginName] = true
		*found = append(*found, DiscoveredPlugin{
			Name: pluginName,
			Path: fullPath,
		})
	}
}
