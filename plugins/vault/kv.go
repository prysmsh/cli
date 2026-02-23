package vault

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/prysmsh/cli/internal/plugin"
)

const (
	kvDataBucketName = "kv_data"
	kvMetaBucketName = "kv_meta"
)

// KVEntry holds an encrypted key-value secret.
type KVEntry struct {
	Data      map[string]string `json:"data"`
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// KVMetadata holds metadata about a KV secret.
type KVMetadata struct {
	CustomMeta map[string]string `json:"custom_metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

func (p *VaultPlugin) loadKVEntry(path string) (*KVEntry, error) {
	data, err := p.store.GetEncrypted(kvDataBucketName, path)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var entry KVEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal kv entry: %w", err)
	}
	return &entry, nil
}

func (p *VaultPlugin) saveKVEntry(path string, entry *KVEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return p.store.PutEncrypted(kvDataBucketName, path, data)
}

func (p *VaultPlugin) loadKVMeta(path string) (*KVMetadata, error) {
	data, err := p.store.GetEncrypted(kvMetaBucketName, path)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var meta KVMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (p *VaultPlugin) saveKVMeta(path string, meta *KVMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return p.store.PutEncrypted(kvMetaBucketName, path, data)
}

func validateKVPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path must not contain '..'")
	}
	return nil
}

func (p *VaultPlugin) execKVPut(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	// Usage: put <path> key=value [key=value ...]
	args := req.Args[1:]
	if len(args) < 2 {
		return p.errResp(ctx, "usage: prysm vault kv put <path> key=value [key=value ...]")
	}

	path := args[0]
	if err := validateKVPath(path); err != nil {
		return p.errResp(ctx, err.Error())
	}

	data := make(map[string]string)
	for _, kv := range args[1:] {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return p.errResp(ctx, fmt.Sprintf("invalid key=value pair: %q", kv))
		}
		data[parts[0]] = parts[1]
	}

	now := time.Now().UTC()
	existing, _ := p.loadKVEntry(path)
	version := 1
	if existing != nil {
		version = existing.Version + 1
	}

	entry := &KVEntry{
		Data:      data,
		Version:   version,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if existing != nil {
		entry.CreatedAt = existing.CreatedAt
	}

	if err := p.saveKVEntry(path, entry); err != nil {
		return p.errResp(ctx, err.Error())
	}

	// Ensure metadata exists.
	meta, _ := p.loadKVMeta(path)
	if meta == nil {
		meta = &KVMetadata{CreatedAt: now, UpdatedAt: now}
	} else {
		meta.UpdatedAt = now
	}
	_ = p.saveKVMeta(path, meta)

	p.appendAudit(ctx, "kv.put", fmt.Sprintf("path=%s version=%d keys=%d", path, version, len(data)))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Secret written to %s (version %d)", path, version))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execKVGet(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	field := fs.String("field", "", "return only this field")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}

	remaining := fs.Args()
	if len(remaining) < 1 {
		return p.errResp(ctx, "usage: prysm vault kv get <path> [--field key]")
	}
	path := remaining[0]

	entry, err := p.loadKVEntry(path)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	if entry == nil {
		return p.errResp(ctx, fmt.Sprintf("secret not found at %s", path))
	}

	p.appendAudit(ctx, "kv.get", fmt.Sprintf("path=%s", path))

	wantJSON := req.OutputFormat == "json"
	if wantJSON {
		data, _ := json.MarshalIndent(entry, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	if *field != "" {
		val, ok := entry.Data[*field]
		if !ok {
			return p.errResp(ctx, fmt.Sprintf("field %q not found in %s", *field, path))
		}
		return plugin.ExecuteResponse{Stdout: val + "\n"}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Secret: %s (version %d)", path, entry.Version))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")

	// Sort keys for stable output.
	keys := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	maxKeyLen := 0
	for _, k := range keys {
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}
	for _, k := range keys {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  %-*s  %s", maxKeyLen, k, entry.Data[k]))
	}
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execKVDelete(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	args := req.Args[1:]
	if len(args) < 1 {
		return p.errResp(ctx, "usage: prysm vault kv delete <path>")
	}
	path := args[0]

	entry, _ := p.loadKVEntry(path)
	if entry == nil {
		return p.errResp(ctx, fmt.Sprintf("secret not found at %s", path))
	}

	if err := p.store.Delete(kvDataBucketName, path); err != nil {
		return p.errResp(ctx, err.Error())
	}
	_ = p.store.Delete(kvMetaBucketName, path)

	p.appendAudit(ctx, "kv.delete", fmt.Sprintf("path=%s", path))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Secret deleted: %s", path))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execKVList(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	prefix := fs.String("prefix", "", "filter by path prefix")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}

	keys, err := p.store.List(kvDataBucketName, *prefix)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	wantJSON := req.OutputFormat == "json"
	if wantJSON {
		data, _ := json.MarshalIndent(keys, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	if len(keys) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "No secrets stored")
		return plugin.ExecuteResponse{}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Secrets (%d):", len(keys)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	for _, k := range keys {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  %s", k))
	}
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execKVMetadata(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("metadata", flag.ContinueOnError)
	setFlag := fs.String("set", "", "set metadata key=value")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	remaining := fs.Args()
	if len(remaining) < 1 {
		return p.errResp(ctx, "usage: prysm vault kv metadata <path> [--set key=value]")
	}
	path := remaining[0]

	meta, _ := p.loadKVMeta(path)
	if meta == nil {
		meta = &KVMetadata{
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
	}

	if *setFlag != "" {
		parts := strings.SplitN(*setFlag, "=", 2)
		if len(parts) != 2 {
			return p.errResp(ctx, "invalid --set format, expected key=value")
		}
		if meta.CustomMeta == nil {
			meta.CustomMeta = make(map[string]string)
		}
		meta.CustomMeta[parts[0]] = parts[1]
		meta.UpdatedAt = time.Now().UTC()
		if err := p.saveKVMeta(path, meta); err != nil {
			return p.errResp(ctx, err.Error())
		}
		_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Metadata updated for %s", path))
		return plugin.ExecuteResponse{}
	}

	wantJSON := req.OutputFormat == "json"
	if wantJSON {
		data, _ := json.MarshalIndent(meta, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Metadata: %s", path))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Created: %s", meta.CreatedAt.Format(time.RFC3339)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Updated: %s", meta.UpdatedAt.Format(time.RFC3339)))
	if len(meta.CustomMeta) > 0 {
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "  Custom:")
		for k, v := range meta.CustomMeta {
			_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("    %s = %s", k, v))
		}
	}
	return plugin.ExecuteResponse{}
}
