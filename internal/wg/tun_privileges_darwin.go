package wg

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// CheckTUNPrivileges tests whether the current process can create utun devices.
// On macOS this requires root or elevated privileges for AF_SYSTEM sockets.
func CheckTUNPrivileges() error {
	fd, err := unix.Socket(unix.AF_SYSTEM, unix.SOCK_DGRAM, 2)
	if err != nil {
		return fmt.Errorf("insufficient privileges to create WireGuard tunnel — re-run with: sudo prysm mesh connect")
	}
	unix.Close(fd)
	return nil
}
