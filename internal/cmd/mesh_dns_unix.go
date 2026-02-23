//go:build unix || linux || darwin

package cmd

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type meshDNSServer struct {
	conn   *net.UDPConn
	hostIP map[string]net.IP
	stopCh chan struct{}
}

func startMeshSplitDNS(hostIP map[string]net.IP) (func(), error) {
	if len(hostIP) == 0 {
		return nil, nil
	}
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:53")
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen local DNS: %w", err)
	}
	srv := &meshDNSServer{
		conn:   conn,
		hostIP: normalizeHostMap(hostIP),
		stopCh: make(chan struct{}),
	}
	go srv.serve()

	cleanupDNS, err := configureSplitDNS()
	if err != nil {
		conn.Close()
		return nil, err
	}
	return func() {
		close(srv.stopCh)
		_ = srv.conn.Close()
		if cleanupDNS != nil {
			_ = cleanupDNS()
		}
	}, nil
}

func normalizeHostMap(in map[string]net.IP) map[string]net.IP {
	out := make(map[string]net.IP, len(in))
	for h, ip := range in {
		if v4 := ip.To4(); v4 != nil {
			out[strings.ToLower(strings.TrimSuffix(h, "."))] = v4
		}
	}
	return out
}

func (s *meshDNSServer) serve() {
	buf := make([]byte, 1500)
	for {
		n, raddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				continue
			}
		}
		resp := s.handleQuery(buf[:n])
		if len(resp) > 0 {
			_, _ = s.conn.WriteToUDP(resp, raddr)
		}
	}
}

func (s *meshDNSServer) handleQuery(req []byte) []byte {
	name, qtype, qend, ok := parseDNSQuery(req)
	if !ok {
		return nil
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	ip, found := s.hostIP[name]
	if qtype != 1 { // A only
		found = false
	}
	return buildDNSResponse(req, qend, ip, found)
}

func parseDNSQuery(req []byte) (string, uint16, int, bool) {
	if len(req) < 12 {
		return "", 0, 0, false
	}
	qd := binary.BigEndian.Uint16(req[4:6])
	if qd == 0 {
		return "", 0, 0, false
	}
	i := 12
	labels := []string{}
	for {
		if i >= len(req) {
			return "", 0, 0, false
		}
		l := int(req[i])
		i++
		if l == 0 {
			break
		}
		if i+l > len(req) {
			return "", 0, 0, false
		}
		labels = append(labels, string(req[i:i+l]))
		i += l
	}
	if i+4 > len(req) {
		return "", 0, 0, false
	}
	qtype := binary.BigEndian.Uint16(req[i : i+2])
	i += 4 // qtype + qclass
	return strings.Join(labels, "."), qtype, i, true
}

func buildDNSResponse(req []byte, qend int, ip net.IP, found bool) []byte {
	if len(req) < 12 || qend > len(req) {
		return nil
	}
	resp := make([]byte, 12)
	copy(resp[0:2], req[0:2]) // txid
	// standard response, recursion desired/available
	flags := uint16(0x8180)
	ancount := uint16(0)
	if !found {
		flags = 0x8183 // NXDOMAIN
	} else {
		ancount = 1
	}
	binary.BigEndian.PutUint16(resp[2:4], flags)
	copy(resp[4:6], req[4:6]) // qdcount
	binary.BigEndian.PutUint16(resp[6:8], ancount)
	// nscount/arcount remain zero
	resp = append(resp, req[12:qend]...) // question

	if found {
		// Answer name pointer to question name at offset 12.
		resp = append(resp, 0xc0, 0x0c)
		resp = append(resp, 0x00, 0x01)             // type A
		resp = append(resp, 0x00, 0x01)             // class IN
		resp = append(resp, 0x00, 0x00, 0x00, 0x1e) // TTL 30s
		resp = append(resp, 0x00, 0x04)             // rdlength
		resp = append(resp, ip.To4()...)
	}
	return resp
}

func configureSplitDNS() (func() error, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("resolvectl"); err != nil {
			return nil, fmt.Errorf("resolvectl not found (required for .mesh split DNS)")
		}
		link, err := detectResolvectlLink()
		if err != nil {
			return nil, err
		}
		if out, err := exec.Command("resolvectl", "dns", link, "127.0.0.1").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("configure resolvectl dns %s: %w (%s)", link, err, string(out))
		}
		if out, err := exec.Command("resolvectl", "domain", link, "~mesh").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("configure resolvectl domain %s: %w (%s)", link, err, string(out))
		}
		return func() error {
			_, _ = exec.Command("resolvectl", "revert", link).CombinedOutput()
			return nil
		}, nil
	case "darwin":
		resolverDir := "/etc/resolver"
		if err := os.MkdirAll(resolverDir, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", resolverDir, err)
		}
		path := filepath.Join(resolverDir, "mesh")
		content := "nameserver 127.0.0.1\nport 53\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		return func() error {
			_ = os.Remove(path)
			return nil
		}, nil
	default:
		return nil, fmt.Errorf("split DNS not supported on %s", runtime.GOOS)
	}
}

func detectResolvectlLink() (string, error) {
	out, err := exec.Command("resolvectl", "status").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect resolvectl status: %w (%s)", err, string(out))
	}

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "Link ") {
			continue
		}
		start := strings.Index(line, "(")
		end := strings.Index(line, ")")
		if start < 0 || end <= start+1 {
			continue
		}
		link := strings.TrimSpace(line[start+1 : end])
		if link == "" || link == "lo" {
			continue
		}
		return link, nil
	}

	return "", fmt.Errorf("no non-loopback resolvectl link found")
}
