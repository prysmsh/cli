package wg

import "fmt"

func configureInterface(ifaceName, overlayIP string) error {
	return fmt.Errorf("WireGuard interface configuration not supported on Windows")
}

func addRoute(cidr, ifaceName string) error {
	return fmt.Errorf("route configuration not supported on Windows")
}
