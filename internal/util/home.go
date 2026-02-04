package util

import (
	"os"
	"path/filepath"
)

// PrysmHome returns the Prysm home directory.
// It checks PRYSM_HOME env var first, then falls back to $HOME/.prysm.
func PrysmHome() string {
	if h := os.Getenv("PRYSM_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".prysm")
	}
	return filepath.Join(home, ".prysm")
}

// EnsurePrysmHome creates the Prysm home directory if it doesn't exist.
func EnsurePrysmHome() (string, error) {
	home := PrysmHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	return home, nil
}
