package vault

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/prysmsh/pkg/pqc"
	"github.com/prysmsh/cli/internal/plugin"
)

const transitBucketName = "transit"

// TransitKeyEntry holds a transit key and all its versions.
type TransitKeyEntry struct {
	Name           string                      `json:"name"`
	Algorithm      string                      `json:"algorithm"`
	CurrentVersion int                         `json:"current_version"`
	Versions       map[int]TransitKeyVersion   `json:"versions"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
}

// TransitKeyVersion holds the key material for a single version.
type TransitKeyVersion struct {
	Material  []byte    `json:"material"` // raw symmetric key or PEM-encoded RSA private key
	CreatedAt time.Time `json:"created_at"`
}

func (p *VaultPlugin) loadTransitKey(name string) (*TransitKeyEntry, error) {
	data, err := p.store.GetEncrypted(transitBucketName, name)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, ErrKeyNotFound
	}
	var entry TransitKeyEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal transit key: %w", err)
	}
	return &entry, nil
}

func (p *VaultPlugin) saveTransitKey(entry *TransitKeyEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal transit key: %w", err)
	}
	return p.store.PutEncrypted(transitBucketName, entry.Name, data)
}

func generateKeyMaterial(algorithm string) ([]byte, error) {
	switch algorithm {
	case "aes256-gcm":
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		return key, nil
	case "chacha20-poly1305":
		key := make([]byte, chacha20poly1305.KeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		return key, nil
	case "rsa-4096":
		privKey, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return nil, err
		}
		privBytes := x509.MarshalPKCS1PrivateKey(privKey)
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})
		return pemBlock, nil
	case "hybrid-pqc":
		kp, err := pqc.GenerateKeyPair()
		if err != nil {
			return nil, err
		}
		return kp.MarshalKeyPair()
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

func transitEncrypt(algorithm string, keyMaterial, plaintext []byte) ([]byte, error) {
	switch algorithm {
	case "aes256-gcm":
		return aesGCMEncrypt(keyMaterial, plaintext)
	case "chacha20-poly1305":
		aead, err := chacha20poly1305.New(keyMaterial)
		if err != nil {
			return nil, err
		}
		nonce := make([]byte, aead.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return nil, err
		}
		return aead.Seal(nonce, nonce, plaintext, nil), nil
	case "rsa-4096":
		block, _ := pem.Decode(keyMaterial)
		if block == nil {
			return nil, fmt.Errorf("invalid RSA key PEM")
		}
		privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return rsa.EncryptOAEP(sha256.New(), rand.Reader, &privKey.PublicKey, plaintext, nil)
	case "hybrid-pqc":
		kp, err := pqc.UnmarshalKeyPair(keyMaterial)
		if err != nil {
			return nil, err
		}
		kemCT, sharedSecret, err := pqc.Encapsulate(kp.PublicKey())
		if err != nil {
			return nil, err
		}
		payloadCT, err := pqc.EncryptPayload(sharedSecret, plaintext)
		if err != nil {
			return nil, err
		}
		result := make([]byte, len(kemCT)+len(payloadCT))
		copy(result, kemCT)
		copy(result[len(kemCT):], payloadCT)
		return result, nil
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

func transitDecrypt(algorithm string, keyMaterial, ciphertext []byte) ([]byte, error) {
	switch algorithm {
	case "aes256-gcm":
		return aesGCMDecrypt(keyMaterial, ciphertext)
	case "chacha20-poly1305":
		aead, err := chacha20poly1305.New(keyMaterial)
		if err != nil {
			return nil, err
		}
		nonceSize := aead.NonceSize()
		if len(ciphertext) < nonceSize+aead.Overhead() {
			return nil, ErrInvalidCiphertext
		}
		nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
		return aead.Open(nil, nonce, ct, nil)
	case "rsa-4096":
		block, _ := pem.Decode(keyMaterial)
		if block == nil {
			return nil, fmt.Errorf("invalid RSA key PEM")
		}
		privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, ciphertext, nil)
	case "hybrid-pqc":
		kp, err := pqc.UnmarshalKeyPair(keyMaterial)
		if err != nil {
			return nil, err
		}
		if len(ciphertext) < pqc.HybridCiphertextSize {
			return nil, ErrInvalidCiphertext
		}
		kemCT := ciphertext[:pqc.HybridCiphertextSize]
		payloadCT := ciphertext[pqc.HybridCiphertextSize:]
		sharedSecret, err := kp.Decapsulate(kemCT)
		if err != nil {
			return nil, err
		}
		return pqc.DecryptPayload(sharedSecret, payloadCT)
	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

func (p *VaultPlugin) execTransitCreateKey(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("create-key", flag.ContinueOnError)
	name := fs.String("name", "", "key name (required)")
	algorithm := fs.String("algorithm", "aes256-gcm", "algorithm: aes256-gcm, chacha20-poly1305, rsa-4096, hybrid-pqc")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *name == "" {
		return p.errResp(ctx, "missing required flag: --name")
	}

	switch *algorithm {
	case "aes256-gcm", "chacha20-poly1305", "rsa-4096", "hybrid-pqc":
	default:
		return p.errResp(ctx, fmt.Sprintf("unsupported algorithm %q — use aes256-gcm, chacha20-poly1305, rsa-4096, or hybrid-pqc", *algorithm))
	}

	if existing, _ := p.loadTransitKey(*name); existing != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q already exists", *name))
	}

	material, err := generateKeyMaterial(*algorithm)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("generate key material: %v", err))
	}

	now := time.Now().UTC()
	entry := &TransitKeyEntry{
		Name:           *name,
		Algorithm:      *algorithm,
		CurrentVersion: 1,
		Versions: map[int]TransitKeyVersion{
			1: {Material: material, CreatedAt: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := p.saveTransitKey(entry); err != nil {
		return p.errResp(ctx, err.Error())
	}

	p.appendAudit(ctx, "transit.create-key", fmt.Sprintf("name=%s algorithm=%s", *name, *algorithm))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Transit key %q created", *name))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Algorithm: %s", *algorithm))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Version:   1"))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execTransitListKeys(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	keys, err := p.store.List(transitBucketName, "")
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	wantJSON := req.OutputFormat == "json"
	for _, a := range req.Args[1:] {
		if a == "--format" || a == "json" {
			wantJSON = true
		}
	}

	if wantJSON {
		var entries []TransitKeyEntry
		for _, name := range keys {
			entry, err := p.loadTransitKey(name)
			if err != nil {
				continue
			}
			// Strip key material from JSON output.
			stripped := *entry
			stripped.Versions = nil
			entries = append(entries, stripped)
		}
		data, _ := json.MarshalIndent(entries, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	if len(keys) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "No transit keys")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "Create one with: prysm vault transit create-key --name mykey")
		return plugin.ExecuteResponse{}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Transit keys (%d):", len(keys)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	for _, name := range keys {
		entry, err := p.loadTransitKey(name)
		if err != nil {
			continue
		}
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  %-20s  %s  v%d", entry.Name, entry.Algorithm, entry.CurrentVersion))
	}
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execTransitKeyInfo(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("key-info", flag.ContinueOnError)
	name := fs.String("name", "", "key name (required)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *name == "" {
		return p.errResp(ctx, "missing required flag: --name")
	}

	entry, err := p.loadTransitKey(*name)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q: %v", *name, err))
	}

	wantJSON := req.OutputFormat == "json"
	if wantJSON {
		info := map[string]interface{}{
			"name":            entry.Name,
			"algorithm":       entry.Algorithm,
			"current_version": entry.CurrentVersion,
			"versions":        len(entry.Versions),
			"created_at":      entry.CreatedAt.Format(time.RFC3339),
			"updated_at":      entry.UpdatedAt.Format(time.RFC3339),
		}
		data, _ := json.MarshalIndent(info, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Transit key: %s", entry.Name))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Algorithm:       %s", entry.Algorithm))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Current version: %d", entry.CurrentVersion))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Total versions:  %d", len(entry.Versions)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Created:         %s", entry.CreatedAt.Format(time.RFC3339)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Updated:         %s", entry.UpdatedAt.Format(time.RFC3339)))

	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  Versions:")
	for v := 1; v <= entry.CurrentVersion; v++ {
		ver, ok := entry.Versions[v]
		if !ok {
			continue
		}
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("    v%d  created %s", v, ver.CreatedAt.Format(time.RFC3339)))
	}
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execTransitRotateKey(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("rotate-key", flag.ContinueOnError)
	name := fs.String("name", "", "key name (required)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *name == "" {
		return p.errResp(ctx, "missing required flag: --name")
	}

	entry, err := p.loadTransitKey(*name)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q: %v", *name, err))
	}

	material, err := generateKeyMaterial(entry.Algorithm)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("generate key material: %v", err))
	}

	now := time.Now().UTC()
	newVersion := entry.CurrentVersion + 1
	entry.Versions[newVersion] = TransitKeyVersion{Material: material, CreatedAt: now}
	entry.CurrentVersion = newVersion
	entry.UpdatedAt = now

	if err := p.saveTransitKey(entry); err != nil {
		return p.errResp(ctx, err.Error())
	}

	p.appendAudit(ctx, "transit.rotate-key", fmt.Sprintf("name=%s version=%d", *name, newVersion))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Key %q rotated to version %d", *name, newVersion))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execTransitDeleteKey(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("delete-key", flag.ContinueOnError)
	name := fs.String("name", "", "key name (required)")
	force := fs.Bool("force", false, "skip confirmation")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *name == "" {
		return p.errResp(ctx, "missing required flag: --name")
	}

	if _, err := p.loadTransitKey(*name); err != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q: %v", *name, err))
	}

	if !*force {
		ok, err := p.host.PromptConfirm(ctx, fmt.Sprintf("Delete transit key %q? This cannot be undone", *name))
		if err != nil || !ok {
			_ = p.host.Log(ctx, plugin.LogLevelInfo, "Cancelled")
			return plugin.ExecuteResponse{}
		}
	}

	if err := p.store.Delete(transitBucketName, *name); err != nil {
		return p.errResp(ctx, err.Error())
	}

	p.appendAudit(ctx, "transit.delete-key", fmt.Sprintf("name=%s", *name))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Key %q deleted", *name))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execTransitEncrypt(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	keyName := fs.String("key", "", "transit key name (required)")
	plaintext := fs.String("plaintext", "", "data to encrypt (reads stdin if omitted)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *keyName == "" {
		return p.errResp(ctx, "missing required flag: --key")
	}

	entry, err := p.loadTransitKey(*keyName)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q: %v", *keyName, err))
	}

	var data []byte
	if *plaintext != "" {
		data = []byte(*plaintext)
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return p.errResp(ctx, fmt.Sprintf("read stdin: %v", err))
		}
	}

	ver := entry.Versions[entry.CurrentVersion]
	ct, err := transitEncrypt(entry.Algorithm, ver.Material, data)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("encrypt: %v", err))
	}

	result := fmt.Sprintf("vault:v%d:%s", entry.CurrentVersion, base64.StdEncoding.EncodeToString(ct))

	p.appendAudit(ctx, "transit.encrypt", fmt.Sprintf("key=%s version=%d", *keyName, entry.CurrentVersion))

	return plugin.ExecuteResponse{Stdout: result + "\n"}
}

func (p *VaultPlugin) execTransitDecrypt(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("decrypt", flag.ContinueOnError)
	keyName := fs.String("key", "", "transit key name (required)")
	ciphertext := fs.String("ciphertext", "", "data to decrypt (reads stdin if omitted)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *keyName == "" {
		return p.errResp(ctx, "missing required flag: --key")
	}

	entry, err := p.loadTransitKey(*keyName)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q: %v", *keyName, err))
	}

	var ctStr string
	if *ciphertext != "" {
		ctStr = strings.TrimSpace(*ciphertext)
	} else {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return p.errResp(ctx, fmt.Sprintf("read stdin: %v", err))
		}
		ctStr = strings.TrimSpace(string(raw))
	}

	version, ctBytes, err := parseVaultCiphertext(ctStr)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	ver, ok := entry.Versions[version]
	if !ok {
		return p.errResp(ctx, fmt.Sprintf("key version %d not found", version))
	}

	plaintext, err := transitDecrypt(entry.Algorithm, ver.Material, ctBytes)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("decrypt: %v", err))
	}

	p.appendAudit(ctx, "transit.decrypt", fmt.Sprintf("key=%s version=%d", *keyName, version))

	return plugin.ExecuteResponse{Stdout: string(plaintext) + "\n"}
}

func (p *VaultPlugin) execTransitRewrap(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("rewrap", flag.ContinueOnError)
	keyName := fs.String("key", "", "transit key name (required)")
	ciphertext := fs.String("ciphertext", "", "ciphertext to rewrap (reads stdin if omitted)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *keyName == "" {
		return p.errResp(ctx, "missing required flag: --key")
	}

	entry, err := p.loadTransitKey(*keyName)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("key %q: %v", *keyName, err))
	}

	var ctStr string
	if *ciphertext != "" {
		ctStr = strings.TrimSpace(*ciphertext)
	} else {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return p.errResp(ctx, fmt.Sprintf("read stdin: %v", err))
		}
		ctStr = strings.TrimSpace(string(raw))
	}

	oldVersion, ctBytes, err := parseVaultCiphertext(ctStr)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	if oldVersion == entry.CurrentVersion {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "Already using latest key version")
		return plugin.ExecuteResponse{Stdout: ctStr + "\n"}
	}

	oldVer, ok := entry.Versions[oldVersion]
	if !ok {
		return p.errResp(ctx, fmt.Sprintf("key version %d not found", oldVersion))
	}

	plaintext, err := transitDecrypt(entry.Algorithm, oldVer.Material, ctBytes)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("decrypt with v%d: %v", oldVersion, err))
	}

	newVer := entry.Versions[entry.CurrentVersion]
	newCt, err := transitEncrypt(entry.Algorithm, newVer.Material, plaintext)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("re-encrypt with v%d: %v", entry.CurrentVersion, err))
	}

	result := fmt.Sprintf("vault:v%d:%s", entry.CurrentVersion, base64.StdEncoding.EncodeToString(newCt))

	p.appendAudit(ctx, "transit.rewrap", fmt.Sprintf("key=%s from=v%d to=v%d", *keyName, oldVersion, entry.CurrentVersion))

	return plugin.ExecuteResponse{Stdout: result + "\n"}
}

// parseVaultCiphertext parses the "vault:v<N>:<base64>" format.
func parseVaultCiphertext(s string) (version int, ciphertext []byte, err error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[0] != "vault" {
		return 0, nil, ErrInvalidCiphertext
	}
	if len(parts[1]) < 2 || parts[1][0] != 'v' {
		return 0, nil, ErrInvalidCiphertext
	}
	if _, err := fmt.Sscanf(parts[1], "v%d", &version); err != nil {
		return 0, nil, ErrInvalidCiphertext
	}
	ciphertext, err = base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	return version, ciphertext, nil
}
