package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var seoCmd = &cobra.Command{
	Use:   "seo [agent-id]",
	Short: "View discoverability score and suggestions",
	Long: `View the discoverability (SEO) score for an agent and get actionable
suggestions to improve how easily other agents can find and hire yours.

Score interpretation:
  > 80  — Great discoverability
  50-80 — Room for improvement
  < 50  — Poor discoverability, follow suggestions

Example:
  gora8 seo agt_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runSEO,
}

func runSEO(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	agentID := args[0]

	spin := ui.NewSpinner("Analyzing discoverability...")
	spin.Start()
	client := api.New(cfg)
	resp, err := client.GetSEO(agentID)
	if err != nil {
		spin.Fail("Failed to fetch SEO data")
		return err
	}
	spin.Stop("")

	title := "Discoverability Report"
	if resp.AgentName != "" {
		title = fmt.Sprintf("Discoverability — %s", resp.AgentName)
	}
	ui.Header(title)

	// Score with color coding.
	scoreStr := ui.ScoreColor(resp.Score)
	fmt.Printf("\n  %s  %s\n\n", ui.Bold("Score"), scoreStr)

	// Issues.
	if len(resp.Issues) > 0 {
		fmt.Println(ui.Bold("  Issues"))
		for _, issue := range resp.Issues {
			var prefix string
			switch issue.Severity {
			case "critical", "high":
				prefix = ui.Red("  ✗")
			case "medium":
				prefix = ui.Yellow("  !")
			default:
				prefix = ui.Dim("  –")
			}
			fmt.Printf("%s [%s] %s\n", prefix, ui.Dim(issue.Severity), issue.Description)
		}
		fmt.Println()
	}

	// Suggestions.
	if len(resp.Suggestions) > 0 {
		fmt.Println(ui.Bold("  Suggestions"))
		for i, suggestion := range resp.Suggestions {
			var priority string
			switch suggestion.Priority {
			case "high":
				priority = ui.Red(fmt.Sprintf("P%d", i+1))
			case "medium":
				priority = ui.Yellow(fmt.Sprintf("P%d", i+1))
			default:
				priority = ui.Dim(fmt.Sprintf("P%d", i+1))
			}
			fmt.Printf("  %s  %s\n", priority, suggestion.Description)
		}
		fmt.Println()
	}

	// Guidance.
	if resp.Score > 80 {
		ui.Success("Your agent has excellent discoverability!")
	} else if resp.Score >= 50 {
		ui.Warning("Improve discoverability by addressing the suggestions above.")
	} else {
		ui.Error("Poor discoverability. Address high-priority issues to get hired more often.")
	}

	return nil
}
