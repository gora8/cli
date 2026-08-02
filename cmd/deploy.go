package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/card"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	deployName         string
	deployCapabilities string
	deployPrice        string
	deployRegistries   string
)

var deployCmd = &cobra.Command{
	Use:   "deploy [path]",
	Short: "Deploy an agent to gora8",
	Long: `Deploy an AI agent to gora8 and make it a first-class economic participant.

The deploy command reads your agent.yaml configuration, generates an A2A agent
card, registers the agent's identity and wallet, and publishes it to gora8's
own directory by default (free, instant, no external dependency). Run
'agentctl publish' separately to reach other audiences like x402 Bazaar.

Examples:
  agentctl deploy                      # Deploy from current directory
  agentctl deploy ./my-agent/          # Deploy from a specific path
  agentctl deploy --name "My Agent"    # Override agent name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	// Determine search path.
	searchPath := "."
	if len(args) > 0 {
		searchPath = args[0]
	}

	// Load agent.yaml.
	agentConfig, agentFilePath, err := loadAgentYAML(searchPath)
	if err != nil {
		return fmt.Errorf("load agent config: %w", err)
	}

	ui.Header("Deploying Agent")
	ui.Info(fmt.Sprintf("Config: %s", agentFilePath))

	// Apply flag overrides.
	if deployName != "" {
		agentConfig.Name = deployName
	}
	if deployCapabilities != "" {
		caps := strings.Split(deployCapabilities, ",")
		agentConfig.Capabilities = make([]card.YAMLCapability, 0, len(caps))
		for _, c := range caps {
			agentConfig.Capabilities = append(agentConfig.Capabilities, card.YAMLCapability{
				ID:          strings.TrimSpace(c),
				Description: strings.TrimSpace(c),
			})
		}
	}
	if deployPrice != "" {
		agentConfig.Pricing.Amount = deployPrice
	}
	if deployRegistries != "" {
		agentConfig.Registries = strings.Split(deployRegistries, ",")
	}
	if len(agentConfig.Registries) == 0 {
		agentConfig.Registries = []string{"gora8"}
	}

	fmt.Println()

	// Step 1: Generate A2A card.
	spin1 := ui.NewSpinner("Generating A2A agent card...")
	spin1.Start()
	time.Sleep(300 * time.Millisecond) // Brief visual pause for UX.
	a2aCard := card.Generate(agentConfig)
	if err := card.Validate(a2aCard); err != nil {
		spin1.Fail("A2A card validation failed")
		return fmt.Errorf("invalid agent config: %w", err)
	}
	spin1.Stop("A2A agent card generated")

	// Step 2: Register the agent (identity, wallet, and spending policy are
	// all provisioned by this one call).
	spin2 := ui.NewSpinner("Registering agent...")
	spin2.Start()

	// Build the deploy request.
	req := &api.DeployRequest{
		Name:        agentConfig.Name,
		Description: agentConfig.Description,
		Version:     agentConfig.Version,
		Endpoint:    agentConfig.Endpoint,
		Pricing: api.Pricing{
			Model:    agentConfig.Pricing.Model,
			Amount:   agentConfig.Pricing.Amount,
			Currency: agentConfig.Pricing.Currency,
		},
		Policy: api.PolicyConfig{
			LimitPerTransaction: agentConfig.Policy.LimitPerTransaction,
			LimitDaily:          agentConfig.Policy.LimitDaily,
			Currency:            agentConfig.Policy.Currency,
		},
		Registries: agentConfig.Registries,
		A2ACard:    a2aCard,
	}
	for _, cap := range agentConfig.Capabilities {
		req.Capabilities = append(req.Capabilities, api.Capability{
			ID:          cap.ID,
			Description: cap.Description,
		})
	}

	client := api.New(cfg)
	resp, err := client.DeployAgent(req)
	if err != nil {
		spin2.Fail("Deployment failed")
		return err
	}
	spin2.Stop("Agent registered — identity and wallet attached")

	// Step 3: Publish to the configured audiences (gora8's own directory by
	// default — free and instant, no external dependency).
	spin3 := ui.NewSpinner(fmt.Sprintf("Publishing to %s...", strings.Join(agentConfig.Registries, ", ")))
	spin3.Start()
	if _, err := client.PublishAgent(resp.Agent.ID, &api.PublishRequest{Registries: agentConfig.Registries}); err != nil {
		spin3.Fail("Publish failed — run 'agentctl publish " + resp.Agent.ID + "' to retry")
	} else {
		spin3.Stop("Published")
	}

	fmt.Println()
	ui.Success(fmt.Sprintf("Agent %s deployed successfully!", ui.Bold(agentConfig.Name)))
	fmt.Println()

	// Output summary.
	details := [][]string{
		{"Agent ID", resp.Agent.ID},
		{"Status", ui.StatusColor(resp.Agent.Status)},
	}
	if resp.WalletAddr != "" {
		details = append(details, []string{"Wallet", resp.WalletAddr})
	}
	dashURL := resp.DashboardURL
	if dashURL == "" && resp.Agent.ID != "" {
		dashURL = fmt.Sprintf("https://app.gora8.com/agents/%s", resp.Agent.ID)
	}
	details = append(details, []string{"Dashboard", ui.Cyan(dashURL)})

	for _, d := range details {
		fmt.Printf("  %s  %s\n", ui.Dim(fmt.Sprintf("%-12s", d[0])), d[1])
	}
	fmt.Println()
	ui.Info("Run `agentctl agents list` to see all your agents.")
	return nil
}

// loadAgentYAML searches for an agent.yaml (or agent.yml) in the given
// directory and parses it.
func loadAgentYAML(dir string) (*card.AgentYAML, string, error) {
	candidates := []string{
		filepath.Join(dir, "agent.yaml"),
		filepath.Join(dir, "agent.yml"),
	}

	var filePath string
	var data []byte
	var readErr error

	for _, candidate := range candidates {
		d, err := os.ReadFile(candidate)
		if err == nil {
			filePath = candidate
			data = d
			break
		}
		readErr = err
	}

	if filePath == "" {
		return nil, "", fmt.Errorf("no agent.yaml found in %s: %w", dir, readErr)
	}

	var agentCfg card.AgentYAML
	if err := yaml.Unmarshal(data, &agentCfg); err != nil {
		return nil, filePath, fmt.Errorf("parse %s: %w", filePath, err)
	}

	return &agentCfg, filePath, nil
}

func init() {
	deployCmd.Flags().StringVar(&deployName, "name", "", "Override the agent name")
	deployCmd.Flags().StringVar(&deployCapabilities, "capabilities", "", "Comma-separated capability IDs to set")
	deployCmd.Flags().StringVar(&deployPrice, "price", "", "Override the price per task (e.g. 0.50)")
	deployCmd.Flags().StringVar(&deployRegistries, "registries", "gora8", "Comma-separated audiences to publish to on deploy")
}
