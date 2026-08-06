package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var identityAgentID string
var identityDID string

var identityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Manage agent DID identities",
	Long: `View and manage the decentralized identifiers (DIDs) for your agents.

Each deployed agent gets a did:web identity — a cryptographically verifiable
identifier that any counterparty can resolve offline. The DID document contains
the agent's public key, endpoint, and capability declarations.

Examples:
  gora8 identity show --agent agt_abc123     # Show DID and document
  gora8 identity verify did:web:acme.com:legal-research
  gora8 identity rotate --agent agt_abc123   # Rotate signing keys`,
}

var identityShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show agent DID and did:web document",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}
		if identityAgentID == "" {
			ui.Error("Specify an agent: --agent <agent-id>")
			return nil
		}

		client := api.New(cfg)
		identity, err := client.GetIdentity(identityAgentID)
		if err != nil {
			return err
		}

		ui.Header("Identity — " + identity.AgentName)
		fmt.Println()
		fmt.Printf("  %-18s %s\n", ui.Dim("DID"), ui.Bold(identity.DID))
		fmt.Printf("  %-18s %s\n", ui.Dim("Method"), identity.Method)
		fmt.Printf("  %-18s %s\n", ui.Dim("Document URL"), identity.DocumentURL)
		fmt.Printf("  %-18s %s\n", ui.Dim("Key type"), identity.KeyType)
		fmt.Printf("  %-18s %s\n", ui.Dim("Key ID"), identity.KeyID)
		fmt.Printf("  %-18s %s\n", ui.Dim("Created"), identity.CreatedAt)
		fmt.Printf("  %-18s %s\n", ui.Dim("Last rotated"), identity.LastRotated)
		fmt.Println()

		fmt.Println(ui.Dim("DID Document:"))
		fmt.Println()
		ui.PrintJSON(identity.Document)
		return nil
	},
}

var identityVerifyCmd = &cobra.Command{
	Use:   "verify <did>",
	Short: "Resolve and verify a DID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}

		did := args[0]
		client := api.New(cfg)

		spin := ui.NewSpinner(fmt.Sprintf("Resolving %s...", did))
		spin.Start()

		result, err := client.VerifyDID(did)
		if err != nil {
			spin.Fail("Resolution failed")
			return err
		}
		spin.Stop("DID resolved")

		fmt.Println()
		if result.Valid {
			ui.Success("DID is valid and verifiable")
		} else {
			ui.Error("DID verification failed: " + result.Error)
		}
		fmt.Println()
		fmt.Printf("  %-18s %s\n", ui.Dim("DID"), did)
		fmt.Printf("  %-18s %s\n", ui.Dim("Method"), result.Method)
		fmt.Printf("  %-18s %s\n", ui.Dim("Endpoint"), result.Endpoint)
		fmt.Printf("  %-18s %s\n", ui.Dim("Key type"), result.KeyType)
		if len(result.Capabilities) > 0 {
			fmt.Printf("  %-18s", ui.Dim("Capabilities"))
			for i, cap := range result.Capabilities {
				if i == 0 {
					fmt.Printf(" %s\n", cap)
				} else {
					fmt.Printf("  %-18s %s\n", "", cap)
				}
			}
		}
		return nil
	},
}

var identityRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate the agent's signing keys",
	Long: `Generate a new Ed25519 keypair for the agent and update the DID document.

The old key is kept valid for 24 hours to allow in-flight delegations to complete.
After rotation, the new key is used for all new transaction signatures.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}
		if identityAgentID == "" {
			ui.Error("Specify an agent: --agent <agent-id>")
			return nil
		}

		client := api.New(cfg)

		ui.Warning("Key rotation will invalidate existing delegation chains signed with the current key.")
		ui.Warning("In-flight transactions may fail. The old key stays valid for 24 hours.")
		fmt.Println()

		spin := ui.NewSpinner("Rotating keys...")
		spin.Start()

		result, err := client.RotateKeys(identityAgentID)
		if err != nil {
			spin.Fail("Key rotation failed")
			return err
		}
		spin.Stop("Keys rotated")

		fmt.Println()
		fmt.Printf("  %-18s %s\n", ui.Dim("New key ID"), result.NewKeyID)
		fmt.Printf("  %-18s %s\n", ui.Dim("Old key expires"), result.OldKeyExpiry)
		fmt.Printf("  %-18s %s\n", ui.Dim("DID document"), result.DocumentURL)
		fmt.Println()
		ui.Success("DID document updated. New key is active immediately.")
		return nil
	},
}

var identityPassportCmd = &cobra.Command{
	Use:   "passport <agent-id>",
	Short: "Fetch an agent's signed Agent Passport",
	Long: `Fetch an agent's Agent Passport — a gora8-signed snapshot of its identity,
locked collateral, and dispute history.

This is a real, verifiable signature (see GET /.well-known/gora8-issuer-key
for the public key), and it's a public endpoint — anyone can fetch and
verify a passport, on gora8 or not, without needing an account here. A
valid signature proves gora8 issued this document and it wasn't altered
after signing; it does not by itself prove gora8 is a neutral authority —
see the Trust & identity section of gora8's roadmap for that distinction.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}

		client := api.New(cfg)
		passport, err := client.GetPassport(args[0])
		if err != nil {
			return err
		}
		ui.Header("Agent Passport")
		ui.PrintJSON(passport)
		return nil
	},
}

func init() {
	identityShowCmd.Flags().StringVar(&identityAgentID, "agent", "", "Agent ID")
	identityRotateCmd.Flags().StringVar(&identityAgentID, "agent", "", "Agent ID")

	identityCmd.AddCommand(identityShowCmd, identityVerifyCmd, identityRotateCmd, identityPassportCmd)
}
