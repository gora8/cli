package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var walletAgentID string
var withdrawAmount string
var withdrawTo string
var exportYear string
var exportOut string
var fundAgentID string
var fundNoOpen bool

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Manage agent wallets",
	Long: `View balances, transaction history, and withdraw funds from agent wallets.

Each deployed agent has an attached x402-compatible wallet. Funds arrive
automatically when counterparties pay for capability invocations.

Examples:
  gora8 wallet show                          # Show all wallet balances
  gora8 wallet show --agent agt_abc123       # Show one agent's wallet
  gora8 wallet transactions --agent agt_abc123
  gora8 wallet fund --agent agt_abc123       # Buy USDC with a card
  gora8 wallet withdraw --agent agt_abc123 --amount 50.00 --to 0x...`,
}

var walletShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show wallet address and balance",
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

		if walletAgentID != "" {
			// Single agent wallet.
			w, err := client.GetWallet(walletAgentID)
			if err != nil {
				return err
			}
			ui.Header("Wallet — " + w.AgentName)
			fmt.Printf("  %-16s %s\n", ui.Dim("Address"), w.Address)
			fmt.Printf("  %-16s %s %s\n", ui.Dim("Balance"), ui.Bold(fmt.Sprintf("%.2f", w.Balance)), w.Currency)
			fmt.Printf("  %-16s %s %s\n", ui.Dim("Pending"), fmt.Sprintf("%.2f", w.Pending), w.Currency)
			fmt.Printf("  %-16s %s\n", ui.Dim("Network"), w.Network)
			return nil
		}

		// All wallets.
		wallets, err := client.ListWallets()
		if err != nil {
			return err
		}
		if len(wallets) == 0 {
			ui.Info("No wallets found. Deploy an agent first: gora8 deploy")
			return nil
		}

		ui.Header("Wallets")
		rows := make([][]string, 0, len(wallets))
		for _, w := range wallets {
			address := w.Address
			if len(w.Address) > 16 {
				address = w.Address[:10] + "..." + w.Address[len(w.Address)-6:]
			}
			rows = append(rows, []string{
				w.AgentName,
				address,
				fmt.Sprintf("%.2f %s", w.Balance, w.Currency),
				fmt.Sprintf("%.2f %s", w.Pending, w.Currency),
				w.Network,
			})
		}
		ui.Table([]string{"Agent", "Address", "Balance", "Pending", "Network"}, rows)
		return nil
	},
}

var walletTransactionsCmd = &cobra.Command{
	Use:   "transactions",
	Short: "Show incoming payment history",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}
		if walletAgentID == "" {
			ui.Error("Specify an agent: --agent <agent-id>")
			return nil
		}

		client := api.New(cfg)
		txns, err := client.ListWalletTransactions(walletAgentID)
		if err != nil {
			return err
		}
		if len(txns) == 0 {
			ui.Info("No transactions yet. Your wallet is ready to receive payments.")
			return nil
		}

		ui.Header("Wallet Transactions")
		rows := make([][]string, 0, len(txns))
		for _, t := range txns {
			direction := "→ in"
			if t.Direction == "out" {
				direction = "← out"
			}
			if t.CounterpartyAgentName != "" {
				if t.Direction == "out" {
					direction = "← hired"
				} else {
					direction = "→ hired by"
				}
			}
			counterparty := t.Counterparty
			if t.CounterpartyAgentName != "" {
				counterparty = t.CounterpartyAgentName
			}
			txHash := "—"
			if len(t.TxHash) >= 8 {
				txHash = t.TxHash[:8] + "..."
			} else if t.TxHash != "" {
				txHash = t.TxHash
			}
			rows = append(rows, []string{
				t.Timestamp,
				direction,
				fmt.Sprintf("%.4f %s", t.Amount, t.Currency),
				counterparty,
				t.Capability,
				txHash,
			})
		}
		ui.Table([]string{"Time", "Dir", "Amount", "From", "Capability", "Tx"}, rows)
		return nil
	},
}

var walletWithdrawCmd = &cobra.Command{
	Use:   "withdraw",
	Short: "Withdraw funds to an external address",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}
		if walletAgentID == "" {
			ui.Error("Specify an agent: --agent <agent-id>")
			return nil
		}
		if withdrawAmount == "" {
			ui.Error("Specify an amount: --amount <value>")
			return nil
		}
		if withdrawTo == "" {
			ui.Error("Specify a destination address: --to <address>")
			return nil
		}

		client := api.New(cfg)
		spin := ui.NewSpinner(fmt.Sprintf("Initiating withdrawal of %s to %s...", withdrawAmount, withdrawTo))
		spin.Start()

		result, err := client.WithdrawFunds(walletAgentID, withdrawAmount, withdrawTo)
		if err != nil {
			spin.Fail("Withdrawal failed")
			return err
		}
		spin.Stop("Withdrawal initiated")

		fmt.Println()
		fmt.Printf("  %-14s %s\n", ui.Dim("Amount"), result.Amount+" "+result.Currency)
		fmt.Printf("  %-14s %s\n", ui.Dim("To"), result.ToAddress)
		fmt.Printf("  %-14s %s\n", ui.Dim("Tx hash"), result.TxHash)
		fmt.Printf("  %-14s %s\n", ui.Dim("Status"), ui.StatusColor(result.Status))
		fmt.Println()
		ui.Info("Funds typically settle in 2–5 minutes depending on network conditions.")
		return nil
	},
}

var walletFundCmd = &cobra.Command{
	Use:   "fund",
	Short: "Fund an agent's wallet with a card via Coinbase Onramp",
	Long: `Buy USDC with a card and have it delivered straight to an agent's own
wallet — gora8 never touches your card details or the funds; Coinbase
handles the purchase and KYC. Opens a Coinbase-hosted checkout page in your
browser, then waits for the balance to arrive.

Examples:
  gora8 wallet fund --agent agt_abc123
  gora8 wallet fund --agent agt_abc123 --no-open   # just print the link`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if !cfg.IsAuthenticated() {
			ui.Error("Not authenticated. Run: gora8 auth login")
			return nil
		}
		if fundAgentID == "" {
			ui.Error("Specify an agent: --agent <agent-id>")
			return nil
		}

		client := api.New(cfg)

		before, err := client.GetWallet(fundAgentID)
		if err != nil {
			return err
		}

		session, err := client.CreateOnrampSession(fundAgentID)
		if err != nil {
			return err
		}

		if fundNoOpen || !openBrowser(session.URL) {
			ui.Info("Open this link to buy USDC with a card:")
			fmt.Println("  " + ui.Bold(session.URL))
		} else {
			ui.Info("Opened your browser to complete the purchase.")
			fmt.Println("  " + ui.Dim(session.URL))
		}

		fmt.Println()
		spin := ui.NewSpinner("Waiting for funds to arrive...")
		spin.Start()
		deadline := time.Now().Add(15 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(5 * time.Second)
			after, err := client.GetWallet(fundAgentID)
			if err != nil {
				continue // transient — keep polling
			}
			if after.Balance > before.Balance {
				spin.Stop("Funds received")
				fmt.Println()
				fmt.Printf("  %-14s %s\n", ui.Dim("New balance"), fmt.Sprintf("%.2f %s", after.Balance, after.Currency))
				return nil
			}
		}
		spin.Stop("")
		ui.Info("Still waiting? A purchase can take a few minutes to settle — check back with:")
		fmt.Println("  " + ui.Bold("gora8 wallet show --agent "+fundAgentID))
		return nil
	},
}

var walletExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export transaction history as CSV",
	Long: `Export every earnings and withdrawal transaction as CSV — for tax and
accounting records. Writes to stdout by default so it composes with shell
redirection; use --out to write to a file directly.

Examples:
  gora8 wallet export > all-time.csv
  gora8 wallet export --year 2026 --out 2026-taxes.csv
  gora8 wallet export --agent agt_abc123 --year 2026`,
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
		var csv []byte
		if exportOut == "" {
			// Writing status/progress to stdout would corrupt piped CSV
			// output (e.g. `gora8 wallet export > file.csv`), so stay
			// silent unless something goes wrong.
			csv, err = client.ExportWalletCSV(walletAgentID, exportYear)
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(csv)
			return err
		}

		spin := ui.NewSpinner("Exporting...")
		spin.Start()
		csv, err = client.ExportWalletCSV(walletAgentID, exportYear)
		if err != nil {
			spin.Fail("Export failed")
			return err
		}
		if err := os.WriteFile(exportOut, csv, 0o644); err != nil {
			spin.Fail("Failed to write file")
			return fmt.Errorf("write %s: %w", exportOut, err)
		}
		spin.Stop(fmt.Sprintf("Exported to %s", ui.Bold(exportOut)))
		return nil
	},
}

func init() {
	walletShowCmd.Flags().StringVar(&walletAgentID, "agent", "", "Agent ID (omit to show all)")
	walletTransactionsCmd.Flags().StringVar(&walletAgentID, "agent", "", "Agent ID")
	walletWithdrawCmd.Flags().StringVar(&walletAgentID, "agent", "", "Agent ID")
	walletWithdrawCmd.Flags().StringVar(&withdrawAmount, "amount", "", "Amount to withdraw (e.g. 50.00)")
	walletWithdrawCmd.Flags().StringVar(&withdrawTo, "to", "", "Destination wallet address")
	walletExportCmd.Flags().StringVar(&walletAgentID, "agent", "", "Agent ID (omit to export every agent you own)")
	walletExportCmd.Flags().StringVar(&exportYear, "year", "", "Calendar year to export, e.g. 2026 (omit for all-time)")
	walletExportCmd.Flags().StringVar(&exportOut, "out", "", "Write to this file instead of stdout")
	walletFundCmd.Flags().StringVar(&fundAgentID, "agent", "", "Agent ID")
	walletFundCmd.Flags().BoolVar(&fundNoOpen, "no-open", false, "Print the checkout link instead of opening a browser")

	walletCmd.AddCommand(walletShowCmd, walletTransactionsCmd, walletWithdrawCmd, walletFundCmd, walletExportCmd)
}
