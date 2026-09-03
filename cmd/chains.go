package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var chainsCmd = &cobra.Command{
	Use:   "chains",
	Short: "See supported chains and opt an agent into one",
	Long: `gora8 deploy already rolls an agent out to every chain in the
auto-rollout list below — nothing to do for those. This command is for
the chains that AREN'T automatic: today, only Ethereum mainnet, whose
gas cost is high enough (often $5-50+ per registration) that it's never
included in an automatic multi-chain deploy on any plan — you opt in
explicitly, with the cost visible before you do.`,
	RunE: runChainsList, // default sub-command
}

var chainsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every chain gora8 supports",
	RunE:  runChainsList,
}

func runChainsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client := api.New(cfg)
	chains, aar, err := client.ListChainCapabilities()
	if err != nil {
		return err
	}

	if cfg.IsAuthenticated() {
		if budget, err := client.GetGasBudget(); err == nil && budget.BudgetUSD > 0 {
			ui.Info(fmt.Sprintf("Sponsored gas this period: $%.2f spent / $%.2f budget (Pro only — Free self-funds via `gora8 wallet fund`).", budget.SpentUSD, budget.BudgetUSD))
			fmt.Println()
		}
	}

	// Six-capability matrix (gap-closure plan Stage 5/9) replacing the
	// old flat "N chains supported" table — Identity/Discovery/Payment
	// genuinely vary per chain; Authority/Agreement/Resolution don't (see
	// api/src/lib/chain-capabilities.ts's doc comment), so they're
	// printed once below instead of as three more per-row columns that
	// would say the same thing every time.
	ui.Header("Supported chains")
	headers := []string{"CHAIN", "CAIP-2", "IDENTITY", "DISCOVERY", "PAYMENT"}
	rows := make([][]string, 0, len(chains))
	for _, c := range chains {
		identity := c.Identity
		if !c.AutoRollout {
			identity = identity + " (gora8 chains add <agent-id> " + c.Caip2 + ")"
		}
		rows = append(rows, []string{c.Name, c.Caip2, identity, c.Discovery, c.Payment})
	}
	ui.Table(headers, rows)

	if aar != nil {
		fmt.Println()
		ui.Info(fmt.Sprintf("Authority/Agreement/Resolution (gora8's own contracts): %s — %s", aar.Status, aar.Note))
	}
	return nil
}

var chainsAddCmd = &cobra.Command{
	Use:   "add <agent-id> <chain>",
	Short: "Explicitly activate an agent on a chain outside the automatic rollout",
	Long: `Registers the agent's ERC-8004 identity (and Mandate, once gora8's own
contracts are live there) on a chain that isn't part of the automatic
'gora8 deploy' rollout — today that's only Ethereum mainnet.

The agent's own wallet must already hold that chain's native gas unless
you're on the Pro plan (gora8 sponsors it there, within your plan's
allowance). Fund a Free-plan agent's wallet first — see 'gora8 wallet fund'.

Example:
  gora8 chains add agt_abc123 eip155:1`,
	Args: cobra.ExactArgs(2),
	RunE: runChainsAdd,
}

func runChainsAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	agentID, chain := args[0], args[1]
	spin := ui.NewSpinner(fmt.Sprintf("Activating %s on %s...", agentID, chain))
	spin.Start()
	client := api.New(cfg)
	result, err := client.ActivateChain(agentID, chain)
	if err != nil {
		spin.Fail("Activation failed")
		return err
	}

	switch result.Status {
	case "active":
		spin.Stop(fmt.Sprintf("Registered on %s (actor id %s)", result.Chain, result.ActorID))
		if result.MandateEnforced {
			ui.Success("Mandate issued and enforced on-chain there too.")
		} else {
			ui.Info("Identity registered — Mandate enforcement isn't live on this chain yet.")
		}
	case "awaiting_gas":
		spin.Fail(fmt.Sprintf("Agent wallet has no native gas on %s yet.", result.Chain))
		ui.Info("Fund it — see 'gora8 wallet fund' — then run this command again.")
	default:
		spin.Fail(fmt.Sprintf("Activation failed on %s: %s", result.Chain, result.Error))
	}
	return nil
}

func init() {
	chainsCmd.AddCommand(chainsListCmd, chainsAddCmd)
}
