package wg

import (
	"fmt"
	"os/exec"
	"strings"
)

func configureInterface(ifaceName, overlayIP string) error {
	if out, err := exec.Command("ifconfig", ifaceName, "inet", overlayIP+"/32", overlayIP).CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig inet: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("ifconfig", ifaceName, "up").CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig up: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func addRoute(cidr, ifaceName string) error {
	out, err := exec.Command("route", "-n", "add", "-net", cidr, "-interface", ifaceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route add %s: %s: %w", cidr, strings.TrimSpace(string(out)), err)
	}
	return nil
}
