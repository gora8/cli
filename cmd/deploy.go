package cmd

import (
	"fmt"
	"os"
	"os/exec"
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
	Long: `Deploy an AI agent as an economic participant, not just a running process.

One command: an A2A agent card is generated, the agent's identity is
registered, a wallet is attached, and a spending Mandate is issued and
activated for on-chain enforcement — a signed document a counterparty can
verify independently, not a promise your agent's own code makes to itself
(see 'gora8 mandate'). With no spending policy set yet, the Mandate
authorizes zero spend; set one with 'gora8 policy set' and it's synced
automatically. None of this requires any other agent to exist yet: your
agent can be paid and can spend, safely, starting with its very first
transaction.

By default the agent publishes to gora8's own directory (free, instant, no
external dependency). Run 'gora8 publish' separately to reach other
audiences like x402 Bazaar.

Examples:
  gora8 deploy                      # Deploy from current directory
  gora8 deploy ./my-agent/          # Deploy from a specific path
  gora8 deploy --name "My Agent"    # Override agent name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
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
	if err := validatePricingModel(agentConfig.Pricing.Model); err != nil {
		return err
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
			Acceptance: &api.AcceptanceLimits{
				PerTransactionLimit: agentConfig.Policy.Acceptance.PerTransactionLimit,
				DailyCap:            agentConfig.Policy.Acceptance.DailyCap,
				MonthlyCap:          agentConfig.Policy.Acceptance.MonthlyCap,
				ApprovalThreshold:   agentConfig.Policy.Acceptance.ApprovalThreshold,
			},
			Spending: &api.SpendingLimits{
				PerTransactionLimit: agentConfig.Policy.Spending.PerTransactionLimit,
				DailyCap:            agentConfig.Policy.Spending.DailyCap,
				MonthlyCap:          agentConfig.Policy.Spending.MonthlyCap,
			},
			Currency: agentConfig.Policy.Currency,
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
	spin2.Stop("Agent registered — identity, wallet, and spending Mandate attached")

	// Step 3: Publish to the configured audiences (gora8's own directory by
	// default — free and instant, no external dependency).
	spin3 := ui.NewSpinner(fmt.Sprintf("Publishing to %s...", strings.Join(agentConfig.Registries, ", ")))
	spin3.Start()
	if _, err := client.PublishAgent(resp.Agent.ID, &api.PublishRequest{Registries: agentConfig.Registries}); err != nil {
		spin3.Fail("Publish failed — run 'gora8 publish " + resp.Agent.ID + "' to retry")
	} else {
		spin3.Stop("Published")
	}

	// Step 4: Best-effort local SDK setup — so the agent's own handler
	// code can `import gora8_agent` / `require("gora8-agent")`
	// immediately, without a separate manual install step. Never fails
	// the deploy itself: a skipped or failed install here just means
	// the developer installs it by hand, same as before this existed.
	setupAgentSDK(searchPath)

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
	if resp.Mandate != nil {
		details = append(details, []string{"Mandate", resp.Mandate.MandateID})
		if resp.Mandate.Enforcement != nil {
			if resp.Mandate.Enforcement.Delegated {
				details = append(details, []string{"Enforcement", ui.Green("active")})
			} else {
				details = append(details, []string{"Enforcement", ui.Yellow("not yet active — run `gora8 mandate issue-onchain " + resp.Agent.ID + "`")})
			}
		}
	}
	if resp.Agent.ActorRef != nil {
		// Not populated by the current API — see the ActorRef doc comment
		// in internal/api/client.go. Rendered defensively so this command
		// doesn't need another code change the day it is.
		details = append(details, []string{"Identity", fmt.Sprintf("%s:%s", resp.Agent.ActorRef.Namespace, resp.Agent.ActorRef.ActorID)})
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
	ui.Info("Run `gora8 agents list` to see all your agents.")
	ui.Info(fmt.Sprintf("Run `gora8 mandate %s` to see and verify its spending Mandate.", resp.Agent.ID))
	return nil
}

// setupAgentSDK detects the deployed project's language by its own
// existing manifest file (never gora8's own preference) and installs
// gora8-agent (see github.com/gora8/goraOS) into it, so the agent's own
// handler code — wherever agentConfig.Endpoint actually points, which
// this CLI never runs or bundles itself — has the SDK available the
// moment `gora8 deploy` finishes. Best-effort throughout: any failure
// degrades to a one-line manual-install hint, never an error this
// command returns.
func setupAgentSDK(dir string) {
	hasFile := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	runIn := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		return cmd.Run()
	}

	switch {
	case hasFile("package.json"):
		spin := ui.NewSpinner("Installing gora8-agent (npm)...")
		spin.Start()
		if err := runIn("npm", "install", "gora8-agent", "--save"); err != nil {
			spin.Fail("Couldn't install gora8-agent automatically")
			ui.Info("Run manually: npm install gora8-agent")
			return
		}
		spin.Stop(`gora8-agent installed — require("gora8-agent") from your handler`)

	case hasFile("requirements.txt"):
		appendLineIfMissing(filepath.Join(dir, "requirements.txt"), "gora8-agent")
		spin := ui.NewSpinner("Installing gora8-agent (pip)...")
		spin.Start()
		if runIn("pip", "install", "-q", "gora8-agent") != nil && runIn("pip3", "install", "-q", "gora8-agent") != nil {
			spin.Fail("Couldn't install gora8-agent automatically")
			ui.Info("Added to requirements.txt — run: pip install -r requirements.txt")
			return
		}
		spin.Stop("gora8-agent installed and added to requirements.txt")

	case hasFile("pyproject.toml"):
		spin := ui.NewSpinner("Installing gora8-agent (pip)...")
		spin.Start()
		if runIn("pip", "install", "-q", "gora8-agent") != nil && runIn("pip3", "install", "-q", "gora8-agent") != nil {
			spin.Fail("Couldn't install gora8-agent automatically")
			ui.Info("Run manually: pip install gora8-agent")
			return
		}
		spin.Stop("gora8-agent installed")
		ui.Info("Add it to pyproject.toml's own dependency list too, for reproducible installs elsewhere (poetry add / uv add gora8-agent).")

	default:
		ui.Info("No requirements.txt/pyproject.toml/package.json found here — install the SDK yourself: pip install gora8-agent (Python) or npm install gora8-agent (Node)")
	}
}

// appendLineIfMissing is deliberately a plain substring check, not a
// requirements-file parser (no version-pin awareness, no comment
// handling) — good enough to avoid a duplicate "gora8-agent" line on a
// repeat `gora8 deploy`, not a general dependency-file editor.
func appendLineIfMissing(path, line string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if strings.Contains(string(data), line) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, _ = f.WriteString("\n")
	}
	_, _ = f.WriteString(line + "\n")
}

// validPricingModels mirrors the values the dashboard and API actually
// recognize (web/app/(app)/agents/[id]/page.tsx) — anything else stores
// silently as an opaque string that the UI can't render as a selection.
var validPricingModels = map[string]bool{
	"per-call":     true,
	"per-token":    true,
	"subscription": true,
	"free":         true,
}

func validatePricingModel(model string) error {
	if model == "" || validPricingModels[model] {
		return nil
	}
	return fmt.Errorf("invalid pricing.model %q in agent.yaml — must be one of: per-call, per-token, subscription, free", model)
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
