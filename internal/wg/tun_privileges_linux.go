package wg

import (
	"fmt"
	"os"
)

// CheckTUNPrivileges tests whether the current process can create TUN devices.
// On Linux this requires root (uid 0) or CAP_NET_ADMIN.
func CheckTUNPrivileges() error {
	if os.Getuid() == 0 {
		return nil
	}
	return fmt.Errorf("insufficient privileges to create WireGuard tunnel — re-run with sudo, or enable prysm-meshd: sudo systemctl enable --now prysm-meshd")
}
