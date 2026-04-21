package cmd

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prysmsh/cli/internal/style"
)

// parseHTTPRequestLine extracts METHOD and PATH from the first line of what
// looks like an HTTP/1.x request. Returns ok=false for non-HTTP data.
func parseHTTPRequestLine(data []byte) (method, path string, ok bool) {
	end := bytes.Index(data, []byte("\r\n"))
	if end < 0 || end > 2048 {
		return "", "", false
	}
	parts := strings.SplitN(string(data[:end]), " ", 3)
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "HTTP/") {
		return "", "", false
	}
	m := parts[0]
	if len(m) == 0 || len(m) > 10 {
		return "", "", false
	}
	for _, r := range m {
		if r < 'A' || r > 'Z' {
			return "", "", false
		}
	}
	return m, parts[1], true
}

// parseHTTPStatusLine extracts the numeric status code from the first line of
// an HTTP/1.x response. Returns ok=false for non-HTTP data.
func parseHTTPStatusLine(data []byte) (status int, ok bool) {
	end := bytes.Index(data, []byte("\r\n"))
	if end < 0 || end > 512 {
		return 0, false
	}
	line := string(data[:end])
	if !strings.HasPrefix(line, "HTTP/") {
		return 0, false
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return 0, false
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil || code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

// formatHeartbeatAge renders the age of a heartbeat timestamp as a short
// human-readable duration ("12s", "2m", "1h") or "—" if unset.
func formatHeartbeatAge(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	age := time.Since(*t)
	if age < 0 {
		age = 0
	}
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}

// printTunnelRequest prints one request/response line to stdout in ngrok style.
func printTunnelRequest(method, path string, status int, dur time.Duration) {
	statusStr := fmt.Sprintf("%d", status)
	switch {
	case status >= 500:
		statusStr = style.Error.Render(statusStr)
	case status >= 400:
		statusStr = style.Warning.Render(statusStr)
	case status >= 300:
		statusStr = style.Info.Render(statusStr)
	default:
		statusStr = style.Success.Render(statusStr)
	}
	fmt.Printf("  %s  %-6s %s  %s\n",
		statusStr,
		method,
		path,
		style.MutedStyle.Render(dur.Round(time.Millisecond).String()),
	)
}
