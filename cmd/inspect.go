package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <agent-id>",
	Short: "One-screen view of how an agent is actually doing",
	Long: `Pulls together everything you'd normally check across several commands —
success rate, latency, revenue, error rate, wallet balance, top callers, and
capability usage — into a single view. Meant to be something you check daily,
not just at deploy time.`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}

func fmtPercent(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", *p*100)
}

func fmtMs(ms *int) string {
	if ms == nil {
		return "—"
	}
	return fmt.Sprintf("%dms", *ms)
}

func fmtUSD(amount *float64) string {
	if amount == nil {
		return "—"
	}
	return fmt.Sprintf("$%.4f", *amount)
}

func runInspect(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	spin := ui.NewSpinner("Gathering stats...")
	spin.Start()
	r, err := client.InspectAgent(args[0])
	if err != nil {
		spin.Fail("Failed to inspect agent")
		return err
	}
	spin.Stop("")

	ui.Header(r.AgentName)
	fmt.Printf("  %-20s %s\n", ui.Dim("Status"), ui.StatusColor(r.Status))
	fmt.Println()

	fmt.Println(ui.Bold("Performance (30d)"))
	fmt.Printf("  %-20s %s\n", ui.Dim("Success rate"), fmtPercent(r.SuccessRate))
	fmt.Printf("  %-20s %s\n", ui.Dim("Error rate"), fmtPercent(r.ErrorRate))
	fmt.Printf("  %-20s %s\n", ui.Dim("Avg latency"), fmtMs(r.AvgResponseMs))
	fmt.Printf("  %-20s %d\n", ui.Dim("Total calls"), r.TotalCalls)
	fmt.Println()

	fmt.Println(ui.Bold("Revenue"))
	fmt.Printf("  %-20s $%.2f\n", ui.Dim("Total earnings"), r.EarningsTotal)
	fmt.Printf("  %-20s %s\n", ui.Dim("Avg revenue/call"), fmtUSD(r.AvgRevenuePerCall))
	fmt.Printf("  %-20s $%.2f\n", ui.Dim("Wallet balance"), r.WalletBalance)
	fmt.Printf("  %-20s $%.2f\n", ui.Dim("Staked collateral"), r.WalletStaked)
	fmt.Println()

	if r.OpenDisputes > 0 {
		ui.Warning(fmt.Sprintf("%d open dispute(s) — run: gora8 policy %s", r.OpenDisputes, r.AgentID))
		fmt.Println()
	}

	if len(r.TopCallers30d) > 0 {
		fmt.Println(ui.Bold("Top callers (30d)"))
		rows := make([][]string, 0, len(r.TopCallers30d))
		for _, c := range r.TopCallers30d {
			label := c.Counterparty
			if c.Name != nil {
				label = *c.Name
			}
			rows = append(rows, []string{label, fmt.Sprintf("%d", c.Calls), fmt.Sprintf("$%.2f", c.TotalPaid)})
		}
		ui.Table([]string{"CALLER", "CALLS", "PAID"}, rows)
		fmt.Println()
	}

	if len(r.TopCapabilities30d) > 0 {
		fmt.Println(ui.Bold("Top capabilities (30d)"))
		rows := make([][]string, 0, len(r.TopCapabilities30d))
		for _, c := range r.TopCapabilities30d {
			rows = append(rows, []string{c.Capability, fmt.Sprintf("%d", c.Calls)})
		}
		ui.Table([]string{"CAPABILITY", "CALLS"}, rows)
	}

	return nil
}
