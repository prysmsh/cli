package vault

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/prysmsh/cli/internal/plugin"
)

const (
	pkiBucketName      = "pki"
	pkiCertsBucketName = "pki_certs"
)

// PKICertRecord describes an issued certificate stored in the vault.
type PKICertRecord struct {
	Serial    string    `json:"serial"`
	Subject   string    `json:"subject"`
	SANs      []string  `json:"sans,omitempty"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	Revoked   bool      `json:"revoked"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
	CertPEM   string    `json:"cert_pem"`
}

func (p *VaultPlugin) execPKIInitCA(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("init-ca", flag.ContinueOnError)
	cn := fs.String("cn", "Prysm Vault Root CA", "common name for the CA")
	ttl := fs.String("ttl", "87600h", "CA certificate validity (e.g. 87600h = 10 years)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}

	// Check if CA already exists.
	existing, _ := p.store.GetEncrypted(pkiBucketName, "ca_cert")
	if existing != nil {
		return p.errResp(ctx, "Root CA already initialized. Delete vault to reinitialize.")
	}

	duration, err := time.ParseDuration(*ttl)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("invalid TTL: %v", err))
	}

	// Generate CA key pair.
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("generate CA key: %v", err))
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("generate serial: %v", err))
	}

	now := time.Now().UTC()
	caTmpl := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   *cn,
			Organization: []string{"Prysm Vault"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(duration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("create CA certificate: %v", err))
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})

	if err := p.store.PutEncrypted(pkiBucketName, "ca_cert", caCertPEM); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if err := p.store.PutEncrypted(pkiBucketName, "ca_key", caKeyPEM); err != nil {
		return p.errResp(ctx, err.Error())
	}
	// Initialize serial counter at 1.
	if err := p.store.PutEncrypted(pkiBucketName, "serial", []byte("1")); err != nil {
		return p.errResp(ctx, err.Error())
	}

	p.appendAudit(ctx, "pki.init-ca", fmt.Sprintf("cn=%s ttl=%s", *cn, *ttl))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, "Root CA initialized")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Subject:   CN=%s, O=Prysm Vault", *cn))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Not After: %s", now.Add(duration).Format(time.RFC3339)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  Serial:    %s", serialNumber.Text(16)))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) loadCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	certPEM, err := p.store.GetEncrypted(pkiBucketName, "ca_cert")
	if err != nil || certPEM == nil {
		return nil, nil, ErrCANotInitialized
	}
	keyPEM, err := p.store.GetEncrypted(pkiBucketName, "ca_key")
	if err != nil || keyPEM == nil {
		return nil, nil, ErrCANotInitialized
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("invalid CA certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("invalid CA key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}

	return cert, key, nil
}

func (p *VaultPlugin) nextSerial() (string, error) {
	data, err := p.store.GetEncrypted(pkiBucketName, "serial")
	if err != nil || data == nil {
		return "", fmt.Errorf("serial counter not found")
	}
	var n int64
	fmt.Sscanf(string(data), "%d", &n)
	next := n + 1
	if err := p.store.PutEncrypted(pkiBucketName, "serial", []byte(fmt.Sprintf("%d", next))); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", next), nil
}

func (p *VaultPlugin) execPKIGetCA(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	certPEM, err := p.store.GetEncrypted(pkiBucketName, "ca_cert")
	if err != nil || certPEM == nil {
		return p.errResp(ctx, ErrCANotInitialized.Error())
	}
	return plugin.ExecuteResponse{Stdout: string(certPEM)}
}

func (p *VaultPlugin) execPKIIssue(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	cn := fs.String("cn", "", "common name (required)")
	san := fs.String("san", "", "subject alternative names (comma-separated)")
	ttl := fs.String("ttl", "8760h", "certificate validity (default 1 year)")
	keyType := fs.String("key-type", "ecdsa-p256", "key type: rsa-2048, rsa-4096, ecdsa-p256")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *cn == "" {
		return p.errResp(ctx, "missing required flag: --cn")
	}

	caCert, caKey, err := p.loadCA()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	duration, err := time.ParseDuration(*ttl)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("invalid TTL: %v", err))
	}

	// Generate key pair for the issued certificate.
	var privKey interface{}
	var pubKey interface{}
	switch *keyType {
	case "rsa-2048":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return p.errResp(ctx, err.Error())
		}
		privKey, pubKey = k, &k.PublicKey
	case "rsa-4096":
		k, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return p.errResp(ctx, err.Error())
		}
		privKey, pubKey = k, &k.PublicKey
	case "ecdsa-p256":
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return p.errResp(ctx, err.Error())
		}
		privKey, pubKey = k, &k.PublicKey
	default:
		return p.errResp(ctx, fmt.Sprintf("unsupported key type: %s", *keyType))
	}

	serial, err := p.nextSerial()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	serialBig := new(big.Int)
	serialBig.SetString(serial, 10)

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serialBig,
		Subject: pkix.Name{
			CommonName: *cn,
		},
		NotBefore:             now,
		NotAfter:              now.Add(duration),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Parse SANs.
	var sans []string
	if *san != "" {
		sans = strings.Split(*san, ",")
		for _, s := range sans {
			s = strings.TrimSpace(s)
			tmpl.DNSNames = append(tmpl.DNSNames, s)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, pubKey, caKey)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("create certificate: %v", err))
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Encode private key.
	var keyPEM []byte
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	case *ecdsa.PrivateKey:
		kb, _ := x509.MarshalECPrivateKey(k)
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	}

	// Store certificate record.
	record := PKICertRecord{
		Serial:    serial,
		Subject:   *cn,
		SANs:      sans,
		NotBefore: now,
		NotAfter:  now.Add(duration),
		CertPEM:   string(certPEM),
	}
	recData, _ := json.Marshal(record)
	if err := p.store.PutEncrypted(pkiCertsBucketName, serial, recData); err != nil {
		return p.errResp(ctx, err.Error())
	}

	p.appendAudit(ctx, "pki.issue", fmt.Sprintf("cn=%s serial=%s", *cn, serial))

	// Output certificate + key.
	var out strings.Builder
	out.Write(certPEM)
	out.Write(keyPEM)

	wantJSON := req.OutputFormat == "json"
	if wantJSON {
		info := map[string]interface{}{
			"serial":     serial,
			"subject":    *cn,
			"sans":       sans,
			"not_before": now.Format(time.RFC3339),
			"not_after":  now.Add(duration).Format(time.RFC3339),
			"cert_pem":   string(certPEM),
			"key_pem":    string(keyPEM),
		}
		data, _ := json.MarshalIndent(info, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Certificate issued: %s (serial %s)", *cn, serial))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	return plugin.ExecuteResponse{Stdout: out.String()}
}

func (p *VaultPlugin) execPKIListCerts(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	keys, err := p.store.List(pkiCertsBucketName, "")
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	wantJSON := req.OutputFormat == "json"

	var records []PKICertRecord
	for _, serial := range keys {
		data, err := p.store.GetEncrypted(pkiCertsBucketName, serial)
		if err != nil {
			continue
		}
		var rec PKICertRecord
		if json.Unmarshal(data, &rec) == nil {
			records = append(records, rec)
		}
	}

	if wantJSON {
		// Strip cert PEM for list view.
		type listItem struct {
			Serial    string    `json:"serial"`
			Subject   string    `json:"subject"`
			NotAfter  time.Time `json:"not_after"`
			Revoked   bool      `json:"revoked"`
		}
		var items []listItem
		for _, r := range records {
			items = append(items, listItem{Serial: r.Serial, Subject: r.Subject, NotAfter: r.NotAfter, Revoked: r.Revoked})
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		return plugin.ExecuteResponse{Stdout: string(data) + "\n"}
	}

	if len(records) == 0 {
		_ = p.host.Log(ctx, plugin.LogLevelInfo, "No certificates issued")
		return plugin.ExecuteResponse{}
	}

	_ = p.host.Log(ctx, plugin.LogLevelInfo, fmt.Sprintf("Certificates (%d):", len(records)))
	_ = p.host.Log(ctx, plugin.LogLevelPlain, "")
	for _, r := range records {
		status := "active"
		if r.Revoked {
			status = "REVOKED"
		} else if time.Now().After(r.NotAfter) {
			status = "expired"
		}
		_ = p.host.Log(ctx, plugin.LogLevelPlain, fmt.Sprintf("  %-8s  %-30s  expires %s  [%s]", r.Serial, r.Subject, r.NotAfter.Format("2006-01-02"), status))
	}
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) execPKIRevoke(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("revoke", flag.ContinueOnError)
	serial := fs.String("serial", "", "certificate serial number (required)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *serial == "" {
		return p.errResp(ctx, "missing required flag: --serial")
	}

	data, err := p.store.GetEncrypted(pkiCertsBucketName, *serial)
	if err != nil || data == nil {
		return p.errResp(ctx, fmt.Sprintf("certificate serial %s not found", *serial))
	}

	var rec PKICertRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if rec.Revoked {
		return p.errResp(ctx, fmt.Sprintf("certificate %s is already revoked", *serial))
	}

	rec.Revoked = true
	rec.RevokedAt = time.Now().UTC()

	recData, _ := json.Marshal(rec)
	if err := p.store.PutEncrypted(pkiCertsBucketName, *serial, recData); err != nil {
		return p.errResp(ctx, err.Error())
	}

	// Rebuild CRL.
	if err := p.rebuildCRL(ctx); err != nil {
		_ = p.host.Log(ctx, plugin.LogLevelWarning, fmt.Sprintf("CRL rebuild failed: %v", err))
	}

	p.appendAudit(ctx, "pki.revoke", fmt.Sprintf("serial=%s subject=%s", *serial, rec.Subject))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("Certificate %s revoked (subject: %s)", *serial, rec.Subject))
	return plugin.ExecuteResponse{}
}

func (p *VaultPlugin) rebuildCRL(ctx context.Context) error {
	caCert, caKey, err := p.loadCA()
	if err != nil {
		return err
	}

	keys, _ := p.store.List(pkiCertsBucketName, "")
	var revokedCerts []pkix.RevokedCertificate
	for _, serial := range keys {
		data, err := p.store.GetEncrypted(pkiCertsBucketName, serial)
		if err != nil {
			continue
		}
		var rec PKICertRecord
		if json.Unmarshal(data, &rec) != nil || !rec.Revoked {
			continue
		}
		serialBig := new(big.Int)
		serialBig.SetString(rec.Serial, 10)
		revokedCerts = append(revokedCerts, pkix.RevokedCertificate{
			SerialNumber:   serialBig,
			RevocationTime: rec.RevokedAt,
		})
	}

	crlTemplate := &x509.RevocationList{
		RevokedCertificateEntries: make([]x509.RevocationListEntry, len(revokedCerts)),
		Number:                    big.NewInt(time.Now().Unix()),
		ThisUpdate:                time.Now().UTC(),
		NextUpdate:                time.Now().UTC().Add(24 * time.Hour),
	}
	for i, rc := range revokedCerts {
		crlTemplate.RevokedCertificateEntries[i] = x509.RevocationListEntry{
			SerialNumber:   rc.SerialNumber,
			RevocationTime: rc.RevocationTime,
		}
	}

	crlDER, err := x509.CreateRevocationList(rand.Reader, crlTemplate, caCert, caKey)
	if err != nil {
		return fmt.Errorf("create CRL: %w", err)
	}

	crlPEM := pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlDER})
	return p.store.PutEncrypted(pkiBucketName, "crl", crlPEM)
}

func (p *VaultPlugin) execPKICRL(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	crlPEM, err := p.store.GetEncrypted(pkiBucketName, "crl")
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	if crlPEM == nil {
		// Generate an empty CRL if none exists.
		if err := p.rebuildCRL(ctx); err != nil {
			return p.errResp(ctx, err.Error())
		}
		crlPEM, _ = p.store.GetEncrypted(pkiBucketName, "crl")
	}
	return plugin.ExecuteResponse{Stdout: string(crlPEM)}
}

func (p *VaultPlugin) execPKISignCSR(ctx context.Context, req plugin.ExecuteRequest) plugin.ExecuteResponse {
	fs := flag.NewFlagSet("sign-csr", flag.ContinueOnError)
	csrFile := fs.String("csr-file", "", "path to CSR PEM file (required)")
	ttl := fs.String("ttl", "8760h", "certificate validity (default 1 year)")
	if err := fs.Parse(req.Args[1:]); err != nil {
		return p.errResp(ctx, err.Error())
	}
	if *csrFile == "" {
		return p.errResp(ctx, "missing required flag: --csr-file")
	}

	caCert, caKey, err := p.loadCA()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}

	csrPEM, err := os.ReadFile(*csrFile)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("read CSR file: %v", err))
	}

	csrBlock, _ := pem.Decode(csrPEM)
	if csrBlock == nil {
		return p.errResp(ctx, "invalid CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("parse CSR: %v", err))
	}
	if err := csr.CheckSignature(); err != nil {
		return p.errResp(ctx, fmt.Sprintf("CSR signature invalid: %v", err))
	}

	duration, err := time.ParseDuration(*ttl)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("invalid TTL: %v", err))
	}

	serial, err := p.nextSerial()
	if err != nil {
		return p.errResp(ctx, err.Error())
	}
	serialBig := new(big.Int)
	serialBig.SetString(serial, 10)

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serialBig,
		Subject:               csr.Subject,
		NotBefore:             now,
		NotAfter:              now.Add(duration),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:              csr.DNSNames,
		IPAddresses:           csr.IPAddresses,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, csr.PublicKey, caKey)
	if err != nil {
		return p.errResp(ctx, fmt.Sprintf("sign certificate: %v", err))
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Store the record.
	record := PKICertRecord{
		Serial:    serial,
		Subject:   csr.Subject.CommonName,
		SANs:      csr.DNSNames,
		NotBefore: now,
		NotAfter:  now.Add(duration),
		CertPEM:   string(certPEM),
	}
	recData, _ := json.Marshal(record)
	_ = p.store.PutEncrypted(pkiCertsBucketName, serial, recData)

	p.appendAudit(ctx, "pki.sign-csr", fmt.Sprintf("cn=%s serial=%s", csr.Subject.CommonName, serial))

	_ = p.host.Log(ctx, plugin.LogLevelSuccess, fmt.Sprintf("CSR signed: %s (serial %s)", csr.Subject.CommonName, serial))
	return plugin.ExecuteResponse{Stdout: string(certPEM)}
}
