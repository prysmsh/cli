//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/prysmsh/cli/internal/api"
)

func main() {
	raw, _ := os.ReadFile(os.Getenv("HOME") + "/.prysm/session.json")
	var s struct {
		Token string `json:"token"`
	}
	json.Unmarshal(raw, &s)

	c := api.NewClient("https://api.prysm.sh/api/v1", api.WithDebug(true))
	c.SetToken(s.Token)

	var result json.RawMessage
	resp, err := c.Do(context.Background(), "POST", "/tokens", map[string]interface{}{
		"name":        "debug-test-3",
		"permissions": []string{"*"},
	}, &result)
	fmt.Printf("err: %v\n", err)
	if resp != nil {
		fmt.Printf("status: %d\n", resp.StatusCode)
	}
	fmt.Printf("raw result: %s\n", string(result))

	var tokenResp struct {
		Token struct {
			ID    uint   `json:"id"`
			Token string `json:"token"`
		} `json:"token"`
	}
	if err := json.Unmarshal(result, &tokenResp); err != nil {
		fmt.Printf("UNMARSHAL ERROR: %v\n", err)
	} else {
		fmt.Printf("parsed token: %q\n", tokenResp.Token.Token)
	}
}
