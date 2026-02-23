package vault

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/prysmsh/cli/internal/plugin"
)

func TestPKIInitCA(t *testing.T) {
	p, ctx := testVault(t)

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test CA"}})
	if resp.ExitCode != 0 {
		t.Fatalf("init-ca failed: %s", resp.Error)
	}

	// Double init should fail.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca"}})
	if resp.ExitCode == 0 {
		t.Fatal("double init-ca should fail")
	}
}

func TestPKIGetCA(t *testing.T) {
	p, ctx := testVault(t)

	// Get CA before init should fail.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get-ca"}})
	if resp.ExitCode == 0 {
		t.Fatal("get-ca before init should fail")
	}

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test Root CA"}})

	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"get-ca"}})
	if resp.ExitCode != 0 {
		t.Fatalf("get-ca failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "BEGIN CERTIFICATE") {
		t.Fatal("expected PEM certificate in output")
	}

	// Parse the CA cert.
	block, _ := pem.Decode([]byte(resp.Stdout))
	if block == nil {
		t.Fatal("invalid PEM in get-ca output")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "Test Root CA" {
		t.Fatalf("expected CN=Test Root CA, got %s", cert.Subject.CommonName)
	}
	if !cert.IsCA {
		t.Fatal("expected CA=true")
	}
}

func TestPKIIssueCert(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test CA"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue", "--cn", "localhost", "--san", "localhost,127.0.0.1.nip.io"}})
	if resp.ExitCode != 0 {
		t.Fatalf("issue failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "BEGIN CERTIFICATE") {
		t.Fatal("expected certificate in output")
	}
	if !strings.Contains(resp.Stdout, "PRIVATE KEY") {
		t.Fatal("expected private key in output")
	}

	// Parse the issued cert.
	block, _ := pem.Decode([]byte(resp.Stdout))
	cert, _ := x509.ParseCertificate(block.Bytes)
	if cert.Subject.CommonName != "localhost" {
		t.Fatalf("expected CN=localhost, got %s", cert.Subject.CommonName)
	}
}

func TestPKIIssueCertECDSA(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test CA"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue", "--cn", "ecdsa-host", "--key-type", "ecdsa-p256"}})
	if resp.ExitCode != 0 {
		t.Fatalf("issue ecdsa failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "EC PRIVATE KEY") {
		t.Fatal("expected EC private key")
	}
}

func TestPKIListCerts(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test CA"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue", "--cn", "host-a"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue", "--cn", "host-b"}})

	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"list-certs"}, OutputFormat: "json"})
	if resp.ExitCode != 0 {
		t.Fatalf("list-certs failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "host-a") || !strings.Contains(resp.Stdout, "host-b") {
		t.Fatalf("expected both certs in list, got: %s", resp.Stdout)
	}
}

func TestPKIRevoke(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test CA"}})
	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue", "--cn", "revoke-me"}})

	// Revoke serial 2 (serial 1 is for CA self-signed via nextSerial).
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"revoke", "--serial", "2"}})
	if resp.ExitCode != 0 {
		t.Fatalf("revoke failed: %s", resp.Error)
	}

	// Double revoke should fail.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"revoke", "--serial", "2"}})
	if resp.ExitCode == 0 {
		t.Fatal("double revoke should fail")
	}

	// CRL should contain the revoked serial.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"crl"}})
	if resp.ExitCode != 0 {
		t.Fatalf("crl failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "X509 CRL") {
		t.Fatal("expected CRL PEM")
	}
}

func TestPKICRL(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca", "--cn", "Test CA"}})

	// CRL with no revocations should still work.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"crl"}})
	if resp.ExitCode != 0 {
		t.Fatalf("crl failed: %s", resp.Error)
	}
	if !strings.Contains(resp.Stdout, "X509 CRL") {
		t.Fatal("expected CRL PEM")
	}
}

func TestPKIMissingCA(t *testing.T) {
	p, ctx := testVault(t)

	// Issue without CA should fail.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue", "--cn", "test"}})
	if resp.ExitCode == 0 {
		t.Fatal("issue without CA should fail")
	}
}

func TestPKIMissingFlags(t *testing.T) {
	p, ctx := testVault(t)

	p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"init-ca"}})

	// Issue without --cn.
	resp := p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"issue"}})
	if resp.ExitCode == 0 {
		t.Fatal("issue without --cn should fail")
	}

	// Revoke without --serial.
	resp = p.Execute(ctx, plugin.ExecuteRequest{Args: []string{"revoke"}})
	if resp.ExitCode == 0 {
		t.Fatal("revoke without --serial should fail")
	}
}
