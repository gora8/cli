package cmd

import (
	"fmt"
	"runtime"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

// Build info — injected via ldflags at build time.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show agentctl version information",
	Run:   runVersion,
}

func runVersion(cmd *cobra.Command, args []string) {
	// Keep api.Version in sync so it appears in User-Agent headers.
	api.Version = Version

	ui.Header("agentctl — gora8 CLI")
	fmt.Println()

	rows := [][]string{
		{"Version", Version},
		{"Commit", Commit},
		{"Build Date", BuildDate},
		{"Go Version", runtime.Version()},
		{"OS/Arch", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)},
	}

	for _, row := range rows {
		fmt.Printf("  %s  %s\n", ui.Dim(fmt.Sprintf("%-12s", row[0])), row[1])
	}

	fmt.Println()
	fmt.Printf("  %s\n", ui.Dim("https://gora8.com  ·  docs: https://gora8.com/docs"))
}
