package cmd

import (
	"encoding/json"
	"os"
	"strings"
)

func wantsJSONOutput(flagValue string) bool {
	flagValue = strings.TrimSpace(strings.ToLower(flagValue))
	switch flagValue {
	case "json":
		return true
	case "table", "text":
		return false
	}

	if app != nil && strings.EqualFold(strings.TrimSpace(app.OutputFormat), "json") {
		return true
	}
	return false
}

func writeJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
