//go:build windows

package cmd

import (
	"fmt"
	"net"
)

func startMeshSplitDNS(_ map[string]net.IP) (func(), error) {
	return nil, fmt.Errorf("split DNS not supported on windows")
}
