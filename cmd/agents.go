package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/agentplane/cli/internal/api"
	"github.com/agentplane/cli/internal/config"
	"github.com/agentplane/cli/internal/ui"
	"github.com/spf13/cobra"
)

var agentsJSONOutput bool

var agentsCmd = &cobra.Command{
	Use:     "agents",
	Short:   "Manage your deployed agents",
	Aliases: []string{"agent"},
	Long:    "List, pause, resume, and delete your deployed agents.",
	RunE:    runAgentsList, // default sub-command
}

// ── list ──────────────────────────────────────────────────────────────────────

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all deployed agents",
	RunE:  runAgentsList,
}

func runAgentsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	spin := ui.NewSpinner("Fetching agents...")
	spin.Start()
	client := api.New(cfg)
	agents, err := client.ListAgents()
	if err != nil {
		spin.Fail("Failed to fetch agents")
		return err
	}
	spin.Stop("")

	if agentsJSONOutput {
		return ui.PrintJSON(agents)
	}

	if len(agents) == 0 {
		ui.Info("No agents deployed yet.")
		ui.Info("Deploy your first agent with: agentctl deploy ./my-agent/")
		return nil
	}

	ui.Header(fmt.Sprintf("Agents (%d)", len(agents)))
	headers := []string{"NAME", "STATUS", "EARNINGS (30D)", "TRANSACTIONS", "LAST ACTIVE"}
	rows := make([][]string, 0, len(agents))
	for _, a := range agents {
		earnings := fmt.Sprintf("$%.2f", a.Earnings30d)
		txCount := fmt.Sprintf("%d", a.Transactions)
		lastActive := a.LastActive
		if lastActive == "" {
			lastActive = "—"
		}
		rows = append(rows, []string{
			a.Name,
			ui.StatusColor(a.Status),
			earnings,
			txCount,
			lastActive,
		})
	}
	ui.Table(headers, rows)
	return nil
}

// ── pause ─────────────────────────────────────────────────────────────────────

var agentsPauseCmd = &cobra.Command{
	Use:   "pause [agent-id]",
	Short: "Pause an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsPause,
}

func runAgentsPause(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	agentID := args[0]
	spin := ui.NewSpinner(fmt.Sprintf("Pausing agent %s...", agentID))
	spin.Start()
	client := api.New(cfg)
	if err := client.PauseAgent(agentID); err != nil {
		spin.Fail("Failed to pause agent")
		return err
	}
	spin.Stop(fmt.Sprintf("Agent %s paused", ui.Bold(agentID)))
	ui.Info("Resume at any time with: agentctl agents resume " + agentID)
	return nil
}

// ── resume ────────────────────────────────────────────────────────────────────

var agentsResumeCmd = &cobra.Command{
	Use:   "resume [agent-id]",
	Short: "Resume a paused agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsResume,
}

func runAgentsResume(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	agentID := args[0]
	spin := ui.NewSpinner(fmt.Sprintf("Resuming agent %s...", agentID))
	spin.Start()
	client := api.New(cfg)
	if err := client.ResumeAgent(agentID); err != nil {
		spin.Fail("Failed to resume agent")
		return err
	}
	spin.Stop(fmt.Sprintf("Agent %s is now active", ui.Bold(agentID)))
	return nil
}

// ── delete ────────────────────────────────────────────────────────────────────

var agentsDeleteForce bool

var agentsDeleteCmd = &cobra.Command{
	Use:   "delete [agent-id]",
	Short: "Delete an agent permanently",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsDelete,
}

func runAgentsDelete(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	agentID := args[0]

	if !agentsDeleteForce {
		ui.Warning(fmt.Sprintf("You are about to permanently delete agent: %s", ui.Bold(agentID)))
		ui.Warning("This action cannot be undone. The agent wallet will be deactivated.")
		fmt.Print("  Type the agent ID to confirm: ")

		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		confirmation := strings.TrimSpace(scanner.Text())
		if confirmation != agentID {
			ui.Error("Confirmation did not match. Aborting.")
			return nil
		}
	}

	spin := ui.NewSpinner(fmt.Sprintf("Deleting agent %s...", agentID))
	spin.Start()
	client := api.New(cfg)
	if err := client.DeleteAgent(agentID); err != nil {
		spin.Fail("Failed to delete agent")
		return err
	}
	spin.Stop(fmt.Sprintf("Agent %s deleted", agentID))
	return nil
}

func init() {
	agentsListCmd.Flags().BoolVar(&agentsJSONOutput, "json", false, "Output as JSON")
	agentsDeleteCmd.Flags().BoolVarP(&agentsDeleteForce, "force", "f", false, "Skip confirmation prompt")

	agentsCmd.AddCommand(
		agentsListCmd,
		agentsPauseCmd,
		agentsResumeCmd,
		agentsDeleteCmd,
	)
}
