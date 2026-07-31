package cmd

import (
	"fmt"

	"github.com/agentplane/cli/internal/api"
	"github.com/agentplane/cli/internal/config"
	"github.com/agentplane/cli/internal/ui"
	"github.com/spf13/cobra"
)

var policyCmd = &cobra.Command{
	Use:   "policy [agent-id]",
	Short: "View or manage spending policy for an agent",
	Long: `View the current spending policy for an agent.
Use 'agentctl policy set' to update the policy.

Example:
  agentctl policy agt_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runPolicyGet,
}

// ── get (default) ─────────────────────────────────────────────────────────────

func runPolicyGet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	agentID := args[0]
	spin := ui.NewSpinner("Fetching policy...")
	spin.Start()
	client := api.New(cfg)
	resp, err := client.GetPolicy(agentID)
	if err != nil {
		spin.Fail("Failed to fetch policy")
		return err
	}
	spin.Stop("")

	ui.Header(fmt.Sprintf("Spending Policy — %s", agentID))
	p := resp.Policy
	currency := p.Currency
	if currency == "" {
		currency = "USD"
	}

	rows := [][]string{}
	if p.LimitPerTransaction != "" {
		rows = append(rows, []string{"Per Transaction", fmt.Sprintf("%s %s", p.LimitPerTransaction, currency)})
	}
	if p.LimitDaily != "" {
		rows = append(rows, []string{"Daily Limit", fmt.Sprintf("%s %s", p.LimitDaily, currency)})
	}
	if p.LimitMonthly != "" {
		rows = append(rows, []string{"Monthly Limit", fmt.Sprintf("%s %s", p.LimitMonthly, currency)})
	}
	if p.ApprovalAbove != "" {
		rows = append(rows, []string{"Require Approval Above", fmt.Sprintf("%s %s", p.ApprovalAbove, currency)})
	}
	rows = append(rows, []string{"Currency", currency})

	fmt.Println()
	for _, row := range rows {
		fmt.Printf("  %s  %s\n", ui.Dim(fmt.Sprintf("%-24s", row[0])), row[1])
	}
	fmt.Println()
	ui.Info(fmt.Sprintf("Update policy with: agentctl policy set %s --limit-per-tx <amount>", agentID))
	return nil
}

// ── set ───────────────────────────────────────────────────────────────────────

var (
	policyLimitPerTx    string
	policyLimitDaily    string
	policyLimitMonthly  string
	policyApprovalAbove string
	policyCurrency      string
)

var policySetCmd = &cobra.Command{
	Use:   "set [agent-id]",
	Short: "Update spending policy for an agent",
	Long: `Update the spending policy for an agent.

You can set limits and configure when manual approval is required.

Examples:
  agentctl policy set agt_abc123 --limit-per-tx 5.00
  agentctl policy set agt_abc123 --limit-daily 50.00 --limit-monthly 200.00
  agentctl policy set agt_abc123 --approval-above 25.00 --currency USD`,
	Args: cobra.ExactArgs(1),
	RunE: runPolicySet,
}

func runPolicySet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	agentID := args[0]

	// Check at least one flag was provided.
	if policyLimitPerTx == "" && policyLimitDaily == "" && policyLimitMonthly == "" &&
		policyApprovalAbove == "" && policyCurrency == "" {
		return fmt.Errorf("no policy fields specified; use --limit-per-tx, --limit-daily, etc.")
	}

	newPolicy := &api.PolicyConfig{
		LimitPerTransaction: policyLimitPerTx,
		LimitDaily:          policyLimitDaily,
		LimitMonthly:        policyLimitMonthly,
		ApprovalAbove:       policyApprovalAbove,
		Currency:            policyCurrency,
	}

	spin := ui.NewSpinner("Updating spending policy...")
	spin.Start()
	client := api.New(cfg)
	resp, err := client.SetPolicy(agentID, newPolicy)
	if err != nil {
		spin.Fail("Failed to update policy")
		return err
	}
	spin.Stop("Spending policy updated")

	// Echo the saved policy back.
	fmt.Println()
	p := resp.Policy
	currency := p.Currency
	if currency == "" {
		currency = "USD"
	}
	rows := [][]string{}
	if p.LimitPerTransaction != "" {
		rows = append(rows, []string{"Per Transaction", fmt.Sprintf("%s %s", p.LimitPerTransaction, currency)})
	}
	if p.LimitDaily != "" {
		rows = append(rows, []string{"Daily Limit", fmt.Sprintf("%s %s", p.LimitDaily, currency)})
	}
	if p.LimitMonthly != "" {
		rows = append(rows, []string{"Monthly Limit", fmt.Sprintf("%s %s", p.LimitMonthly, currency)})
	}
	if p.ApprovalAbove != "" {
		rows = append(rows, []string{"Require Approval Above", fmt.Sprintf("%s %s", p.ApprovalAbove, currency)})
	}
	rows = append(rows, []string{"Currency", currency})

	for _, row := range rows {
		fmt.Printf("  %s  %s\n", ui.Dim(fmt.Sprintf("%-24s", row[0])), row[1])
	}
	return nil
}

func init() {
	policySetCmd.Flags().StringVar(&policyLimitPerTx, "limit-per-tx", "", "Maximum amount per single transaction")
	policySetCmd.Flags().StringVar(&policyLimitDaily, "limit-daily", "", "Maximum daily spending total")
	policySetCmd.Flags().StringVar(&policyLimitMonthly, "limit-monthly", "", "Maximum monthly spending total")
	policySetCmd.Flags().StringVar(&policyApprovalAbove, "approval-above", "", "Require manual approval for transactions above this amount")
	policySetCmd.Flags().StringVar(&policyCurrency, "currency", "USD", "Currency code (default: USD)")

	policyCmd.AddCommand(policySetCmd)
}
