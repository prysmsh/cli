package exit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/prysmsh/cli/internal/util"
)

const exitStateFile = "exit-proxy.json"

// ExitState persists proxy metadata so `off` and `status` can find it.
type ExitState struct {
	PID        int       `json:"pid"`
	ExitPeer   string    `json:"exit_peer"`
	ListenAddr string    `json:"listen_addr"`
	StartedAt  time.Time `json:"started_at"`
}

func statePath() string {
	return filepath.Join(util.PrysmHome(), exitStateFile)
}

func writeExitState(st *ExitState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0o600)
}

func readExitState() (*ExitState, error) {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return nil, err
	}
	var st ExitState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func removeExitState() {
	_ = os.Remove(statePath())
}
