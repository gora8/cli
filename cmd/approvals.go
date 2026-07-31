package cmd

import (
	"fmt"
	"time"

	"github.com/agentplane/cli/internal/api"
	"github.com/agentplane/cli/internal/config"
	"github.com/agentplane/cli/internal/ui"
	"github.com/spf13/cobra"
)

var approvalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "Manage pending transaction approvals",
	Long:  "List, approve, or deny pending transaction approvals for your agents.",
}

// ── list ──────────────────────────────────────────────────────────────────────

var approvalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending approvals",
	RunE:  runApprovalsList,
}

func runApprovalsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	spin := ui.NewSpinner("Fetching pending approvals...")
	spin.Start()
	client := api.New(cfg)
	approvals, err := client.ListApprovals("pending")
	if err != nil {
		spin.Fail("Failed to fetch approvals")
		return err
	}
	spin.Stop("")

	if len(approvals) == 0 {
		ui.Success("No pending approvals.")
		return nil
	}

	ui.Header(fmt.Sprintf("Pending Approvals (%d)", len(approvals)))

	headers := []string{"ID", "AGENT", "COUNTERPARTY", "AMOUNT", "CAPABILITY", "AGE"}
	rows := make([][]string, 0, len(approvals))
	for _, a := range approvals {
		age := humanizeAge(a.CreatedAt)
		currency := a.Currency
		if currency == "" {
			currency = "USD"
		}
		amount := fmt.Sprintf("$%.2f %s", a.Amount, currency)
		rows = append(rows, []string{
			a.ID,
			a.AgentName,
			a.Counterparty,
			amount,
			a.Capability,
			age,
		})
	}
	ui.Table(headers, rows)
	fmt.Println()
	ui.Info("Approve with: agentctl approvals approve <id>")
	ui.Info("Deny with:    agentctl approvals deny <id>")
	return nil
}

// ── approve ───────────────────────────────────────────────────────────────────

var approvalsApproveCmd = &cobra.Command{
	Use:   "approve [approval-id]",
	Short: "Approve a pending transaction",
	Args:  cobra.ExactArgs(1),
	RunE:  runApprovalsApprove,
}

func runApprovalsApprove(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	approvalID := args[0]
	spin := ui.NewSpinner(fmt.Sprintf("Approving %s...", approvalID))
	spin.Start()
	client := api.New(cfg)
	if err := client.ApproveApproval(approvalID); err != nil {
		spin.Fail("Failed to approve")
		return err
	}
	spin.Stop(fmt.Sprintf("Approval %s approved — transaction will proceed", ui.Bold(approvalID)))
	return nil
}

// ── deny ──────────────────────────────────────────────────────────────────────

var approvalsDenyCmd = &cobra.Command{
	Use:   "deny [approval-id]",
	Short: "Deny a pending transaction",
	Args:  cobra.ExactArgs(1),
	RunE:  runApprovalsDeny,
}

func runApprovalsDeny(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	approvalID := args[0]
	spin := ui.NewSpinner(fmt.Sprintf("Denying %s...", approvalID))
	spin.Start()
	client := api.New(cfg)
	if err := client.DenyApproval(approvalID); err != nil {
		spin.Fail("Failed to deny")
		return err
	}
	spin.Stop(fmt.Sprintf("Approval %s denied — transaction blocked", ui.Bold(approvalID)))
	return nil
}

// humanizeAge converts an ISO8601 timestamp to a human-readable age string.
func humanizeAge(ts string) string {
	if ts == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try without nanoseconds.
		t, err = time.Parse("2006-01-02T15:04:05Z", ts)
		if err != nil {
			return ts
		}
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func init() {
	approvalsCmd.AddCommand(approvalsListCmd, approvalsApproveCmd, approvalsDenyCmd)
}
