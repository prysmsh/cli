package vault

import (
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
)

func TestTransitCreateAndListKeys(t *testing.T) {
	p, ctx := testVault(t)

	// Create an AES key.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "mykey"}})
	if resp.ExitCode != 0 || resp.Error != "" {
		t.Fatalf("create-key failed: %s", resp.Error)
	}

	// Create a ChaCha20 key.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "chacha", "--algorithm", "chacha20-poly1305"}})
	if resp.ExitCode != 0 {
		t.Fatalf("create-key chacha failed: %s", resp.Error)
	}

	// Duplicate name should fail.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "mykey"}})
	if resp.ExitCode == 0 {
		t.Fatal("duplicate create-key should fail")
	}

	// List keys.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"list-keys"}, OutputFormat: "json"})
	if !strings.Contains(resp.Stdout, "mykey") || !strings.Contains(resp.Stdout, "chacha") {
		t.Fatalf("list-keys should contain both keys, got: %s", resp.Stdout)
	}
}

func TestTransitKeyInfo(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "infotest"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"key-info", "--name", "infotest"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("key-info failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "aes256-gcm") {
		t.Fatalf("expected aes256-gcm in output, got: %s", resp.Stdout)
	}

	// Non-existent key.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"key-info", "--name", "nope"}})
	if resp.ExitCode == 0 {
		t.Fatal("key-info for nonexistent key should fail")
	}
}

func TestTransitEncryptDecryptAES(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "enc-test"}})

	// Encrypt.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "enc-test", "--plaintext", "hello world"}})
	if resp.ExitCode != 0 {
		t.Fatalf("encrypt failed: %s", resp.Error)
	}
	ct := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(ct, "vault:v1:") {
		t.Fatalf("expected vault:v1: prefix, got: %s", ct)
	}

	// Decrypt.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "enc-test", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("decrypt failed: %s", resp.Error)
	}
	pt := strings.TrimSpace(resp.Stdout)
	if pt != "hello world" {
		t.Fatalf("expected 'hello world', got: %s", pt)
	}
}

func TestTransitEncryptDecryptChaCha(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "chacha-test", "--algorithm", "chacha20-poly1305"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "chacha-test", "--plaintext", "secret data"}})
	if resp.ExitCode != 0 {
		t.Fatalf("encrypt failed: %s", resp.Error)
	}
	ct := strings.TrimSpace(resp.Stdout)

	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "chacha-test", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("decrypt failed: %s", resp.Error)
	}
	if strings.TrimSpace(resp.Stdout) != "secret data" {
		t.Fatalf("unexpected plaintext: %s", resp.Stdout)
	}
}

func TestTransitEncryptDecryptRSA(t *testing.T) {
	if testing.Short() {
		t.Skip("RSA key generation is slow")
	}

	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "rsa-test", "--algorithm", "rsa-4096"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "rsa-test", "--plaintext", "rsa secret"}})
	if resp.ExitCode != 0 {
		t.Fatalf("encrypt failed: %s", resp.Error)
	}
	ct := strings.TrimSpace(resp.Stdout)

	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "rsa-test", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("decrypt failed: %s", resp.Error)
	}
	if strings.TrimSpace(resp.Stdout) != "rsa secret" {
		t.Fatalf("unexpected plaintext: %s", resp.Stdout)
	}
}

func TestTransitRotateAndRewrap(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "rot-test"}})

	// Encrypt with v1.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "rot-test", "--plaintext", "rotate me"}})
	ct := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(ct, "vault:v1:") {
		t.Fatalf("expected v1, got: %s", ct)
	}

	// Rotate key.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"rotate-key", "--name", "rot-test"}})
	if resp.ExitCode != 0 {
		t.Fatalf("rotate failed: %s", resp.Error)
	}

	// New encryption should use v2.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "rot-test", "--plaintext", "new data"}})
	ct2 := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(ct2, "vault:v2:") {
		t.Fatalf("expected v2, got: %s", ct2)
	}

	// v1 ciphertext should still decrypt.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "rot-test", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("decrypt v1 after rotation failed: %s", resp.Error)
	}
	if strings.TrimSpace(resp.Stdout) != "rotate me" {
		t.Fatal("v1 decryption mismatch")
	}

	// Rewrap v1 ciphertext to v2.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"rewrap", "--key", "rot-test", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("rewrap failed: %s", resp.Error)
	}
	rewrapped := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(rewrapped, "vault:v2:") {
		t.Fatalf("expected v2 after rewrap, got: %s", rewrapped)
	}

	// Rewrapped ciphertext should decrypt to original plaintext.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "rot-test", "--ciphertext", rewrapped}})
	if strings.TrimSpace(resp.Stdout) != "rotate me" {
		t.Fatal("rewrapped decryption mismatch")
	}
}

func TestTransitEncryptDecryptHybridPQC(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "pqc-test", "--algorithm", "hybrid-pqc"}})
	if resp.ExitCode != 0 || resp.Error != "" {
		t.Fatalf("create-key hybrid-pqc failed: %s", resp.Error)
	}

	// Encrypt.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "pqc-test", "--plaintext", "quantum safe data"}})
	if resp.ExitCode != 0 {
		t.Fatalf("encrypt failed: %s", resp.Error)
	}
	ct := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(ct, "vault:v1:") {
		t.Fatalf("expected vault:v1: prefix, got: %s", ct)
	}

	// Decrypt.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "pqc-test", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("decrypt failed: %s", resp.Error)
	}
	pt := strings.TrimSpace(resp.Stdout)
	if pt != "quantum safe data" {
		t.Fatalf("expected 'quantum safe data', got: %s", pt)
	}
}

func TestTransitHybridPQCRotateAndRewrap(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "pqc-rot", "--algorithm", "hybrid-pqc"}})

	// Encrypt with v1.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "pqc-rot", "--plaintext", "rotate pqc"}})
	ct := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(ct, "vault:v1:") {
		t.Fatalf("expected v1, got: %s", ct)
	}

	// Rotate key.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"rotate-key", "--name", "pqc-rot"}})
	if resp.ExitCode != 0 {
		t.Fatalf("rotate failed: %s", resp.Error)
	}

	// New encryption uses v2.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "pqc-rot", "--plaintext", "new pqc data"}})
	ct2 := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(ct2, "vault:v2:") {
		t.Fatalf("expected v2, got: %s", ct2)
	}

	// v1 ciphertext still decrypts.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "pqc-rot", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("decrypt v1 after rotation failed: %s", resp.Error)
	}
	if strings.TrimSpace(resp.Stdout) != "rotate pqc" {
		t.Fatal("v1 decryption mismatch")
	}

	// Rewrap v1 ciphertext to v2.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"rewrap", "--key", "pqc-rot", "--ciphertext", ct}})
	if resp.ExitCode != 0 {
		t.Fatalf("rewrap failed: %s", resp.Error)
	}
	rewrapped := strings.TrimSpace(resp.Stdout)
	if !strings.HasPrefix(rewrapped, "vault:v2:") {
		t.Fatalf("expected v2 after rewrap, got: %s", rewrapped)
	}

	// Rewrapped ciphertext decrypts to original plaintext.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "pqc-rot", "--ciphertext", rewrapped}})
	if strings.TrimSpace(resp.Stdout) != "rotate pqc" {
		t.Fatal("rewrapped decryption mismatch")
	}
}

func TestTransitDeleteKey(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "del-test"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"delete-key", "--name", "del-test", "--force"}})
	if resp.ExitCode != 0 {
		t.Fatalf("delete-key failed: %s", resp.Error)
	}

	// Key should be gone.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"key-info", "--name", "del-test"}})
	if resp.ExitCode == 0 {
		t.Fatal("key-info should fail after delete")
	}
}

func TestTransitInvalidCiphertext(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "bad-ct"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"decrypt", "--key", "bad-ct", "--ciphertext", "not-valid"}})
	if resp.ExitCode == 0 {
		t.Fatal("decrypt with invalid ciphertext should fail")
	}
}

func TestTransitMissingFlags(t *testing.T) {
	p, ctx := testVault(t)

	// create-key without --name.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key"}})
	if resp.ExitCode == 0 {
		t.Fatal("create-key without --name should fail")
	}

	// encrypt without --key.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--plaintext", "hello"}})
	if resp.ExitCode == 0 {
		t.Fatal("encrypt without --key should fail")
	}
}

func TestParseVaultCiphertext(t *testing.T) {
	tests := []struct {
		input   string
		wantVer int
		wantErr bool
	}{
		{"vault:v1:aGVsbG8=", 1, false},
		{"vault:v42:dGVzdA==", 42, false},
		{"invalid", 0, true},
		{"vault:x1:aGVsbG8=", 0, true},
		{"vault:v1:", 1, false}, // empty payload is valid base64
	}

	for _, tt := range tests {
		ver, _, err := parseVaultCiphertext(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("parseVaultCiphertext(%q): expected error", tt.input)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("parseVaultCiphertext(%q): unexpected error: %v", tt.input, err)
		}
		if !tt.wantErr && ver != tt.wantVer {
			t.Errorf("parseVaultCiphertext(%q): version=%d, want %d", tt.input, ver, tt.wantVer)
		}
	}
}
