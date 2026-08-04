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
	Short: "Upgrade to Pro for unlimited agents",
	Long: `The free plan is limited to 1 deployed agent, CLI-only (the web
dashboard requires Pro). This opens a real Stripe Checkout session for the
$29/mo Pro plan — unlimited agents, plus full web dashboard access.

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
	resp, err := client.CreateCheckoutSession()
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
