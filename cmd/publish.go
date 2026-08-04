package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var publishRegistries string

var publishCmd = &cobra.Command{
	Use:   "publish [agent-id]",
	Short: "Publish an agent to discovery audiences",
	Long: `Publish an agent to one or more discovery audiences.

Run 'gora8 publish <agent-id>' with no --registries flag to see the
currently available audiences fetched live from the server, or pass
--registries all to publish to every one of them.

Examples:
  gora8 publish agt_abc123
  gora8 publish agt_abc123 --registries gora8,x402
  gora8 publish agt_abc123 --registries all`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func runPublish(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	agentID := args[0]
	client := api.New(cfg)

	available, err := client.ListRegistries()
	if err != nil {
		return fmt.Errorf("list available audiences: %w", err)
	}

	registryList, err := parseRegistries(publishRegistries, available)
	if err != nil {
		return err
	}

	ui.Header(fmt.Sprintf("Publishing Agent %s", agentID))
	fmt.Println()

	// Show individual registry spinners for visual feedback.
	for _, reg := range registryList {
		spin := ui.NewSpinner(fmt.Sprintf("Publishing to %s...", ui.Bold(reg)))
		spin.Start()
		time.Sleep(200 * time.Millisecond) // Brief visual pause.
		spin.Stop("")                      // Stop will be overridden by API response.
	}

	// Call the API.
	spin := ui.NewSpinner("Finalizing publication...")
	spin.Start()
	resp, err := client.PublishAgent(agentID, &api.PublishRequest{
		Registries: registryList,
	})
	if err != nil {
		spin.Fail("Publication failed")
		return err
	}
	spin.Stop("")

	fmt.Println()

	// Report results per registry.
	allOK := true
	for _, result := range resp.Results {
		if result.Status == "success" || result.Status == "published" {
			ui.Success(fmt.Sprintf("%-10s %s", result.Registry, ui.Dim(result.URL)))
		} else {
			allOK = false
			errMsg := result.Error
			if errMsg == "" {
				errMsg = "unknown error"
			}
			ui.Error(fmt.Sprintf("%-10s %s", result.Registry, errMsg))
		}
		// Pre-publish signals (low success rate, open disputes, no call
		// history yet) — non-blocking, shown even on a successful publish.
		for _, w := range result.Warnings {
			ui.Warning(fmt.Sprintf("           %s", w))
		}
	}

	fmt.Println()
	if allOK {
		ui.Success(fmt.Sprintf("Agent %s published to %d audience%s",
			ui.Bold(agentID), len(resp.Results), pluralSuffix(len(resp.Results), "", "s")))
	} else {
		ui.Warning("Some audiences failed. Check the errors above and retry.")
	}
	return nil
}

// parseRegistries expands "all" (against the live-fetched audience list) and
// trims each requested name, validating it against what the server actually
// supports rather than a hardcoded vocabulary.
func parseRegistries(raw string, available []string) ([]string, error) {
	if raw == "" {
		return available, nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.EqualFold(p, "all") {
			return available, nil
		}
		if p == "" {
			continue
		}
		found := false
		for _, a := range available {
			if strings.EqualFold(a, p) {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown audience %q. Available: %s", p, strings.Join(available, ", "))
		}
		result = append(result, p)
	}
	return result, nil
}

func pluralSuffix(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func init() {
	publishCmd.Flags().StringVar(&publishRegistries, "registries", "",
		"Comma-separated audiences to publish to (default: all available)")
}
