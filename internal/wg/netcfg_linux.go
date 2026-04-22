package wg

import (
	"fmt"
	"os/exec"
	"strings"
)

func configureInterface(ifaceName, overlayIP string) error {
	if out, err := exec.Command("ip", "addr", "add", overlayIP+"/32", "dev", ifaceName).CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("ip", "link", "set", ifaceName, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set up: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Allow overlay traffic through Tailscale's iptables rules (non-fatal).
	if exec.Command("iptables", "-C", "ts-input", "-i", ifaceName, "-j", "ACCEPT").Run() != nil {
		_ = exec.Command("iptables", "-I", "ts-input", "3", "-i", ifaceName, "-j", "ACCEPT").Run()
	}
	return nil
}

func addRoute(cidr, ifaceName string) error {
	out, err := exec.Command("ip", "route", "replace", cidr, "dev", ifaceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip route add %s: %s: %w", cidr, strings.TrimSpace(string(out)), err)
	}
	return nil
}
