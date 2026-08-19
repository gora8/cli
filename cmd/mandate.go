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
spending authority.

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
	ui.Info("GET /.well-known/gora8-issuer-key.")
	if onChain, ok := mandate["onChain"].(map[string]interface{}); ok {
		fmt.Println()
		if issued, _ := onChain["issued"].(bool); issued {
			active, _ := onChain["active"].(bool)
			if active {
				ui.Success("On-chain: issued and active on AuthorityRegistry.")
			} else {
				ui.Warning("On-chain: issued but REVOKED on AuthorityRegistry.")
			}
		} else {
			ui.Info("On-chain: not yet issued. Run `gora8 mandate issue-onchain " + args[0] + "`.")
		}
	}
	return nil
}

var mandateIssueOnChainCmd = &cobra.Command{
	Use:   "issue-onchain <agent-id>",
	Short: "Resync this agent's current Mandate on AuthorityRegistry (Base Sepolia testnet)",
	Long: `Resyncs the agent's current Mandate on-chain, on AuthorityRegistry
(Base Sepolia testnet), and re-points its wallet at it if needed.

'gora8 deploy' and 'gora8 policy set' already do this automatically,
best-effort — you don't need to run this as part of normal use. It's here
as an explicit, loud retry for the rare case that automatic sync failed
(e.g. a transient RPC hiccup): this surfaces the real error instead of
failing silently the way the deploy/policy call's own best-effort attempt
does.

This doesn't replace the signed Mandate document ('gora8 mandate') — it
adds a second, on-chain-checkable fact: whether this exact Mandate
(identified by a hash of the agent's id and current policy) has been
issued and, if so, whether it's since been revoked — checkable in one
contract call by any third party, independent of gora8's API.

Example:
  gora8 mandate issue-onchain agt_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runMandateIssueOnChain,
}

func runMandateIssueOnChain(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	spin := ui.NewSpinner("Issuing Mandate on AuthorityRegistry...")
	spin.Start()
	result, err := client.IssueMandateOnChain(args[0])
	if err != nil {
		spin.Fail("Issuance failed")
		return err
	}

	if result.Status == "already-issued" {
		spin.Stop("This Mandate was already issued on-chain")
	} else {
		spin.Stop("Mandate issued on-chain")
	}

	fmt.Println()
	ui.Info("Mandate ID: " + result.MandateID)
	if result.Enforcement != nil {
		if result.Enforcement.Delegated {
			ui.Success("Enforcement: active")
			if result.Enforcement.TxHash != "" {
				ui.Info("Tx hash:     " + result.Enforcement.TxHash)
			}
		} else {
			ui.Warning("Enforcement: not active — " + result.Enforcement.Error)
		}
	}
	return nil
}

func init() {
	mandateCmd.AddCommand(mandateIssueOnChainCmd)
}
