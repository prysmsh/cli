//go:build unix || linux || darwin

package cmd

import "syscall"

// setSysProcAttrSetsid sets the Setsid field on Unix systems.
func setSysProcAttrSetsid(attr *syscall.SysProcAttr) {
	attr.Setsid = true
}
