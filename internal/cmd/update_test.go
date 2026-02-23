package cmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input   string
		major   int
		minor   int
		patch   int
		wantErr bool
	}{
		{"1.2.3", 1, 2, 3, false},
		{"v1.2.3", 1, 2, 3, false},
		{"0.0.1", 0, 0, 1, false},
		{"10.20.30", 10, 20, 30, false},
		{"bad", 0, 0, 0, true},
		{"1.2", 0, 0, 0, true},
		{"", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := parseSemver(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch {
				t.Errorf("parseSemver(%q) = %d.%d.%d, want %d.%d.%d",
					tt.input, v.Major, v.Minor, v.Patch, tt.major, tt.minor, tt.patch)
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"v1.0.0", "1.0.0", 0},
		{"1.2.3", "v1.2.3", 0},
		{"0.9.0", "1.0.0", -1},
		{"2.0.0", "1.99.99", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got, err := compareSemver(tt.a, tt.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCompareSemverErrors(t *testing.T) {
	_, err := compareSemver("bad", "1.0.0")
	if err == nil {
		t.Fatal("expected error for invalid first argument")
	}

	_, err = compareSemver("1.0.0", "bad")
	if err == nil {
		t.Fatal("expected error for invalid second argument")
	}
}

func TestBuildAssetName(t *testing.T) {
	tests := []struct {
		ver, goos, goarch string
		want              string
	}{
		{"1.0.0", "darwin", "arm64", "prysm-cli-1.0.0-darwin-arm64.tar.gz"},
		{"1.0.0", "darwin", "amd64", "prysm-cli-1.0.0-darwin-amd64.tar.gz"},
		{"2.1.0", "linux", "amd64", "prysm-cli-2.1.0-linux-amd64.tar.gz"},
		{"1.0.0", "linux", "arm64", "prysm-cli-1.0.0-linux-arm64.tar.gz"},
		{"1.0.0", "windows", "amd64", "prysm-cli-1.0.0-windows-amd64.zip"},
		{"3.2.1", "windows", "arm64", "prysm-cli-3.2.1-windows-arm64.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := buildAssetName(tt.ver, tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("buildAssetName(%q, %q, %q) = %q, want %q",
					tt.ver, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestExtractFromTarGz(t *testing.T) {
	content := []byte("#!/bin/sh\necho hello\n")

	// Build a tar.gz in memory with a "prysm" entry.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	if err := tw.WriteHeader(&tar.Header{
		Name: "prysm-cli-1.0.0/prysm",
		Size: int64(len(content)),
		Mode: 0o755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	got, err := extractFromTarGz(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractFromTarGzNotFound(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	if err := tw.WriteHeader(&tar.Header{
		Name: "README.md",
		Size: 5,
		Mode: 0o644,
	}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte("hello"))
	tw.Close()
	gw.Close()

	_, err := extractFromTarGz(buf.Bytes())
	if err == nil {
		t.Fatal("expected error when binary not found in archive")
	}
}

func TestExtractFromZip(t *testing.T) {
	content := []byte("MZ fake exe content")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create("prysm-cli-1.0.0/prysm.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	got, err := extractFromZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractFromZipNotFound(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.Create("README.md")
	fw.Write([]byte("hello"))
	zw.Close()

	_, err := extractFromZip(buf.Bytes())
	if err == nil {
		t.Fatal("expected error when binary not found in archive")
	}
}

func TestParseChecksums(t *testing.T) {
	input := strings.Join([]string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  prysm-cli-1.4.0-linux-amd64.tar.gz",
		"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2  prysm-cli-1.4.0-darwin-arm64.tar.gz",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff  prysm-cli-1.4.0-windows-amd64.zip",
	}, "\n")

	got, err := parseChecksums(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}

	expected := map[string]string{
		"prysm-cli-1.4.0-linux-amd64.tar.gz":  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"prysm-cli-1.4.0-darwin-arm64.tar.gz": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		"prysm-cli-1.4.0-windows-amd64.zip":   "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}
	for filename, wantHash := range expected {
		gotHash, ok := got[filename]
		if !ok {
			t.Errorf("missing entry for %s", filename)
			continue
		}
		if gotHash != wantHash {
			t.Errorf("hash for %s = %s, want %s", filename, gotHash, wantHash)
		}
	}
}

func TestParseChecksumsAdditionalFormats(t *testing.T) {
	input := strings.Join([]string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 *prysm-cli-1.4.0-linux-amd64.tar.gz",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ./prysm-cli-1.4.0-darwin-arm64.tar.gz",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb prysm-cli-1.4.0-windows-amd64.zip",
	}, "\n")

	got, err := parseChecksums(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got["prysm-cli-1.4.0-linux-amd64.tar.gz"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("unexpected hash for linux tar.gz: %q", got["prysm-cli-1.4.0-linux-amd64.tar.gz"])
	}
	if got["prysm-cli-1.4.0-darwin-arm64.tar.gz"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected hash for darwin tar.gz: %q", got["prysm-cli-1.4.0-darwin-arm64.tar.gz"])
	}
	if got["prysm-cli-1.4.0-windows-amd64.zip"] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("unexpected hash for windows zip: %q", got["prysm-cli-1.4.0-windows-amd64.zip"])
	}
}

func TestParseChecksumsInvalid(t *testing.T) {
	// Lines with wrong format (single space, no space, empty) should be skipped.
	input := strings.Join([]string{
		"not-a-valid-line",
		"abc123 invalid-short-hash.tar.gz",
		"",
		"   ",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  valid-file.tar.gz",
	}, "\n")

	got, err := parseChecksums(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 valid entry, got %d: %v", len(got), got)
	}
	if got["valid-file.tar.gz"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("unexpected hash for valid-file.tar.gz: %s", got["valid-file.tar.gz"])
	}

	// Completely invalid input with no valid lines should error.
	_, err = parseChecksums("garbage\nmore garbage\n")
	if err == nil {
		t.Fatal("expected error for input with no valid checksums")
	}
}

func TestFindChecksumAsset(t *testing.T) {
	tests := []struct {
		name     string
		assets   []githubAsset
		wantURL  string
		wantName string
	}{
		{
			name: "canonical sha256sums",
			assets: []githubAsset{
				{Name: "prysm-cli-1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/asset.tar.gz"},
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.com/SHA256SUMS"},
			},
			wantURL:  "https://example.com/SHA256SUMS",
			wantName: "SHA256SUMS",
		},
		{
			name: "fallback checksums txt",
			assets: []githubAsset{
				{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
			},
			wantURL:  "https://example.com/checksums.txt",
			wantName: "checksums.txt",
		},
		{
			name: "missing checksum asset",
			assets: []githubAsset{
				{Name: "prysm-cli-1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/asset.tar.gz"},
			},
			wantURL:  "",
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotName := findChecksumAsset(tt.assets)
			if gotURL != tt.wantURL {
				t.Fatalf("url = %q, want %q", gotURL, tt.wantURL)
			}
			if gotName != tt.wantName {
				t.Fatalf("name = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello world\n")
	hash := sha256.Sum256(data)
	hexHash := fmt.Sprintf("%x", hash)

	if err := verifyChecksum(data, hexHash); err != nil {
		t.Fatalf("expected checksum to match, got error: %v", err)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	data := []byte("hello world\n")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	err := verifyChecksum(data, wrongHash)
	if err == nil {
		t.Fatal("expected error for mismatched checksum")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should mention 'checksum mismatch', got: %v", err)
	}
}

func TestVerifyChecksumInvalidLength(t *testing.T) {
	data := []byte("hello world\n")
	shortHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	err := verifyChecksum(data, shortHash)
	if err == nil {
		t.Fatal("expected error for invalid checksum length")
	}
	if !strings.Contains(err.Error(), "invalid checksum length") {
		t.Fatalf("expected invalid length error, got: %v", err)
	}
}
