package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade to Production for unlimited agents",
	Long: `Builder is limited to 1 deployed agent, CLI-only (the web dashboard
requires Production or above). This opens a real Stripe Checkout session
for Production — unlimited agents, plus full web dashboard access,
priced on economically consequential decisions rather than seats.

Already on the older flat $29/mo Pro plan? Nothing changes for you —
this command is for new upgrades only.

The CLI itself never sees your payment details; Stripe hosts the checkout
page directly.`,
	Args: cobra.NoArgs,
	RunE: runUpgrade,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	spin := ui.NewSpinner("Creating checkout session...")
	spin.Start()
	resp, err := client.CreateTierCheckoutSession("production")
	if err != nil {
		spin.Fail("Could not create checkout session")
		return err
	}
	spin.Stop("")

	fmt.Println()
	ui.Success("Open this link to complete your upgrade:")
	fmt.Println(ui.Bold(resp.URL))
	fmt.Println()
	fmt.Println(ui.Dim("Your plan updates automatically once payment completes — no need to run anything else."))
	return nil
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
