package subnet

import (
	"syscall"
	"unsafe"
)

// getsockoptRaw calls getsockopt(2) with a raw byte buffer. Returns (value,
// errno). Used to retrieve SO_ORIGINAL_DST which isn't wrapped by x/sys/unix
// in a convenient form for our struct sockaddr_in layout.
func getsockoptRaw(fd, level, opt int, val unsafe.Pointer, size *uint32) (int, int, syscall.Errno) {
	r0, r1, errno := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(level),
		uintptr(opt),
		uintptr(val),
		uintptr(unsafe.Pointer(size)),
		0,
	)
	return int(r0), int(r1), errno
}
