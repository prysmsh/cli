package subnet

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"syscall"
	"unsafe"
)

// DialFunc dials a remote address (typically via DERP exit peer).
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// SO_ORIGINAL_DST from Linux netfilter headers. We keep this local so
// cross-platform builds don't depend on a linux-only x/sys/unix symbol.
const soOriginalDst = 80

// getOriginalDst retrieves the original destination address of an intercepted
// TCP connection (populated by iptables REDIRECT on Linux).
func getOriginalDst(conn *net.TCPConn) (string, error) {
	f, err := conn.File()
	if err != nil {
		return "", fmt.Errorf("get conn file: %w", err)
	}
	defer f.Close()

	fd := int(f.Fd())

	// struct sockaddr_in: sa_family(2) + port(2) + addr(4) + pad(8) = 16 bytes
	var addr [16]byte
	size := uint32(unsafe.Sizeof(addr))

	// getsockopt(fd, IPPROTO_IP, SO_ORIGINAL_DST, &sockaddr_in, &size)
	_, _, errno := getsockoptRaw(fd, syscall.IPPROTO_IP, soOriginalDst, unsafe.Pointer(&addr[0]), &size)
	if errno != 0 {
		return "", fmt.Errorf("SO_ORIGINAL_DST: %w", errno)
	}

	// Decode: bytes 2-3 = port (big-endian), bytes 4-7 = IPv4 address
	port := int(addr[2])<<8 | int(addr[3])
	ip := net.IP(addr[4:8])
	return fmt.Sprintf("%s:%d", ip, port), nil
}

// serveConn handles a single redirected TCP connection: retrieves the original
// destination, dials via DERP, then bridges the two connections.
func serveConn(conn net.Conn, dialFn DialFunc) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		conn.Close()
		return
	}

	dst, err := getOriginalDst(tc)
	if err != nil {
		log.Printf("[subnet] SO_ORIGINAL_DST: %v — closing", err)
		tc.Close()
		return
	}

	remote, err := dialFn(context.Background(), "tcp", dst)
	if err != nil {
		log.Printf("[subnet] dial %s via DERP: %v", dst, err)
		tc.Close()
		return
	}

	bridge(tc, remote)
}

// bridge bidirectionally copies between two connections, closing both when done.
func bridge(a, b net.Conn) {
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	_, _ = io.Copy(b, a)
	<-done
	a.Close()
	b.Close()
}
