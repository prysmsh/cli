package vault

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/prysmsh/cli/internal/plugin"
)

const auditBucketName = "audit"

// AuditEntry represents a single tamper-evident audit log entry.
type AuditEntry struct {
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Operation string    `json:"operation"`
	Detail    string    `json:"detail"`
	PrevHash  string    `json:"prev_hash"`
	Hash      string    `json:"hash"`
}

func computeAuditHash(seq uint64, ts time.Time, operation, detail, prevHash string) string {
	payload := fmt.Sprintf("%d|%s|%s|%s|%s", seq, ts.Format(time.RFC3339Nano), operation, detail, prevHash)
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:])
}

// appendAudit appends a hash-chained audit entry. Safe to call when store is nil (no-op).
func (p *VaultPlugin) appendAudit(ctx context.Context, operation, detail string) {
	if p.store == nil {
		return
	}

	// Find the last entry's hash.
	var prevHash string
	var lastSeq uint64
	_ = p.store.ScanSequential(auditBucketName, func(seq uint64, value []byte) error {
		lastSeq = seq
		var e AuditEntry
		if json.Unmarshal(value, &e) == nil {
			prevHash = e.Hash
		}
		return nil
	})

	now := time.Now().UTC()
	newSeq := lastSeq + 1
	hash := computeAuditHash(newSeq, now, operation, detail, prevHash)

	entry := AuditEntry{
		Sequence:  newSeq,
		Timestamp: now,
		Operation: operation,
		Detail:    detail,
		PrevHash:  prevHash,
		Hash:      hash,
	}

	data, _ := json.Marshal(entry)
	p.store.AppendSequential(auditBucketName, data)
}

func (p *VaultPlugin) loadAuditEntries() ([]AuditEntry, error) {
	var entries []AuditEntry
	err := p.store.ScanSequential(auditBucketName, func(seq uint64, value []byte) error {
		var e AuditEntry
		if err := json.Unmarshal(value, &e); err != nil {
			return err
		}
		entries = append(entries, e)
		return nil
	})
	return entries, err
}

func (p *VaultPlugin) execAuditLog(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	since := fs.String("since", "", "show entries after this time (RFC3339)")
	until := fs.String("until", "", "show entries before this time (RFC3339)")
	operation := fs.String("operation", "", "filter by operation prefix")
	limit := fs.Int("limit", 50, "max entries to show")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}

	entries, err := p.loadAuditEntries()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	// Apply filters.
	var filtered []AuditEntry
	for _, e := range entries {
		if *since != "" {
			t, err := time.Parse(time.RFC3339, *since)
			if err == nil && e.Timestamp.Before(t) {
				continue
			}
		}
		if *until != "" {
			t, err := time.Parse(time.RFC3339, *until)
			if err == nil && e.Timestamp.After(t) {
				continue
			}
		}
		if *operation != "" && !strings.HasPrefix(e.Operation, *operation) {
			continue
		}
		filtered = append(filtered, e)
	}

	// Apply limit (show most recent).
	if len(filtered) > *limit {
		filtered = filtered[len(filtered)-*limit:]
	}

	wantJSON := req.OutputFormat == "json"
	for _, a := range req.Args[1:] {
		if a == "--format" || a == "json" {
			wantJSON = true
		}
	}

	if wantJSON {
		data, _ := json.MarshalIndent(filtered, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	if len(filtered) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "No audit entries found")
		return plugin.ExecuteResponse{}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Audit log (%d entries):", len(filtered)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	for _, e := range filtered {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  #%-6d  %s  %-24s  %s",
			e.Sequence,
			e.Timestamp.Format("2006-01-02 15:04:05"),
			e.Operation,
			e.Detail,
		))
	}
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execAuditVerify(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	entries, err := p.loadAuditEntries()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	if len(entries) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "Audit log is empty — nothing to verify")
		return plugin.ExecuteResponse{}
	}

	var prevHash string
	for i, e := range entries {
		expected := computeAuditHash(e.Sequence, e.Timestamp, e.Operation, e.Detail, e.PrevHash)
		if e.Hash != expected {
			return p.errResp(ctx, fmt.Sprintf("Hash chain broken at entry #%d: expected %s, got %s", e.Sequence, expected, e.Hash))
		}
		if i > 0 && e.PrevHash != prevHash {
			return p.errResp(ctx, fmt.Sprintf("Chain link broken at entry #%d: prev_hash mismatch", e.Sequence))
		}
		prevHash = e.Hash
	}

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Audit log integrity verified: %d entries, chain intact", len(entries)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  First entry: #%d (%s)", entries[0].Sequence, entries[0].Timestamp.Format(time.RFC3339)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Last entry:  #%d (%s)", entries[len(entries)-1].Sequence, entries[len(entries)-1].Timestamp.Format(time.RFC3339)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Head hash:   %s", entries[len(entries)-1].Hash[:16]+"..."))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execAuditExport(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("format", "json", "output format: json, csv")
	output := fs.String("output", "", "output file path (default: stdout)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}

	entries, err := p.loadAuditEntries()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	var result string
	switch *format {
	case "json":
		data, _ := json.MarshalIndent(entries, "", "  ")
		result = string(data) + "\n"
	case "csv":
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		w.Write([]string{"sequence", "timestamp", "operation", "detail", "prev_hash", "hash"})
		for _, e := range entries {
			w.Write([]string{
				fmt.Sprintf("%d", e.Sequence),
				e.Timestamp.Format(time.RFC3339),
				e.Operation,
				e.Detail,
				e.PrevHash,
				e.Hash,
			})
		}
		w.Flush()
		result = buf.String()
	default:
		return p.errResp(ctx, fmt.Sprintf("unsupported format: %s (use json or csv)", *format))
	}

	if *output != "" {
		if err := writeFile(*output, []byte(result)); err != nil {
			return p.errResp(ctx, fmt.Sprintf("write file: %v", err))
		}
		_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Exported %d entries to %s", len(entries), *output))
		return plugin.ExecuteResponse{}
	}

	return plugin.ExecuteResponse{Stdout: result}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
