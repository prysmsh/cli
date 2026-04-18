package main

import (
	"fmt"
	"os"

	"github.com/prysmsh/cli/internal/cmd"
	"github.com/prysmsh/cli/internal/style"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, style.Error.Render("Error: "+err.Error()))
		os.Exit(1)
	}
}
