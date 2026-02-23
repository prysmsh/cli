//go:build !linux

package subnet

import (
	"fmt"
	"syscall"
	"unsafe"
)

func getsockoptRaw(fd, level, opt int, val unsafe.Pointer, size *uint32) (int, int, syscall.Errno) {
	_ = fmt.Sprintf("getsockoptRaw not available on this platform")
	return 0, 0, syscall.ENOTSUP
}
