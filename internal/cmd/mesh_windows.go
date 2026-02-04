//go:build windows

package cmd

import "syscall"

// setSysProcAttrSetsid is a no-op on Windows (Setsid doesn't exist).
func setSysProcAttrSetsid(attr *syscall.SysProcAttr) {
	// Setsid is not available on Windows
}
