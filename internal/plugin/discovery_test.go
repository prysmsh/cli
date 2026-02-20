package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverExternal_EmptyHomeDir(t *testing.T) {
	dir := t.TempDir()
	// Empty dir -> no plugins
	got := DiscoverExternal(dir)
	if len(got) != 0 {
		t.Errorf("DiscoverExternal(empty) len = %d, want 0", len(got))
	}
}

func TestDiscoverExternal_FromHomePluginsDir(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a fake executable (prysm-plugin-foo)
	pluginPath := filepath.Join(pluginsDir, "prysm-plugin-foo")
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh\nexit 0"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := DiscoverExternal(dir)
	if len(got) != 1 {
		t.Fatalf("DiscoverExternal len = %d, want 1", len(got))
	}
	if got[0].Name != "foo" {
		t.Errorf("Name = %q, want foo", got[0].Name)
	}
	if got[0].Path != pluginPath {
		t.Errorf("Path = %q, want %q", got[0].Path, pluginPath)
	}
}

func TestDiscoverExternal_NonExecutableSkipped(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pluginPath := filepath.Join(pluginsDir, "prysm-plugin-bar")
	if err := os.WriteFile(pluginPath, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DiscoverExternal(dir)
	if len(got) != 0 {
		t.Errorf("DiscoverExternal (non-executable) len = %d, want 0", len(got))
	}
}

func TestDiscoverExternal_NonPrefixSkipped(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	otherPath := filepath.Join(pluginsDir, "other-binary")
	if err := os.WriteFile(otherPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := DiscoverExternal(dir)
	if len(got) != 0 {
		t.Errorf("DiscoverExternal (no prefix) len = %d, want 0", len(got))
	}
}

func TestDiscoverExternal_DeduplicatedByName(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	path1 := filepath.Join(pluginsDir, "prysm-plugin-dup")
	path2 := filepath.Join(pluginsDir, "prysm-plugin-dup2")
	if err := os.WriteFile(path1, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Same name "dup" and "dup2" - we get two. If we had two with same plugin name
	// (e.g. two dirs in PATH both with prysm-plugin-foo), only first is kept.
	got := DiscoverExternal(dir)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestDiscoverExternal_PluginsDirNotExist(t *testing.T) {
	dir := t.TempDir()
	// plugins subdir doesn't exist
	got := DiscoverExternal(dir)
	if len(got) != 0 {
		t.Errorf("DiscoverExternal(no plugins dir) len = %d, want 0", len(got))
	}
}

func TestDiscoverExternal_EmptyPluginNameSkipped(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "prysm-plugin-" with nothing after -> plugin name ""
	badPath := filepath.Join(pluginsDir, "prysm-plugin-")
	if err := os.WriteFile(badPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := DiscoverExternal(dir)
	if len(got) != 0 {
		t.Errorf("DiscoverExternal(prysm-plugin- only) len = %d, want 0", len(got))
	}
}
