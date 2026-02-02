package derp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureDeviceID returns a stable device identifier stored within the given directory.
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

func generateID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return "cli-" + hex.EncodeToString(buf[:]), nil
}
