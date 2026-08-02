package cmd

import (
	"fmt"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	logsTail   int
	logsFollow bool
)

var logsCmd = &cobra.Command{
	Use:   "logs [agent-id]",
	Short: "View recent agent interactions",
	Long: `View recent interactions (log entries) for an agent.

Each entry shows the timestamp, counterparty, capability invoked, status,
duration, and amount charged.

Examples:
  gora8 logs agt_abc123
  gora8 logs agt_abc123 --tail 50
  gora8 logs agt_abc123 --follow`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	agentID := args[0]

	if logsFollow {
		return runLogsFollow(cfg, agentID)
	}
	return runLogsOnce(cfg, agentID, logsTail)
}

func runLogsOnce(cfg *config.Config, agentID string, tail int) error {
	spin := ui.NewSpinner("Fetching logs...")
	spin.Start()
	client := api.New(cfg)
	entries, err := client.GetLogs(agentID, tail)
	if err != nil {
		spin.Fail("Failed to fetch logs")
		return err
	}
	spin.Stop("")

	printLogEntries(agentID, entries)
	return nil
}

func runLogsFollow(cfg *config.Config, agentID string) error {
	ui.Info(fmt.Sprintf("Following logs for agent %s (Ctrl+C to stop)...", ui.Bold(agentID)))
	fmt.Println()

	client := api.New(cfg)
	seen := make(map[string]bool)

	for {
		entries, err := client.GetLogs(agentID, logsTail)
		if err != nil {
			ui.Warning(fmt.Sprintf("Poll error: %v", err))
		} else {
			for _, entry := range entries {
				if !seen[entry.ID] {
					seen[entry.ID] = true
					printSingleEntry(entry)
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func printLogEntries(agentID string, entries []api.LogEntry) {
	if len(entries) == 0 {
		ui.Info(fmt.Sprintf("No interactions found for agent %s.", agentID))
		return
	}

	ui.Header(fmt.Sprintf("Logs — %s (%d entries)", agentID, len(entries)))
	fmt.Println()
	for _, entry := range entries {
		printSingleEntry(entry)
	}
}

func printSingleEntry(entry api.LogEntry) {
	ts := entry.Timestamp
	if ts == "" {
		ts = "—"
	}

	statusStr := ui.StatusColor(entry.Status)
	durationStr := ""
	if entry.Duration > 0 {
		durationStr = fmt.Sprintf(" %s", ui.Dim(fmt.Sprintf("%dms", entry.Duration)))
	}

	amountStr := ""
	if entry.Amount > 0 {
		cur := entry.Currency
		if cur == "" {
			cur = "USD"
		}
		amountStr = fmt.Sprintf(" %s", ui.Green(fmt.Sprintf("$%.4f", entry.Amount)))
	}

	counterparty := entry.Counterparty
	if counterparty == "" {
		counterparty = "unknown"
	}

	fmt.Printf("  %s  %s  %s → %s%s%s\n",
		ui.Dim(ts),
		statusStr,
		ui.Cyan(entry.Capability),
		counterparty,
		durationStr,
		amountStr,
	)
	if entry.Summary != "" {
		fmt.Printf("    %s\n", ui.Dim(entry.Summary))
	}
}

func init() {
	logsCmd.Flags().IntVar(&logsTail, "tail", 20, "Number of recent log entries to show")
	logsCmd.Flags().BoolVar(&logsFollow, "follow", false, "Poll for new logs every 2 seconds")
}
