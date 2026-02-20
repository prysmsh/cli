package api

import (
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	client := NewClient("https://api.example.com/api/v1")
	got := client.buildURL("tunnels", "1")
	if got == "" {
		t.Fatal("buildURL returned empty")
	}
	if !strings.Contains(got, "tunnels") || !strings.Contains(got, "1") {
		t.Errorf("buildURL() = %q, want path to include tunnels/1", got)
	}
}
