package main

import (
	"os"

	"github.com/agentplane/cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
