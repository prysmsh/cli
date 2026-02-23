// Package subnet implements transparent subnet routing over DERP.
//
// Architecture (Linux, needs root):
//
//	Any app → kernel routing table
//	   ip route add <CIDR> dev lo    ← route exists so kernel doesn't drop the packet
//	   iptables -t nat OUTPUT -d <CIDR> -p tcp -j REDIRECT --to-port <N>
//	         ↓ kernel redirects TCP connections to 127.0.0.1:<N>
//	   SubnetRouter TCP listener on 127.0.0.1:<N>
//	         ↓ SO_ORIGINAL_DST reveals real destination
//	   dialFunc(ctx, "tcp", "realIP:realPort")
//	         ↓
//	   DERP → agent → cluster service
//
// macOS support is deferred (requires pf/pfctl).
package subnet

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// addRedirect adds an OS route via lo (so the kernel doesn't drop the packet)
// and an iptables OUTPUT REDIRECT rule to intercept outgoing TCP to cidr.
func addRedirect(cidr string, localPort int) error {
	switch runtime.GOOS {
	case "linux":
		// Add a route via loopback so the kernel routes the packet instead of
		// returning EHOSTUNREACH. iptables REDIRECT fires after routing.
		routeOut, routeErr := exec.Command(
			"ip", "route", "add", cidr, "dev", "lo",
		).CombinedOutput()
		if routeErr != nil {
			// If the route already exists that is fine; any other error is fatal.
			if string(routeOut) != "RTNETLINK answers: File exists\n" {
				return fmt.Errorf("ip route add %s dev lo: %w (%s)", cidr, routeErr, routeOut)
			}
		}

		out, err := exec.Command(
			"iptables", "-t", "nat", "-A", "OUTPUT",
			"-d", cidr, "-p", "tcp",
			"-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", localPort),
		).CombinedOutput()
		if err != nil {
			// Roll back the route we just added.
			_ = exec.Command("ip", "route", "del", cidr, "dev", "lo").Run()
			return fmt.Errorf("iptables REDIRECT %s → :%d: %w (%s)", cidr, localPort, err, out)
		}
		return nil
	default:
		return fmt.Errorf("subnet routing not supported on %s (Linux required)", runtime.GOOS)
	}
}

// addBypass inserts a high-priority OUTPUT RETURN rule for cidr so traffic is
// never redirected by our subnet router rules.
func addBypass(cidr string) error {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command(
			"iptables", "-t", "nat", "-I", "OUTPUT", "1",
			"-d", cidr, "-p", "tcp",
			"-j", "RETURN",
		).CombinedOutput()
		if err != nil {
			// Ignore duplicate-equivalent failures and continue.
			msg := string(out)
			if strings.Contains(msg, "exists") {
				return nil
			}
			return fmt.Errorf("iptables bypass %s: %w (%s)", cidr, err, out)
		}
		return nil
	default:
		return fmt.Errorf("subnet bypass not supported on %s", runtime.GOOS)
	}
}

// removeRedirect deletes the iptables OUTPUT REDIRECT rule and the lo route for cidr.
func removeRedirect(cidr string, localPort int) error {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command(
			"iptables", "-t", "nat", "-D", "OUTPUT",
			"-d", cidr, "-p", "tcp",
			"-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", localPort),
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("iptables delete REDIRECT %s: %w (%s)", cidr, err, out)
		}
		// Best-effort route removal; ignore errors (route may have been removed already).
		_ = exec.Command("ip", "route", "del", cidr, "dev", "lo").Run()
		return nil
	default:
		return fmt.Errorf("subnet routing not supported on %s", runtime.GOOS)
	}
}

// cleanStaleRedirects removes any iptables REDIRECT rules for cidr whose
// destination port has no active listener — left over from a previous mesh
// connect that was killed without running Stop/disconnect.
func cleanStaleRedirects(cidr string) int {
	if runtime.GOOS != "linux" {
		return 0
	}
	// Normalise cidr to the form iptables-save uses (e.g. "10.233.0.11/32").
	normalized := cidr
	if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
		normalized = ipNet.String()
	}

	out, err := exec.Command("iptables-save", "-t", "nat").Output()
	if err != nil {
		return 0
	}

	removed := 0
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "-A OUTPUT") || !strings.Contains(line, "-j REDIRECT") {
			continue
		}
		if !strings.Contains(line, normalized) && !strings.Contains(line, cidr) {
			continue
		}
		port := parseRedirectPort(line)
		if port == 0 {
			continue
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err != nil {
			// Nothing listening — stale rule, remove it.
			log.Printf("[subnet] removing stale REDIRECT rule: %s → :%d", cidr, port)
			if removeErr := removeRedirect(cidr, port); removeErr == nil {
				removed++
			}
		} else {
			conn.Close()
		}
	}
	return removed
}

// CleanupStaleRedirectsForCIDRs removes stale REDIRECT rules for the provided
// CIDRs. A stale rule is one whose redirect destination has no active listener.
// Returns the number of successfully removed rules.
func CleanupStaleRedirectsForCIDRs(cidrs []string) int {
	total := 0
	for _, cidr := range uniqueSortedStrings(cidrs) {
		total += cleanStaleRedirects(cidr)
	}
	return total
}

// parseRedirectPort extracts the port from an iptables-save line like:
// -A OUTPUT -d 10.x.x.x/32 -p tcp -j REDIRECT --to-ports 44367
func parseRedirectPort(line string) int {
	const marker = "--to-ports "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0
	}
	rest := line[idx+len(marker):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		end = len(rest)
	}
	port, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0
	}
	return port
}

func removeBypass(cidr string) error {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command(
			"iptables", "-t", "nat", "-D", "OUTPUT",
			"-d", cidr, "-p", "tcp",
			"-j", "RETURN",
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("iptables delete bypass %s: %w (%s)", cidr, err, out)
		}
		return nil
	default:
		return fmt.Errorf("subnet bypass not supported on %s", runtime.GOOS)
	}
}
