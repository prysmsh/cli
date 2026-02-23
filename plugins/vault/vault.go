// Package vault implements the builtin "vault" plugin that provides a local
// embedded secrets engine with transit encryption, KV storage, PKI certificate
// management, and tamper-evident audit logging.
package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/prysmsh/pkg/pqc"
	"github.com/prysmsh/cli/internal/plugin"
)

// VaultPlugin implements the Prysm builtin vault secrets engine.
type VaultPlugin struct {
	host  plugin.HostServices
	store *Store
	dek   []byte
}

// New creates a new vault plugin. Pass nil for host during eager registration;
// call SetHost before Execute.
func New(host plugin.HostServices) *VaultPlugin {
	return &VaultPlugin{host: host}
}

// SetHost sets or replaces the host services used by this plugin.
func (p *VaultPlugin) SetHost(host plugin.HostServices) {
	p.host = host
}

// Manifest returns the plugin's metadata and command tree.
func (p *VaultPlugin) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "vault",
		Version:     "0.1.0",
		Description: "Embedded secrets engine with transit encryption, KV storage, PKI, and audit",
		Commands: []plugin.CommandSpec{
			{
				Name:  "vault",
				Short: "Embedded secrets engine",
				Subcommands: []plugin.CommandSpec{
					{Name: "init", Short: "Initialize the vault", DisableFlagParsing: true},
					{Name: "status", Short: "Show vault status", DisableFlagParsing: true},
					{
						Name:  "transit",
						Short: "Transit encryption engine",
						Subcommands: []plugin.CommandSpec{
							{Name: "create-key", Short: "Create a named encryption key", DisableFlagParsing: true},
							{Name: "list-keys", Short: "List all transit keys", DisableFlagParsing: true},
							{Name: "key-info", Short: "Show key metadata", DisableFlagParsing: true},
							{Name: "rotate-key", Short: "Rotate key to a new version", DisableFlagParsing: true},
							{Name: "delete-key", Short: "Delete a transit key", DisableFlagParsing: true},
							{Name: "encrypt", Short: "Encrypt data", DisableFlagParsing: true},
							{Name: "decrypt", Short: "Decrypt data", DisableFlagParsing: true},
							{Name: "rewrap", Short: "Re-encrypt with latest key version", DisableFlagParsing: true},
						},
					},
					{
						Name:  "kv",
						Short: "Key-value secret storage",
						Subcommands: []plugin.CommandSpec{
							{Name: "put", Short: "Store a secret", DisableFlagParsing: true},
							{Name: "get", Short: "Retrieve a secret", DisableFlagParsing: true},
							{Name: "delete", Short: "Delete a secret", DisableFlagParsing: true},
							{Name: "list", Short: "List secrets", DisableFlagParsing: true},
							{Name: "metadata", Short: "Show or set secret metadata", DisableFlagParsing: true},
						},
					},
					{
						Name:  "pki",
						Short: "PKI certificate management",
						Subcommands: []plugin.CommandSpec{
							{Name: "init-ca", Short: "Generate root CA", DisableFlagParsing: true},
							{Name: "get-ca", Short: "Export root CA certificate", DisableFlagParsing: true},
							{Name: "issue", Short: "Issue a certificate", DisableFlagParsing: true},
							{Name: "list-certs", Short: "List issued certificates", DisableFlagParsing: true},
							{Name: "revoke", Short: "Revoke a certificate", DisableFlagParsing: true},
							{Name: "crl", Short: "Export current CRL", DisableFlagParsing: true},
							{Name: "sign-csr", Short: "Sign a CSR", DisableFlagParsing: true},
						},
					},
					{
						Name:  "audit",
						Short: "Tamper-evident audit log",
						Subcommands: []plugin.CommandSpec{
							{Name: "log", Short: "Query audit entries", DisableFlagParsing: true},
							{Name: "verify", Short: "Verify hash chain integrity", DisableFlagParsing: true},
							{Name: "export", Short: "Export audit log", DisableFlagParsing: true},
						},
					},
				},
			},
		},
	}
}

// Execute dispatches the command to the appropriate handler.
func (p *VaultPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	if len(req.Args) == 0 {
		return plugin.ExecuteResponse{Error: "subcommand required"}
	}
	switch req.Args[0] {
	case "init":
		return p.execInit(ctx, req)
	case "status":
		return p.execStatus(ctx, req)
	// Transit
	case "create-key":
		return p.withVault(ctx, req, p.execTransitCreateKey)
	case "list-keys":
		return p.withVault(ctx, req, p.execTransitListKeys)
	case "key-info":
		return p.withVault(ctx, req, p.execTransitKeyInfo)
	case "rotate-key":
		return p.withVault(ctx, req, p.execTransitRotateKey)
	case "delete-key":
		return p.withVault(ctx, req, p.execTransitDeleteKey)
	case "encrypt":
		return p.withVault(ctx, req, p.execTransitEncrypt)
	case "decrypt":
		return p.withVault(ctx, req, p.execTransitDecrypt)
	case "rewrap":
		return p.withVault(ctx, req, p.execTransitRewrap)
	// KV
	case "put":
		return p.withVault(ctx, req, p.execKVPut)
	case "get":
		return p.withVault(ctx, req, p.execKVGet)
	case "delete":
		return p.withVault(ctx, req, p.execKVDelete)
	case "list":
		return p.withVault(ctx, req, p.execKVList)
	case "metadata":
		return p.withVault(ctx, req, p.execKVMetadata)
	// PKI
	case "init-ca":
		return p.withVault(ctx, req, p.execPKIInitCA)
	case "get-ca":
		return p.withVault(ctx, req, p.execPKIGetCA)
	case "issue":
		return p.withVault(ctx, req, p.execPKIIssue)
	case "list-certs":
		return p.withVault(ctx, req, p.execPKIListCerts)
	case "revoke":
		return p.withVault(ctx, req, p.execPKIRevoke)
	case "crl":
		return p.withVault(ctx, req, p.execPKICRL)
	case "sign-csr":
		return p.withVault(ctx, req, p.execPKISignCSR)
	// Audit
	case "log":
		return p.withVault(ctx, req, p.execAuditLog)
	case "verify":
		return p.withVault(ctx, req, p.execAuditVerify)
	case "export":
		return p.withVault(ctx, req, p.execAuditExport)
	default:
		return plugin.ExecuteResponse{Error: fmt.Sprintf("unknown subcommand: %s", req.Args[0])}
	}
}

// commandHandler is a function signature for vault subcommand handlers.
type commandHandler func(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse

// withVault opens the vault, executes the handler, then closes the vault.
func (p *VaultPlugin) withVault(ctx context.Context, req plugin.ExecuteRequest, handler commandHandler) plugin.ExecuteResponse {
	if err := p.openVault(ctx); err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelError, err.Error())
		return plugin.ExecuteResponse{ExitCode: 1}
	}
	defer p.closeVault()
	return handler(ctx, req)
}

// vaultDir returns the vault data directory path.
func (p *VaultPlugin) vaultDir(ctx context.Context) (string, error) {
	cfg, err := p.host.GetConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("get config: %w", err)
	}
	return filepath.Join(cfg.HomeDir, "vault"), nil
}

// vaultDBPath returns the vault database file path.
func (p *VaultPlugin) vaultDBPath(ctx context.Context) (string, error) {
	dir, err := p.vaultDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.db"), nil
}

// openVault opens the vault store and derives/unwraps the DEK.
func (p *VaultPlugin) openVault(ctx context.Context) error {
	dbPath, err := p.vaultDBPath(ctx)
	if err != nil {
		return err
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return err
	}
	if !store.IsInitialized() {
		store.Close()
		return ErrVaultNotInitialized
	}

	auth, err := p.host.GetAuthContext(ctx)
	if err != nil {
		store.Close()
		return fmt.Errorf("not authenticated — run `prysm login` first")
	}

	salt, err := store.GetMeta("salt")
	if err != nil || salt == nil {
		store.Close()
		return fmt.Errorf("vault metadata corrupted: missing salt")
	}

	// Derive primary KEK from current session token.
	kek, err := DeriveKEK(auth.Token, salt)
	if err != nil {
		store.Close()
		return err
	}

	// Try PQC envelope first if available.
	var dek []byte
	encryptedPrivKey, _ := store.GetMeta("pqc_private_key")
	if encryptedPrivKey != nil {
		dek, err = unwrapPQCDEK(kek, salt, store)
	}

	if dek == nil {
		// Classical path (legacy vault or PQC KEK failed).
		wrappedDEK, wErr := store.GetMeta("wrapped_dek")
		if wErr != nil || wrappedDEK == nil {
			store.Close()
			return fmt.Errorf("vault metadata corrupted: missing wrapped DEK")
		}

		dek, err = UnwrapKey(kek, wrappedDEK)
		if err != nil {
			// Primary KEK failed (token changed), try fallback.
			fallbackKEK, fbErr := DeriveFallbackKEK(salt, auth.UserID, auth.OrgID)
			if fbErr != nil {
				store.Close()
				return fmt.Errorf("derive fallback KEK: %w", fbErr)
			}
			wrappedFallback, _ := store.GetMeta("wrapped_dek_fallback")
			if wrappedFallback == nil {
				store.Close()
				return fmt.Errorf("vault unlock failed: session token changed and no fallback available")
			}
			dek, err = UnwrapKey(fallbackKEK, wrappedFallback)
			if err != nil {
				store.Close()
				return fmt.Errorf("vault unlock failed: both primary and fallback keys rejected")
			}
			// Re-wrap DEK with new primary KEK for next time.
			if newWrapped, wrapErr := WrapKey(kek, dek); wrapErr == nil {
				_ = store.PutMeta("wrapped_dek", newWrapped)
				_ = store.PutMeta("kek_fingerprint", []byte(KEKFingerprint(kek)))
			}
			// Re-wrap PQC envelope with new KEK.
			if encryptedPrivKey != nil {
				if pqcDEK, epk, kemCT, pErr := initPQCEnvelope(kek, salt, dek); pErr == nil {
					_ = store.PutMeta("pqc_wrapped_dek", pqcDEK)
					_ = store.PutMeta("pqc_private_key", epk)
					_ = store.PutMeta("kem_ciphertext", kemCT)
				}
			}
		}
	}

	store.SetDEK(dek)
	p.store = store
	p.dek = dek
	return nil
}

// closeVault closes the vault store and clears sensitive state.
func (p *VaultPlugin) closeVault() {
	if p.store != nil {
		p.store.Close()
		p.store = nil
	}
	p.dek = nil
}

// errResp logs an error and returns an error response.
func (p *VaultPlugin) errResp(ctx context.Context, msg string) plugin.ExecuteResponse {
	_ = p.host.Log(ctx, plugin.LogLevelError, msg)
	return plugin.ExecuteResponse{ExitCode: 1}
}

// initPQCEnvelope generates a PQC envelope that wraps the DEK with a composite key
// derived from both the classical KEK and a hybrid KEM shared secret.
func initPQCEnvelope(kek, salt, dek []byte) (pqcWrappedDEK, encryptedPrivKey, kemCiphertext []byte, err error) {
	kp, err := pqc.GenerateKeyPair()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate PQC keypair: %w", err)
	}
	keypairBytes, err := kp.MarshalKeyPair()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal PQC keypair: %w", err)
	}
	encryptedPrivKey, err = aesGCMEncrypt(kek, keypairBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encrypt PQC private key: %w", err)
	}
	kemCiphertext, sharedSecret, err := pqc.Encapsulate(kp.PublicKey())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("KEM encapsulate: %w", err)
	}
	compositeKey, err := DeriveCompositeKey(kek, sharedSecret, salt)
	if err != nil {
		return nil, nil, nil, err
	}
	pqcWrappedDEK, err = xchacha20Encrypt(compositeKey, dek)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("wrap DEK with composite key: %w", err)
	}
	return pqcWrappedDEK, encryptedPrivKey, kemCiphertext, nil
}

// unwrapPQCDEK unwraps the DEK using the PQC envelope.
func unwrapPQCDEK(kek, salt []byte, store *Store) ([]byte, error) {
	encryptedPrivKey, err := store.GetMeta("pqc_private_key")
	if err != nil || encryptedPrivKey == nil {
		return nil, fmt.Errorf("missing PQC private key")
	}
	keypairBytes, err := aesGCMDecrypt(kek, encryptedPrivKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt PQC private key: %w", err)
	}
	kp, err := pqc.UnmarshalKeyPair(keypairBytes)
	if err != nil {
		return nil, fmt.Errorf("unmarshal PQC keypair: %w", err)
	}
	kemCT, err := store.GetMeta("kem_ciphertext")
	if err != nil || kemCT == nil {
		return nil, fmt.Errorf("missing KEM ciphertext")
	}
	sharedSecret, err := kp.Decapsulate(kemCT)
	if err != nil {
		return nil, fmt.Errorf("KEM decapsulate: %w", err)
	}
	compositeKey, err := DeriveCompositeKey(kek, sharedSecret, salt)
	if err != nil {
		return nil, err
	}
	pqcWrappedDEK, err := store.GetMeta("pqc_wrapped_dek")
	if err != nil || pqcWrappedDEK == nil {
		return nil, fmt.Errorf("missing PQC wrapped DEK")
	}
	return xchacha20Decrypt(compositeKey, pqcWrappedDEK)
}

// execInit initializes a new vault.
func (p *VaultPlugin) execInit(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	auth, err := p.host.GetAuthContext(ctx)
	if err != nil {
		return p.errResp(ctx, "Not authenticated — run `prysm login` first")
	}

	dbPath, err := p.vaultDBPath(ctx)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	defer store.Close()

	if store.IsInitialized() {
		return p.errResp(ctx, ErrVaultAlreadyInitialized.Error())
	}

	salt, err := GenerateSalt()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	dek, err := GenerateDEK()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	kek, err := DeriveKEK(auth.Token, salt)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	fallbackKEK, err := DeriveFallbackKEK(salt, auth.UserID, auth.OrgID)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	wrappedDEK, err := WrapKey(kek, dek)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("wrap DEK: %v", err))
	}
	wrappedFallback, err := WrapKey(fallbackKEK, dek)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("wrap fallback DEK: %v", err))
	}

	// Generate PQC envelope (hybrid X25519 + Kyber768).
	pqcWrappedDEK, encryptedPrivKey, kemCiphertext, err := initPQCEnvelope(kek, salt, dek)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("PQC envelope: %v", err))
	}

	for _, kv := range []struct{ k string; v []byte }{
		{"salt", salt},
		{"wrapped_dek", wrappedDEK},
		{"wrapped_dek_fallback", wrappedFallback},
		{"kek_fingerprint", []byte(KEKFingerprint(kek))},
		{"schema_version", []byte("1")},
		{"pqc_private_key", encryptedPrivKey},
		{"kem_ciphertext", kemCiphertext},
		{"pqc_wrapped_dek", pqcWrappedDEK},
	} {
		if err := store.PutMeta(kv.k, kv.v); err != nil {
			return p.errResp(ctx, err.Error())
		}
	}

	// Log the first audit entry.
	store.SetDEK(dek)
	p.store = store
	p.dek = dek
	p.appendAudit(ctx, "vault.init", "vault initialized")
	p.store = nil
	p.dek = nil

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Vault initialized")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Database:        %s", dbPath))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  KEK fingerprint: %s", KEKFingerprint(kek)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  PQC envelope:    active (X25519 + Kyber768)"))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Schema version:  1"))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "Next steps:")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm vault transit create-key --name mykey")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "  prysm vault kv put secret/db user=admin pass=secret")
	return plugin.ExecuteResponse{}
}

// execStatus shows vault status.
func (p *VaultPlugin) execStatus(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	dbPath, err := p.vaultDBPath(ctx)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, "Vault is not initialized")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
		_ = p.host.Log(ctx, plugin.LogLevelPlain, "Run `prysm vault init` to create a new vault")
		return plugin.ExecuteResponse{}
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	defer store.Close()

	if !store.IsInitialized() {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, "Vault database exists but is not initialized")
		return plugin.ExecuteResponse{}
	}

	fp, _ := store.GetMeta("kek_fingerprint")
	sv, _ := store.GetMeta("schema_version")
	pqcKey, _ := store.GetMeta("pqc_private_key")
	pqcStatus := "inactive"
	if pqcKey != nil {
		pqcStatus = "active (X25519 + Kyber768)"
	}
	transitKeys, _ := store.List("transit", "")
	kvSecrets, _ := store.List("kv_data", "")
	caCert, _ := store.Get("pki", "ca_cert")
	pkiStatus := "not initialized"
	if caCert != nil {
		pkiStatus = "initialized"
	}
	auditCount := store.CountKeys("audit")

	wantJSON := req.OutputFormat == "json"
	for _, a := range req.Args[1:] {
		if a == "--format" || a == "json" {
			wantJSON = true
		}
	}

	if wantJSON {
		info := map[string]interface{}{
			"initialized":     true,
			"database":        dbPath,
			"kek_fingerprint": string(fp),
			"pqc_envelope":    pqcStatus,
			"schema_version":  string(sv),
			"transit_keys":    len(transitKeys),
			"kv_secrets":      len(kvSecrets),
			"pki_status":      pkiStatus,
			"audit_entries":   auditCount,
		}
		data, _ := json.MarshalIndent(info, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Vault is initialized")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Database:        %s", dbPath))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  KEK fingerprint: %s", string(fp)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  PQC envelope:    %s", pqcStatus))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Schema version:  %s", string(sv)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Transit keys:    %d", len(transitKeys)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  KV secrets:      %d", len(kvSecrets)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  PKI:             %s", pkiStatus))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Audit entries:   %d", auditCount))
	return plugin.ExecuteResponse{}
}
