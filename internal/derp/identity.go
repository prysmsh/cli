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

// looksHumanReadable returns true if the hostname looks like it was chosen by
// a human rather than auto-generated (container IDs, UUIDs, etc.).
func looksHumanReadable(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Pure hex strings >=12 chars are likely container IDs or machine-ids.
	if len(s) >= 12 {
		allHex := true
		for _, c := range s {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '-') {
				allHex = false
				break
			}
		}
		if allHex {
			return false
		}
	}
	return true
}

func generateID() (string, error) {
	// Try several sources for a human-readable hostname, in order of preference.
	candidates := []string{
		os.Getenv("PRYSM_DEVICE_NAME"),
	}

	// /etc/hostname (Linux, usually set by the user)
	if data, err := os.ReadFile("/etc/hostname"); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(data)))
	}

	// os.Hostname()
	if h, err := os.Hostname(); err == nil {
		candidates = append(candidates, h)
	}

	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		sanitized := hostnameSanitize.ReplaceAllString(strings.TrimSpace(raw), "-")
		sanitized = strings.Trim(sanitized, "-")
		if len(sanitized) > 50 {
			sanitized = sanitized[:50]
		}
		if looksHumanReadable(sanitized) {
			return sanitized + "-cli", nil
		}
	}

	// Fallback when no human-readable hostname is available
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return "cli-" + hex.EncodeToString(buf[:]), nil
}
