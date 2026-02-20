package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/warp-run/prysm-cli/internal/api"
)

// meshPeerRow represents a single row in the mesh peers table (mesh node or cluster).
type meshPeerRow struct {
	DeviceID string
	PeerType string
	Status   string
	LastPing string
	Exit     string
}

func renderMeshNodes(nodes []api.MeshNode) {
	renderMeshPeerRows(meshNodesToRows(nodes))
}

func meshNodesToRows(nodes []api.MeshNode) []meshPeerRow {
	rows := make([]meshPeerRow, 0, len(nodes))
	for _, node := range nodes {
		lastPing := "-"
		if node.LastPing != nil {
			lastPing = node.LastPing.Format(time.RFC3339)
		}
		exit := "-"
		if node.ExitEnabled {
			exit = fmt.Sprintf("prio:%d", node.ExitPriority)
		}
		rows = append(rows, meshPeerRow{
			DeviceID: node.DeviceID,
			PeerType: node.PeerType,
			Status:   node.Status,
			LastPing: lastPing,
			Exit:     exit,
		})
	}
	return rows
}

func renderMeshPeerRows(rows []meshPeerRow) {
	sort.Slice(rows, func(i, j int) bool {
		return strings.Compare(rows[i].DeviceID, rows[j].DeviceID) < 0
	})

	fmt.Printf("%-24s %-10s %-12s %-19s %-8s\n", "DEVICE", "TYPE", "STATUS", "LAST PING", "EXIT")
	for _, row := range rows {
		fmt.Printf("%-24s %-10s %-12s %-19s %-8s\n",
			truncate(row.DeviceID, 24),
			row.PeerType,
			row.Status,
			row.LastPing,
			row.Exit,
		)
	}
}
