package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWithProfileAndEnvOverrides(t *testing.T) {
	t.Setenv("PRYSM_FORMAT", "json")
	t.Setenv("PRYSM_DERP_URL", "wss://derp.staging.prysm.sh/derp")

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

configYAML := `
api_url: https://api.prod.prysm.sh/v1
compliance_url: https://compliance.prod.prysm.sh/v1
profiles:
  staging:
    api_url: https://api.staging.prysm.sh/v1
    home: %s
    format: yaml
`

	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("failed to create home dir: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf(configYAML, homeDir)), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath, "staging")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.APIBaseURL, "https://api.staging.prysm.sh/v1"; got != want {
		t.Fatalf("APIBaseURL mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.ComplianceURL, "https://compliance.prod.prysm.sh/v1"; got != want {
		t.Fatalf("ComplianceURL mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.OutputFormat, "json"; got != want {
		t.Fatalf("OutputFormat mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.DERPServerURL, "wss://derp.staging.prysm.sh/derp"; got != want {
		t.Fatalf("DERPServerURL mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.HomeDir, homeDir; got != want {
		t.Fatalf("HomeDir mismatch: got %q want %q", got, want)
	}
	if got, want := cfg.Profile, "staging"; got != want {
		t.Fatalf("Profile mismatch: got %q want %q", got, want)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load with empty path should not error: %v", err)
	}

	// Should have defaults
	if cfg.APIBaseURL != "https://api.prysm.sh/api/v1" {
		t.Errorf("APIBaseURL = %q, want default", cfg.APIBaseURL)
	}
	if cfg.OutputFormat != "table" {
		t.Errorf("OutputFormat = %q, want table", cfg.OutputFormat)
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml", "")
	if err != nil {
		t.Fatalf("Load with nonexistent path should not error: %v", err)
	}

	// Should have defaults
	if cfg.APIBaseURL != "https://api.prysm.sh/api/v1" {
		t.Errorf("APIBaseURL = %q, want default", cfg.APIBaseURL)
	}
}

func TestLoadDefaultProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `
api_url: https://api.custom.com/v1
format: json
`
	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath, "default")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.APIBaseURL != "https://api.custom.com/v1" {
		t.Errorf("APIBaseURL = %q, want https://api.custom.com/v1", cfg.APIBaseURL)
	}
	if cfg.OutputFormat != "json" {
		t.Errorf("OutputFormat = %q, want json", cfg.OutputFormat)
	}
}

func TestLoadUndefinedProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `
api_url: https://api.example.com
profiles:
  production:
    api_url: https://api.prod.example.com
`
	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath, "nonexistent")
	if err == nil {
		t.Fatal("Expected error for undefined profile")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte("invalid: yaml: content: ["), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(cfgPath, "")
	if err == nil {
		t.Fatal("Expected error for invalid YAML")
	}
}

func TestLoadAllEnvOverrides(t *testing.T) {
	t.Setenv("PRYSM_API_URL", "https://env.api.com")
	t.Setenv("PRYSM_COMPLIANCE_URL", "https://env.compliance.com")
	t.Setenv("PRYSM_DERP_URL", "wss://env.derp.com")
	t.Setenv("PRYSM_HOME", "/env/home")
	t.Setenv("PRYSM_FORMAT", "yaml")
	t.Setenv("PRYSM_ORG", "env-org")

	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.APIBaseURL != "https://env.api.com" {
		t.Errorf("APIBaseURL = %q, want https://env.api.com", cfg.APIBaseURL)
	}
	if cfg.ComplianceURL != "https://env.compliance.com" {
		t.Errorf("ComplianceURL = %q, want https://env.compliance.com", cfg.ComplianceURL)
	}
	if cfg.DERPServerURL != "wss://env.derp.com" {
		t.Errorf("DERPServerURL = %q, want wss://env.derp.com", cfg.DERPServerURL)
	}
	if cfg.HomeDir != "/env/home" {
		t.Errorf("HomeDir = %q, want /env/home", cfg.HomeDir)
	}
	if cfg.OutputFormat != "yaml" {
		t.Errorf("OutputFormat = %q, want yaml", cfg.OutputFormat)
	}
	if cfg.Organization != "env-org" {
		t.Errorf("Organization = %q, want env-org", cfg.Organization)
	}
}

func TestLoadTrailingSlashStripped(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `
api_url: https://api.example.com/v1/
compliance_url: https://compliance.example.com/
derp_url: wss://derp.example.com/derp/
`
	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(cfgPath, "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.APIBaseURL != "https://api.example.com/v1" {
		t.Errorf("APIBaseURL trailing slash not stripped: %q", cfg.APIBaseURL)
	}
	if cfg.ComplianceURL != "https://compliance.example.com" {
		t.Errorf("ComplianceURL trailing slash not stripped: %q", cfg.ComplianceURL)
	}
	if cfg.DERPServerURL != "wss://derp.example.com/derp" {
		t.Errorf("DERPServerURL trailing slash not stripped: %q", cfg.DERPServerURL)
	}
}

func TestDefaultHomeDir(t *testing.T) {
	home, err := DefaultHomeDir()
	if err != nil {
		t.Fatalf("DefaultHomeDir failed: %v", err)
	}

	if home == "" {
		t.Error("DefaultHomeDir returned empty string")
	}
	if !filepath.IsAbs(home) {
		t.Errorf("DefaultHomeDir should return absolute path, got: %q", home)
	}
}

func TestConfigMerge(t *testing.T) {
	base := Config{
		APIBaseURL:    "https://base.api.com",
		ComplianceURL: "https://base.compliance.com",
		DERPServerURL: "wss://base.derp.com",
		HomeDir:       "/base/home",
		OutputFormat:  "table",
		Organization:  "base-org",
	}

	other := Config{
		APIBaseURL:   "https://other.api.com",
		OutputFormat: "json",
	}

	base.merge(other)

	if base.APIBaseURL != "https://other.api.com" {
		t.Errorf("APIBaseURL not overwritten: %q", base.APIBaseURL)
	}
	if base.OutputFormat != "json" {
		t.Errorf("OutputFormat not overwritten: %q", base.OutputFormat)
	}
	// These should not change
	if base.ComplianceURL != "https://base.compliance.com" {
		t.Errorf("ComplianceURL changed unexpectedly: %q", base.ComplianceURL)
	}
	if base.HomeDir != "/base/home" {
		t.Errorf("HomeDir changed unexpectedly: %q", base.HomeDir)
	}
}

func TestLoadWithEmptyProfilesMap(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `
api_url: https://api.example.com
`
	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Non-default profile with no profiles map should error
	_, err := Load(cfgPath, "custom")
	if err == nil {
		t.Fatal("Expected error when requesting profile with no profiles defined")
	}
}

func TestConfigMergeDefaultSession(t *testing.T) {
	base := Config{APIBaseURL: "https://api.example.com", DefaultSession: ""}
	other := Config{DefaultSession: "prod"}
	base.merge(other)
	if base.DefaultSession != "prod" {
		t.Errorf("DefaultSession = %q, want prod", base.DefaultSession)
	}
}

func TestLoadPathIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Pass a directory as config path; ReadFile will fail with a non-IsNotExist error
	_, err := Load(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error when config path is a directory")
	}
	if !strings.Contains(err.Error(), "read config file") {
		t.Errorf("error should mention read config file: %v", err)
	}
}
