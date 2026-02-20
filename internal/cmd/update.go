package cmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/style"
)

// githubRelease is the subset of the GitHub releases API we care about.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const releasesURL = "https://api.github.com/repos/prysmsh/cli/releases/latest"

func newUpdateCommand() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update prysm to the latest release",
		Long: `Check for and install the latest release of the Prysm CLI from GitHub.

Use --check to see if an update is available without installing it.`,
		// Skip app init — update works without Prysm config/auth.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(checkOnly)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for updates without installing")
	return cmd
}

func runUpdate(checkOnly bool) error {
	currentVersion := version
	if currentVersion == "dev" || currentVersion == "" {
		fmt.Println(style.Warning.Render("Running a dev build — cannot determine current version."))
		fmt.Println(style.Info.Render("Download the latest release from https://github.com/prysmsh/cli/releases"))
		return nil
	}

	fmt.Println(style.Info.Render("Checking for updates..."))

	rel, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")

	cmp, err := compareSemver(currentVersion, latestVersion)
	if err != nil {
		return fmt.Errorf("compare versions: %w", err)
	}

	if cmp >= 0 {
		fmt.Println(style.Success.Render(fmt.Sprintf("Already up to date (v%s).", currentVersion)))
		return nil
	}

	if checkOnly {
		fmt.Println(style.Warning.Render(fmt.Sprintf("Update available: v%s → v%s", currentVersion, latestVersion)))
		fmt.Println(style.Info.Render("Run 'prysm update' to install."))
		return nil
	}

	// Find the right asset for this OS/arch.
	assetName := buildAssetName(latestVersion, runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no release asset found for %s/%s (expected %s)", runtime.GOOS, runtime.GOARCH, assetName)
	}

	fmt.Println(style.Info.Render(fmt.Sprintf("Downloading v%s...", latestVersion)))

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download release: HTTP %d", resp.StatusCode)
	}

	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read release archive: %w", err)
	}

	binaryData, err := extractBinary(archiveData, assetName)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current binary: %w", err)
	}
	selfPath, err = filepath.EvalSymlinks(selfPath)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	if err := atomicReplaceBinary(selfPath, binaryData); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Println(style.Success.Render(fmt.Sprintf("Updated to v%s.", latestVersion)))
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", releasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "prysm-cli/updater")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parse release JSON: %w", err)
	}
	return &rel, nil
}

// buildAssetName returns the expected archive filename for a given release version,
// OS, and architecture. Examples:
//
//	prysm-cli-1.0.0-darwin-arm64.tar.gz
//	prysm-cli-1.0.0-windows-amd64.zip
func buildAssetName(ver, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("prysm-cli-%s-%s-%s%s", ver, goos, goarch, ext)
}

// extractBinary extracts the "prysm" (or "prysm.exe") binary from the archive data.
func extractBinary(data []byte, assetName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractFromZip(data)
	}
	return extractFromTarGz(data)
}

func extractFromTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}

		name := filepath.Base(hdr.Name)
		if name == "prysm" || name == "prysm.exe" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary not found in tar.gz archive")
}

func extractFromZip(data []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name == "prysm" || name == "prysm.exe" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open file in zip: %w", err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary not found in zip archive")
}

// atomicReplaceBinary writes the new binary to a temp file in the same directory
// and renames it over the original. On Windows it uses a rename-aside strategy.
func atomicReplaceBinary(targetPath string, newBinary []byte) error {
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)

	tmpFile, err := os.CreateTemp(dir, base+".update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(newBinary); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("set permissions: %w", err)
	}

	if runtime.GOOS == "windows" {
		// Windows cannot rename over a running executable; rename the old aside first.
		oldPath := targetPath + ".old"
		os.Remove(oldPath) // clean up any previous .old file
		if err := os.Rename(targetPath, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename aside old binary: %w", err)
		}
		if err := os.Rename(tmpPath, targetPath); err != nil {
			// Try to restore the old binary.
			_ = os.Rename(oldPath, targetPath)
			os.Remove(tmpPath)
			return fmt.Errorf("rename new binary into place: %w", err)
		}
		os.Remove(oldPath)
		return nil
	}

	// POSIX: atomic rename.
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename new binary into place: %w", err)
	}
	return nil
}

// semver is a parsed MAJOR.MINOR.PATCH version.
type semver struct {
	Major, Minor, Patch int
}

// parseSemver parses a "MAJOR.MINOR.PATCH" string (with optional "v" prefix).
func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(s, "v")
	var v semver
	n, err := fmt.Sscanf(s, "%d.%d.%d", &v.Major, &v.Minor, &v.Patch)
	if err != nil || n != 3 {
		return semver{}, fmt.Errorf("invalid semver: %q", s)
	}
	return v, nil
}

// compareSemver returns -1, 0, or 1 as a is less than, equal to, or greater than b.
func compareSemver(a, b string) (int, error) {
	va, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}

	switch {
	case va.Major != vb.Major:
		return cmpInt(va.Major, vb.Major), nil
	case va.Minor != vb.Minor:
		return cmpInt(va.Minor, vb.Minor), nil
	default:
		return cmpInt(va.Patch, vb.Patch), nil
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
