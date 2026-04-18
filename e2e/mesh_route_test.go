// Package e2e contains functional end-to-end tests that run the real prysm
// CLI binary and real curl against live infrastructure.
//
// Prerequisites:
//   - Valid session at ~/.prysm/session.json (run: prysm login)
//   - Mesh route "test-e2e" active: teste2e.frank.mesh:30000 → api:8080
//   - Root access for subnet mode (sudo)
//   - For TestCrossClusterRoute: CCR active (frank→hp, nginx-test:80) so that
//     cc-nginx-test--default service exists in frank's prysm-system namespace.
//
// Run:
//
//	cd cli && GOWORK=off sudo -E go test ./e2e/ -v -count=1 -timeout 90s
//
// Or build the binary first and point at it:
//
//	PRYSM_BIN=/tmp/prysm-dev GOWORK=off sudo -E go test ./e2e/ -v -count=1
package e2e

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

const (
	meshHost    = "teste2e.frank.mesh"
	meshPort    = 30000
	wantBody    = "Mesh route OK from api"
	socks5Port  = 11080
	connectWait = 4 * time.Second // time to let mesh connect settle

	// cross-cluster test: frank (exit) → hp nginx-test via CCR proxy service.
	// Prerequisite: CCR active (frank→hp, service=nginx-test, namespace=default)
	// so that cc-nginx-test--default exists in frank's prysm-system namespace.
	ccrRouteName  = "test-e2e-ccr"
	ccrSvcName    = "cc-nginx-test--default"
	ccrSvcPort    = 80
	ccrSocks5Port = 11081
	wantCCRBody   = "nginx" // nginx welcome page contains "nginx"
)

// prysm returns the path to the CLI binary.
// Checks PRYSM_BIN env var, then /tmp/prysm-dev, then builds from source.
func prysm(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("PRYSM_BIN"); b != "" {
		return b
	}
	if _, err := os.Stat("/tmp/prysm-dev"); err == nil {
		return "/tmp/prysm-dev"
	}
	t.Log("building CLI binary…")
	cmd := exec.Command("go", "build", "-o", "/tmp/prysm-dev", "./cmd/prysm/")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Dir = ".." // cli/
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	return "/tmp/prysm-dev"
}

// userHome returns the real user's home dir — preserves the invoking user's
// home even when running under sudo (which sets HOME=/root).
func userHome() string {
	// SUDO_USER is set by sudo to the original caller.
	if su := os.Getenv("SUDO_USER"); su != "" {
		if home, err := os.ReadFile("/etc/passwd"); err == nil {
			for _, line := range strings.Split(string(home), "\n") {
				fields := strings.Split(line, ":")
				if len(fields) >= 6 && fields[0] == su {
					return fields[5]
				}
			}
		}
	}
	// Fallback: use current HOME (works when not under sudo).
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	d, _ := os.UserHomeDir()
	return d
}

// prysmEnv returns os.Environ() with HOME set to the real user's home.
func prysmEnv() []string {
	env := os.Environ()
	home := userHome()
	for i, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			env[i] = "HOME=" + home
			return env
		}
	}
	return append(env, "HOME="+home)
}

// requireRoot skips the test if not running as root.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("subnet mode requires root; re-run with sudo")
	}
}

// requireSession skips if no valid session exists.
func requireSession(t *testing.T) {
	t.Helper()
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(home + "/.prysm/session.json"); err != nil {
		t.Skip("no session found — run: prysm login")
	}
}

// meshDisconnect ensures any existing mesh connection is torn down before the test.
func meshDisconnect(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command(bin, "mesh", "disconnect")
	cmd.Env = prysmEnv()
	_ = cmd.Run()
	time.Sleep(300 * time.Millisecond)
}

// waitReachable polls until addr is TCP-reachable or deadline is exceeded.
func waitReachable(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			c.Close()
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}

// curlDirect runs curl against the mesh URL with no proxy (subnet/TUN mode).
func curlDirect(t *testing.T) string {
	t.Helper()
	url := fmt.Sprintf("http://%s:%d/", meshHost, meshPort)
	out, err := exec.Command("curl", "-sf", "--max-time", "5", url).Output()
	if err != nil {
		t.Fatalf("curl direct %s: %v", url, err)
	}
	return strings.TrimSpace(string(out))
}

// curlSocks5 runs curl through a local SOCKS5 proxy.
func curlSocks5(t *testing.T, port int) string {
	t.Helper()
	url := fmt.Sprintf("http://%s:%d/", meshHost, meshPort)
	proxy := fmt.Sprintf("socks5h://127.0.0.1:%d", port)
	out, err := exec.Command("curl", "-sf", "--max-time", "5", "--proxy", proxy, url).Output()
	if err != nil {
		t.Fatalf("curl socks5 %s via %s: %v", url, proxy, err)
	}
	return strings.TrimSpace(string(out))
}

// TestMeshRouteSubnetMode verifies:
//
//	prysm mesh connect (subnet=true, default) → curl http://teste2e.frank.mesh:30000/
func TestMeshRouteSubnetMode(t *testing.T) {
	requireRoot(t)
	requireSession(t)

	bin := prysm(t)
	meshDisconnect(t, bin)

	// Start mesh connect in subnet mode (default: --subnet=true, socks5 disabled).
	cmd := exec.Command(bin, "mesh", "connect", "--foreground", "--socks5-port", "0")
	cmd.Env = prysmEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mesh connect: %v", err)
	}
	t.Cleanup(func() {
		dc := exec.Command(bin, "mesh", "disconnect")
		dc.Env = prysmEnv()
		dc.Run()
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("mesh connect stderr:\n%s", stderr.String())
		}
	})

	t.Logf("waiting %s for mesh connect to settle…", connectWait)
	time.Sleep(connectWait)

	// Verify DNS resolves the mesh hostname to a cluster IP.
	addrs, err := net.LookupHost(meshHost)
	if err != nil || len(addrs) == 0 {
		t.Fatalf("DNS lookup %s: %v", meshHost, err)
	}
	t.Logf("DNS: %s → %v", meshHost, addrs)

	// Direct curl (iptables REDIRECT intercepts traffic to cluster IP).
	got := curlDirect(t)
	t.Logf("response: %q", got)
	if !strings.Contains(got, wantBody) {
		t.Errorf("subnet mode: got %q, want %q", got, wantBody)
	}
}

// TestMeshRouteSocks5Mode verifies:
//
//	prysm mesh connect --subnet=false --socks5-port 11080 →
//	curl --proxy socks5h://127.0.0.1:11080 http://teste2e.frank.mesh:30000/
func TestMeshRouteSocks5Mode(t *testing.T) {
	requireSession(t)

	bin := prysm(t)
	meshDisconnect(t, bin)

	// Start mesh connect in SOCKS5-only mode (no iptables needed, no root).
	cmd := exec.Command(bin, "mesh", "connect", "--foreground",
		"--subnet=false",
		"--socks5-port", fmt.Sprintf("%d", socks5Port),
	)
	cmd.Env = prysmEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mesh connect: %v", err)
	}
	t.Cleanup(func() {
		dc := exec.Command(bin, "mesh", "disconnect")
		dc.Env = prysmEnv()
		dc.Run()
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("mesh connect stderr:\n%s", stderr.String())
		}
	})

	// Wait for SOCKS5 port to be ready.
	socks5Addr := fmt.Sprintf("127.0.0.1:%d", socks5Port)
	t.Logf("waiting for SOCKS5 on %s…", socks5Addr)
	if err := waitReachable(socks5Addr, connectWait); err != nil {
		t.Fatalf("SOCKS5 not ready: %v\nstderr: %s", err, stderr.String())
	}

	// Curl through the SOCKS5 proxy.
	got := curlSocks5(t, socks5Port)
	t.Logf("response: %q", got)
	if !strings.Contains(got, wantBody) {
		t.Errorf("socks5 mode: got %q, want %q", got, wantBody)
	}
}

// TestCrossClusterCurl verifies the CCR data path from within frank's cluster:
//
//	pod in frank → cc-nginx-test--default (ClusterIP in prysm-system)
//	             → DERP → hp → nginx-test:80
//
// No prysm CLI, SOCKS5, or root required — just kubectl access to k3d-frank.
// Prerequisite: CCR active (frank→hp, nginx-test:80).
func TestCrossClusterCurl(t *testing.T) {
	requireKubeContext(t, "k3d-frank")

	// Find the cc-proxy pod that acts as the in-cluster relay.
	nameOut, err := exec.Command(
		"kubectl", "--context", "k3d-frank",
		"get", "pods", "-n", "prysm-system",
		"-l", "app.kubernetes.io/name=cc-proxy",
		"-o", "jsonpath={.items[0].metadata.name}",
	).Output()
	if err != nil || len(strings.TrimSpace(string(nameOut))) == 0 {
		t.Skipf("no cc-proxy pod in k3d-frank/prysm-system (CCR not active?): %v", err)
	}
	podName := strings.TrimSpace(string(nameOut))
	t.Logf("exec pod: %s", podName)

	// Curl the CCR service from inside frank → traffic flows via DERP → hp → nginx-test.
	resp, err := exec.Command(
		"kubectl", "--context", "k3d-frank",
		"exec", "-n", "prysm-system", podName,
		"--", "curl", "-sf", "--max-time", "10",
		"http://cc-nginx-test--default.prysm-system/",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl exec curl: %v\n%s", err, resp)
	}
	got := strings.TrimSpace(string(resp))
	t.Logf("response (first 200 chars): %q", got[:min(len(got), 200)])
	if !strings.Contains(strings.ToLower(got), wantCCRBody) {
		t.Errorf("cross-cluster curl: got %q, want containing %q", got, wantCCRBody)
	}
}

// requireKubeContext skips the test if the named kubectl context is not available.
func requireKubeContext(t *testing.T, ctx string) {
	t.Helper()
	out, err := exec.Command("kubectl", "config", "get-contexts", ctx, "-o", "name").Output()
	if err != nil || strings.TrimSpace(string(out)) != ctx {
		t.Skipf("kubectl context %q not available", ctx)
	}
}

// min returns the smaller of a and b (backfill for Go < 1.21).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestCrossClusterRoute verifies a two-hop path:
//
//	laptop → SOCKS5 → DERP → frank (exit) → cc-nginx-test--default → DERP → hp → nginx-test:80
//
// It dynamically creates a mesh route on frank for the CCR proxy service,
// then curls through SOCKS5 and checks for the nginx welcome response.
func TestCrossClusterRoute(t *testing.T) {
	requireSession(t)

	bin := prysm(t)
	meshDisconnect(t, bin)

	// Create a mesh route on frank for the CCR proxy service.
	createCmd := exec.Command(bin,
		"mesh", "routes", "create",
		"--cluster", "frank",
		"--name", ccrRouteName,
		"--service", ccrSvcName,
		"--service-port", fmt.Sprintf("%d", ccrSvcPort),
	)
	createCmd.Env = prysmEnv()
	createOut, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create mesh route for CCR: %v\n%s", err, createOut)
	}
	t.Logf("routes create output:\n%s", createOut)

	routeID, extPort := parseMeshRouteOutput(string(createOut))
	if routeID == 0 {
		t.Fatalf("could not parse route ID from output:\n%s", createOut)
	}
	if extPort == 0 {
		t.Fatalf("could not parse external port from output:\n%s", createOut)
	}
	t.Logf("created route %d on external port %d", routeID, extPort)

	t.Cleanup(func() {
		del := exec.Command(bin, "mesh", "routes", "delete", fmt.Sprintf("%d", routeID))
		del.Env = prysmEnv()
		if out, err := del.CombinedOutput(); err != nil {
			t.Logf("delete route %d: %v (%s)", routeID, err, out)
		}
	})

	// Start mesh connect in SOCKS5-only mode.
	cmd := exec.Command(bin, "mesh", "connect", "--foreground",
		"--subnet=false",
		"--socks5-port", fmt.Sprintf("%d", ccrSocks5Port),
	)
	cmd.Env = prysmEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mesh connect: %v", err)
	}
	t.Cleanup(func() {
		dc := exec.Command(bin, "mesh", "disconnect")
		dc.Env = prysmEnv()
		dc.Run()
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("mesh connect stderr:\n%s", stderr.String())
		}
	})

	// Wait for SOCKS5 to be ready.
	socks5Addr := fmt.Sprintf("127.0.0.1:%d", ccrSocks5Port)
	t.Logf("waiting for SOCKS5 on %s…", socks5Addr)
	if err := waitReachable(socks5Addr, connectWait); err != nil {
		t.Fatalf("SOCKS5 not ready: %v\nstderr: %s", err, stderr.String())
	}

	// Curl via SOCKS5 to <route_slug>.frank.mesh:<extPort>.
	routeSlug := meshSlug(ccrRouteName) // "teste2eccr"
	meshURL := fmt.Sprintf("http://%s.frank.mesh:%d/", routeSlug, extPort)
	t.Logf("curl via SOCKS5: %s", meshURL)

	proxy := fmt.Sprintf("socks5h://127.0.0.1:%d", ccrSocks5Port)
	out, err := exec.Command("curl", "-sf", "--max-time", "10", "--proxy", proxy, meshURL).CombinedOutput()
	if err != nil {
		t.Fatalf("curl %s via %s: %v\n%s", meshURL, proxy, err, out)
	}
	got := strings.TrimSpace(string(out))
	t.Logf("response: %q", got)
	if !strings.Contains(strings.ToLower(got), wantCCRBody) {
		t.Errorf("cross-cluster route: got %q, want containing %q", got, wantCCRBody)
	}
}

// parseMeshRouteOutput extracts (routeID, externalPort) from
// `prysm mesh routes create` output, e.g.:
//
//	"Route 42 created targeting frank"
//	"Local clients can reach cc-nginx-test--default:80 via :30001 (TCP)."
func parseMeshRouteOutput(out string) (routeID int64, extPort int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Route") && strings.Contains(line, "created") {
			for _, f := range strings.Fields(line) {
				if n, err := strconv.ParseInt(f, 10, 64); err == nil && n > 0 {
					routeID = n
				}
			}
		}
		// "Local clients can reach ... via :30001 (TCP)."
		// "Local clients can reach ... via foo.frank.mesh:30001 (TCP)."
		if strings.Contains(line, "Local clients") {
			if viaIdx := strings.Index(line, " via "); viaIdx >= 0 {
				rest := line[viaIdx+5:]
				if i := strings.IndexByte(rest, ' '); i >= 0 {
					rest = rest[:i]
				}
				if i := strings.LastIndexByte(rest, ':'); i >= 0 {
					portStr := strings.TrimRight(rest[i+1:], ".")
					if n, err := strconv.Atoi(portStr); err == nil && n > 0 {
						extPort = n
					}
				}
			}
		}
	}
	return
}

// meshSlug replicates routeHostSlug from cmd/mesh.go:
// spaces/underscores/slashes/dots → hyphens; letters/digits kept (lowered);
// hyphens themselves are dropped; leading/trailing hyphens trimmed.
func meshSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r == ' ' || r == '_' || r == '/' || r == '.':
			if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
				b.WriteByte('-')
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Trim(b.String(), "-")
}
