package wg

import "fmt"

// CheckTUNPrivileges tests whether the current process can create TUN devices.
// On Windows, WireGuard requires Administrator privileges.
func CheckTUNPrivileges() error {
	return fmt.Errorf("WireGuard tunnel requires Administrator privileges on Windows")
}
