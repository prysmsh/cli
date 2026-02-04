package derp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EnsureDeviceID returns a stable device identifier stored within the given directory.
// New IDs use hostname-cli to avoid duplicates when CLI and desktop run on the same machine.
func EnsureDeviceID(homeDir string) (string, error) {
	if homeDir == "" {
		return "", fmt.Errorf("home directory is required")
	}

	path := filepath.Join(homeDir, "mesh-device-id")
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}

	id, err := generateID()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("ensure mesh directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist mesh device id: %w", err)
	}

	return id, nil
}

var hostnameSanitize = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func generateID() (string, error) {
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		sanitized := hostnameSanitize.ReplaceAllString(strings.TrimSpace(hostname), "-")
		sanitized = strings.Trim(sanitized, "-")
		if len(sanitized) > 50 {
			sanitized = sanitized[:50]
		}
		if sanitized != "" {
			return sanitized + "-cli", nil
		}
	}
	// Fallback when hostname is unavailable or invalid
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return "cli-" + hex.EncodeToString(buf[:]), nil
}
