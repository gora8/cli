package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var policyCmd = &cobra.Command{
	Use:   "policy [agent-id]",
	Short: "View or manage acceptance and spending limits for an agent",
	Long: `View the current acceptance and spending limits for an agent.

Two genuinely different things: acceptance limits control which incoming
calls this agent takes (not a financial safeguard — being paid isn't a
risk); spending limits cap how much this agent may pay out of its own
wallet when it hires another agent (the real financial guardrail).

Use 'gora8 policy set' to update either.

Example:
  gora8 policy agt_abc123`,
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
		ui.Error("Not authenticated. Run: gora8 auth login")
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

	p := resp.Policy
	currency := p.Currency
	if currency == "" {
		currency = "USD"
	}

	ui.Header(fmt.Sprintf("Policy — %s", agentID))
	fmt.Println()
	fmt.Printf("  %s\n", ui.Bold("Status: "+statusLabel(p.Suspended)))

	fmt.Println()
	fmt.Printf("  %s\n", ui.Dim("Acceptance limits — which incoming calls this agent takes"))
	printLimitRows(acceptanceRows(p.Acceptance, currency))

	fmt.Println()
	fmt.Printf("  %s\n", ui.Dim("Spending limits — what this agent may pay to hire other agents"))
	printLimitRows(spendingRows(p.Spending, currency))

	fmt.Println()
	ui.Info(fmt.Sprintf("Update with: gora8 policy set %s --limit-per-tx <amount>  (acceptance)", agentID))
	ui.Info(fmt.Sprintf("         or: gora8 policy set %s --spend-per-tx <amount>  (spending)", agentID))
	return nil
}

func statusLabel(suspended bool) string {
	if suspended {
		return "Suspended"
	}
	return "Active"
}

func acceptanceRows(a *api.AcceptanceLimits, currency string) [][]string {
	if a == nil {
		return [][]string{{"(none set)", ""}}
	}
	rows := [][]string{}
	if a.PerTransactionLimit > 0 {
		rows = append(rows, []string{"Per-call limit", fmt.Sprintf("%.2f %s", a.PerTransactionLimit, currency)})
	}
	if a.DailyCap > 0 {
		rows = append(rows, []string{"Daily cap", fmt.Sprintf("%.2f %s", a.DailyCap, currency)})
	}
	if a.MonthlyCap > 0 {
		rows = append(rows, []string{"Monthly cap", fmt.Sprintf("%.2f %s", a.MonthlyCap, currency)})
	}
	if a.ApprovalThreshold > 0 {
		rows = append(rows, []string{"Approval threshold", fmt.Sprintf("%.2f %s", a.ApprovalThreshold, currency)})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none set)", ""})
	}
	return rows
}

func spendingRows(s *api.SpendingLimits, currency string) [][]string {
	if s == nil {
		return [][]string{{"(none set)", ""}}
	}
	rows := [][]string{}
	if s.PerTransactionLimit > 0 {
		rows = append(rows, []string{"Per-hire limit", fmt.Sprintf("%.2f %s", s.PerTransactionLimit, currency)})
	}
	if s.DailyCap > 0 {
		rows = append(rows, []string{"Daily cap", fmt.Sprintf("%.2f %s", s.DailyCap, currency)})
	}
	if s.MonthlyCap > 0 {
		rows = append(rows, []string{"Monthly cap", fmt.Sprintf("%.2f %s", s.MonthlyCap, currency)})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"(none set)", ""})
	}
	return rows
}

func printLimitRows(rows [][]string) {
	for _, row := range rows {
		if row[1] == "" {
			fmt.Printf("    %s\n", ui.Dim(row[0]))
			continue
		}
		fmt.Printf("    %s  %s\n", ui.Dim(fmt.Sprintf("%-20s", row[0])), row[1])
	}
}

// ── set ───────────────────────────────────────────────────────────────────────

var (
	policyLimitPerTx    float64
	policyLimitDaily    float64
	policyLimitMonthly  float64
	policyApprovalAbove float64
	policySpendPerTx    float64
	policySpendDaily    float64
	policySpendMonthly  float64
	policyCurrency      string
)

var policySetCmd = &cobra.Command{
	Use:   "set [agent-id]",
	Short: "Update acceptance or spending limits for an agent",
	Long: `Update acceptance limits (incoming calls) or spending limits (outbound
hiring) for an agent. Only the flags you pass are changed — everything
else is fetched first and left untouched.

Examples:
  gora8 policy set agt_abc123 --limit-per-tx 5.00          # acceptance
  gora8 policy set agt_abc123 --limit-daily 50.00 --limit-monthly 200.00
  gora8 policy set agt_abc123 --approval-above 25.00

  gora8 policy set agt_abc123 --spend-per-tx 2.00          # spending
  gora8 policy set agt_abc123 --spend-daily 20.00 --spend-monthly 100.00`,
	Args: cobra.ExactArgs(1),
	RunE: runPolicySet,
}

func runPolicySet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	agentID := args[0]
	flags := cmd.Flags()
	changed := flags.Changed("limit-per-tx") || flags.Changed("limit-daily") || flags.Changed("limit-monthly") ||
		flags.Changed("approval-above") || flags.Changed("spend-per-tx") || flags.Changed("spend-daily") ||
		flags.Changed("spend-monthly") || flags.Changed("currency")
	if !changed {
		return fmt.Errorf("no policy fields specified; use --limit-per-tx, --spend-per-tx, etc. (see --help)")
	}

	client := api.New(cfg)

	// PATCH replaces the whole stored policy, not a partial merge — fetch
	// the current policy first so untouched fields (including whichever of
	// acceptance/spending this call isn't updating) survive the round trip.
	current, err := client.GetPolicy(agentID)
	if err != nil {
		return fmt.Errorf("fetch current policy: %w", err)
	}
	policy := current.Policy
	if policy.Acceptance == nil {
		policy.Acceptance = &api.AcceptanceLimits{}
	}
	if policy.Spending == nil {
		policy.Spending = &api.SpendingLimits{}
	}

	if flags.Changed("limit-per-tx") {
		policy.Acceptance.PerTransactionLimit = policyLimitPerTx
	}
	if flags.Changed("limit-daily") {
		policy.Acceptance.DailyCap = policyLimitDaily
	}
	if flags.Changed("limit-monthly") {
		policy.Acceptance.MonthlyCap = policyLimitMonthly
	}
	if flags.Changed("approval-above") {
		policy.Acceptance.ApprovalThreshold = policyApprovalAbove
	}
	if flags.Changed("spend-per-tx") {
		policy.Spending.PerTransactionLimit = policySpendPerTx
	}
	if flags.Changed("spend-daily") {
		policy.Spending.DailyCap = policySpendDaily
	}
	if flags.Changed("spend-monthly") {
		policy.Spending.MonthlyCap = policySpendMonthly
	}
	if flags.Changed("currency") {
		policy.Currency = policyCurrency
	}

	spin := ui.NewSpinner("Updating policy...")
	spin.Start()
	resp, err := client.SetPolicy(agentID, &policy)
	if err != nil {
		spin.Fail("Failed to update policy")
		return err
	}
	spin.Stop("Policy updated")

	fmt.Println()
	p := resp.Policy
	currency := p.Currency
	if currency == "" {
		currency = "USD"
	}
	fmt.Printf("  %s\n", ui.Dim("Acceptance limits"))
	printLimitRows(acceptanceRows(p.Acceptance, currency))
	fmt.Println()
	fmt.Printf("  %s\n", ui.Dim("Spending limits"))
	printLimitRows(spendingRows(p.Spending, currency))

	// A spending edit changes the agent's mandateId — the API resyncs
	// on-chain enforcement to match automatically, best-effort. Surfaced
	// here so a real sync failure isn't silent, not because the caller
	// needs to do anything about it (see 'gora8 mandate issue-onchain'
	// for the manual retry if it ever comes to that).
	if resp.Mandate != nil && resp.Mandate.Enforcement != nil && !resp.Mandate.Enforcement.Delegated {
		fmt.Println()
		ui.Warning("On-chain enforcement sync failed: " + resp.Mandate.Enforcement.Error)
		ui.Info(fmt.Sprintf("Retry with: gora8 mandate issue-onchain %s", agentID))
	}
	return nil
}

func init() {
	policySetCmd.Flags().Float64Var(&policyLimitPerTx, "limit-per-tx", 0, "Acceptance: max amount per single incoming call")
	policySetCmd.Flags().Float64Var(&policyLimitDaily, "limit-daily", 0, "Acceptance: max daily incoming total")
	policySetCmd.Flags().Float64Var(&policyLimitMonthly, "limit-monthly", 0, "Acceptance: max monthly incoming total")
	policySetCmd.Flags().Float64Var(&policyApprovalAbove, "approval-above", 0, "Acceptance: require manual approval above this amount")
	policySetCmd.Flags().Float64Var(&policySpendPerTx, "spend-per-tx", 0, "Spending: max amount per single hire")
	policySetCmd.Flags().Float64Var(&policySpendDaily, "spend-daily", 0, "Spending: max daily hiring spend")
	policySetCmd.Flags().Float64Var(&policySpendMonthly, "spend-monthly", 0, "Spending: max monthly hiring spend")
	policySetCmd.Flags().StringVar(&policyCurrency, "currency", "", "Currency code")

	policyCmd.AddCommand(policySetCmd)
}
