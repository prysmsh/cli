package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/warp-run/prysm-cli/internal/api"
)

func renderMeshNodes(nodes []api.MeshNode) {
	sort.Slice(nodes, func(i, j int) bool {
		return strings.Compare(nodes[i].DeviceID, nodes[j].DeviceID) < 0
	})

	fmt.Printf("%-24s %-10s %-12s %-19s %-8s\n", "DEVICE", "TYPE", "STATUS", "LAST PING", "EXIT")
	for _, node := range nodes {
		lastPing := "-"
		if node.LastPing != nil {
			lastPing = node.LastPing.Format(time.RFC3339)
		}

		exit := "-"
		if node.ExitEnabled {
			exit = fmt.Sprintf("prio:%d", node.ExitPriority)
		}

		fmt.Printf("%-24s %-10s %-12s %-19s %-8s\n",
			node.DeviceID,
			node.PeerType,
			node.Status,
			lastPing,
			exit,
		)
	}
}
