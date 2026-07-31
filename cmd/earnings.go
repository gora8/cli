package cmd

import (
	"fmt"

	"github.com/agentplane/cli/internal/api"
	"github.com/agentplane/cli/internal/config"
	"github.com/agentplane/cli/internal/ui"
	"github.com/spf13/cobra"
)

var earningsPeriod string
var earningsJSON bool

var earningsCmd = &cobra.Command{
	Use:   "earnings [agent-id]",
	Short: "View agent earnings",
	Long: `View earnings for one or all agents.

Examples:
  agentctl earnings                   # All agents
  agentctl earnings agt_abc123        # Specific agent
  agentctl earnings --period 7d       # Last 7 days
  agentctl earnings --period 30d      # Last 30 days (default)
  agentctl earnings --period all      # All time`,
	Args: cobra.MaximumNArgs(1),
	RunE: runEarnings,
}

func runEarnings(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	var agentID string
	if len(args) > 0 {
		agentID = args[0]
	}

	spin := ui.NewSpinner("Fetching earnings...")
	spin.Start()
	client := api.New(cfg)
	resp, err := client.GetEarnings(agentID, earningsPeriod)
	if err != nil {
		spin.Fail("Failed to fetch earnings")
		return err
	}
	spin.Stop("")

	if earningsJSON {
		return ui.PrintJSON(resp)
	}

	currency := resp.Currency
	if currency == "" {
		currency = "USD"
	}

	// Header.
	title := "Earnings — All Agents"
	if resp.AgentName != "" {
		title = fmt.Sprintf("Earnings — %s", resp.AgentName)
	}
	ui.Header(title)

	// Summary line.
	fmt.Printf("\n  %s  %s %s\n", ui.Bold("Total"), ui.Green(fmt.Sprintf("$%.4f", resp.Total)), ui.Dim(currency))
	fmt.Printf("  %s  %d\n\n", ui.Bold("Transactions"), resp.Transactions)

	// Per-period breakdown table.
	if len(resp.Periods) > 0 {
		headers := []string{"PERIOD", "EARNINGS", "TRANSACTIONS"}
		rows := make([][]string, 0, len(resp.Periods))
		for _, p := range resp.Periods {
			rows = append(rows, []string{
				p.Label,
				fmt.Sprintf("$%.4f %s", p.Amount, currency),
				fmt.Sprintf("%d", p.Transactions),
			})
		}
		ui.Table(headers, rows)
	}
	return nil
}

func init() {
	earningsCmd.Flags().StringVar(&earningsPeriod, "period", "30d",
		"Time period: 7d, 30d, 90d, all")
	earningsCmd.Flags().BoolVar(&earningsJSON, "json", false, "Output as JSON")
}
