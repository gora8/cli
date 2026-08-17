package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var mandateCmd = &cobra.Command{
	Use:   "mandate <agent-id>",
	Short: "Fetch and display an agent's current spending Mandate",
	Long: `Fetch an agent's Mandate — the signed document representing its current
spending authority (see github.com/gora8/protocol).

This is not the same thing as 'gora8 policy', even though both describe
spending limits: policy is what you set; the Mandate is the signed,
portable proof of it a counterparty can verify independently, without
trusting gora8's word or your own agent's claims about itself. That
independent verifiability is the actual point — a budget check inside
your agent's own code is a promise it makes to itself; a Mandate is a
claim anyone can check.

The endpoint this command calls is public and unauthenticated — anyone,
on gora8 or not, can verify an agent's current spending authority before
dealing with it.

Example:
  gora8 mandate agt_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runMandate,
}

func runMandate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	mandate, err := client.GetMandate(args[0])
	if err != nil {
		return err
	}

	ui.Header("Mandate")
	fmt.Println()
	if status, ok := mandate["status"].(string); ok {
		if status == "active" {
			ui.Success("Status: " + status)
		} else {
			ui.Warning("Status: " + status)
		}
		fmt.Println()
	}
	ui.PrintJSON(mandate)
	fmt.Println()
	ui.Info("Verify this signature independently against the issuer key at")
	ui.Info("GET /.well-known/gora8-issuer-key — see github.com/gora8/protocol")
	ui.Info("for the canonicalization and verification algorithm.")
	return nil
}
