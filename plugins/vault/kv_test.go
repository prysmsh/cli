package vault

import (
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
)

func TestKVPutAndGet(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/db", "user=admin", "pass=s3cret"}})
	if resp.ExitCode != 0 {
		t.Fatalf("put failed: %s", resp.Error)
	}

	// Get all fields.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "secret/db"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("get failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "admin") || !strings.Contains(resp.Stdout, "s3cret") {
		t.Fatalf("expected secret data in output, got: %s", resp.Stdout)
	}

	// Get specific field.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "--field", "user", "secret/db"}})
	if resp.ExitCode != 0 {
		t.Fatalf("get --field failed: %s", resp.Error)
	}
	if strings.TrimSpace(resp.Stdout) != "admin" {
		t.Fatalf("expected 'admin', got: %s", resp.Stdout)
	}
}

func TestKVPutOverwrite(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/test", "key=v1"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/test", "key=v2"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "--field", "key", "secret/test"}})
	if strings.TrimSpace(resp.Stdout) != "v2" {
		t.Fatalf("expected 'v2', got: %s", resp.Stdout)
	}

	// Version should be 2.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "secret/test"}, OutputFormat: "json"})
	if !strings.Contains(resp.Stdout, `"version": 2`) {
		t.Fatalf("expected version 2, got: %s", resp.Stdout)
	}
}

func TestKVDelete(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/del", "key=val"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"delete", "secret/del"}})
	if resp.ExitCode != 0 {
		t.Fatalf("delete failed: %s", resp.Error)
	}

	// Get should fail.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "secret/del"}})
	if resp.ExitCode == 0 {
		t.Fatal("get after delete should fail")
	}
}

func TestKVList(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/a", "k=1"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/b", "k=2"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"other/c", "k=3"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"list"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("list failed: %s", resp.Error)
	}

	// Should contain secret/a and secret/b.
	if !strings.Contains(resp.Stdout, "secret/a") || !strings.Contains(resp.Stdout, "secret/b") {
		t.Fatalf("expected secrets in list, got: %s", resp.Stdout)
	}

	// List with prefix.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"list", "--prefix", "secret/"}})
	if resp.ExitCode != 0 {
		t.Fatalf("list with prefix failed: %s", resp.Error)
	}
}

func TestKVMetadata(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/meta", "k=v"}})

	// Set custom metadata.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"metadata", "--set", "owner=team-a", "secret/meta"}})
	if resp.ExitCode != 0 {
		t.Fatalf("metadata set failed: %s", resp.Error)
	}

	// Read metadata.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"metadata", "secret/meta"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("metadata get failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "team-a") {
		t.Fatalf("expected custom metadata, got: %s", resp.Stdout)
	}
}

func TestKVGetNonexistent(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get", "nonexistent/path"}})
	if resp.ExitCode == 0 {
		t.Fatal("get nonexistent should fail")
	}
}

func TestKVDeleteNonexistent(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"delete", "nonexistent/path"}})
	if resp.ExitCode == 0 {
		t.Fatal("delete nonexistent should fail")
	}
}

func TestKVPutMissingArgs(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put"}})
	if resp.ExitCode == 0 {
		t.Fatal("put without args should fail")
	}

	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/x"}})
	if resp.ExitCode == 0 {
		t.Fatal("put without key=value should fail")
	}
}

func TestKVPathValidation(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "../escape", "k=v"}})
	if resp.ExitCode == 0 {
		t.Fatal("path with .. should fail")
	}
}
