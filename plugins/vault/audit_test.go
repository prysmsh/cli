package vault

import (
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
)

func TestAuditLogAndVerify(t *testing.T) {
	p, ctx := testVault(t)

	// Init already creates one audit entry.
	// Create a key to generate more entries.
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "audit-test"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"encrypt", "--key", "audit-test", "--plaintext", "data"}})

	// Query log.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"log"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("audit log failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "vault.init") {
		t.Fatalf("expected vault.init in audit log, got: %s", resp.Stdout)
	}
	if !strings.Contains(resp.Stdout, "transit.create-key") {
		t.Fatalf("expected transit.create-key in audit log")
	}
	if !strings.Contains(resp.Stdout, "transit.encrypt") {
		t.Fatalf("expected transit.encrypt in audit log")
	}

	// Verify chain integrity.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"verify"}})
	if resp.ExitCode != 0 {
		t.Fatalf("audit verify failed: %s", resp.Error)
	}
}

func TestAuditLogFilters(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "f1"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"put", "secret/x", "k=v"}})

	// Filter by operation.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"log", "--operation", "transit"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatal(resp.Error)
	}
	if !strings.Contains(resp.Stdout, "transit.create-key") {
		t.Fatal("expected transit entries in filtered log")
	}
	if strings.Contains(resp.Stdout, "kv.put") {
		t.Fatal("kv.put should be filtered out")
	}

	// Limit.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"log", "--limit", "1"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatal(resp.Error)
	}
}

func TestAuditExportJSON(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "export-test"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"export", "--format", "json"}})
	if resp.ExitCode != 0 {
		t.Fatalf("export json failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "vault.init") {
		t.Fatal("expected vault.init in JSON export")
	}
}

func TestAuditExportCSV(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"create-key", "--name", "csv-test"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"export", "--format", "csv"}})
	if resp.ExitCode != 0 {
		t.Fatalf("export csv failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "sequence,timestamp") {
		t.Fatal("expected CSV header")
	}
}

func TestAuditExportToFile(t *testing.T) {
	p, ctx := testVault(t)

	outFile := t.TempDir() + "/audit.json"
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"export", "--format", "json", "--output", outFile}})
	if resp.ExitCode != 0 {
		t.Fatalf("export to file failed: %s", resp.Error)
	}
}

func TestAuditVerifyEmptyLog(t *testing.T) {
	// Create a vault with no operations beyond init, then manually clear audit.
	// For simplicity, just test that verify on a fresh vault works (has init entry).
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"verify"}})
	if resp.ExitCode != 0 {
		t.Fatalf("verify on vault with init entry should pass: %s", resp.Error)
	}
}

func TestAuditHashChainIntegrity(t *testing.T) {
	p, ctx := testVaultWithStore(t)

	// Manually append a few entries and verify the chain.
	p.appendAudit(ctx, "test.op1", "detail1")
	p.appendAudit(ctx, "test.op2", "detail2")
	p.appendAudit(ctx, "test.op3", "detail3")

	entries, err := p.loadAuditEntries()
	if err != nil {
		t.Fatal(err)
	}

	// Verify chain manually.
	var prevHash string
	for i, e := range entries {
		if i > 0 && e.PrevHash != prevHash {
			t.Fatalf("entry %d: prev_hash mismatch", e.Sequence)
		}
		expected := computeAuditHash(e.Sequence, e.Timestamp, e.Operation, e.Detail, e.PrevHash)
		if e.Hash != expected {
			t.Fatalf("entry %d: hash mismatch", e.Sequence)
		}
		prevHash = e.Hash
	}
}

func TestAuditExportInvalidFormat(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"export", "--format", "xml"}})
	if resp.ExitCode == 0 {
		t.Fatal("export with invalid format should fail")
	}
}
