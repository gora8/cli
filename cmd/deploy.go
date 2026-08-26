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
	deployName          string
	deployCapabilities  string
	deployPrice         string
	deployRegistries    string
	deployWalletAddress string
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

	// Step 1.5: Generate the agent's own EVM wallet locally — gora8 never
	// generates or holds this key (see SELF_CUSTODY_ARCHITECTURE.md in
	// the gora8 monorepo). Named after the agent so `gora8-signer start
	// <name>` (run wherever the agent's own endpoint actually lives —
	// often a different machine than the one running this deploy) can
	// find the same key again.
	signerName := slugify(agentConfig.Name)
	var walletAddress string
	if deployWalletAddress != "" {
		walletAddress = deployWalletAddress
		ui.Info(fmt.Sprintf("Using provided wallet address: %s", walletAddress))
	} else {
		spinSigner := ui.NewSpinner("Generating agent wallet (gora8-signer)...")
		spinSigner.Start()
		address, err := initSigner(signerName)
		if err != nil {
			spinSigner.Fail("Couldn't generate a wallet locally")
			return fmt.Errorf(
				"gora8-signer init failed: %w\n\nInstall it first: npm install -g gora8-signer (requires Node.js), "+
					"or run 'npx gora8-signer init %s' yourself and pass the address with --wallet-address", err, signerName,
			)
		}
		walletAddress = address
		spinSigner.Stop(fmt.Sprintf("Wallet generated: %s", walletAddress))
	}

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
		Registries:    agentConfig.Registries,
		A2ACard:       a2aCard,
		WalletAddress: walletAddress,
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

	// One command, multiple chains: report the automatic rollout the same
	// deploy call just triggered (api/src/lib/chains.ts's
	// getAutoRolloutChains() — Ethereum isn't in it, see `gora8 chains`).
	if len(resp.Chains) > 0 {
		active, awaitingGas := 0, 0
		for _, c := range resp.Chains {
			if c.Status == "active" {
				active++
			} else if c.Status == "awaiting_gas" {
				awaitingGas++
			}
		}
		ui.Info(fmt.Sprintf("Rolled out to %d/%d additional chains.", active, len(resp.Chains)))
		if awaitingGas > 0 {
			ui.Warning(fmt.Sprintf("%d chain(s) need the agent wallet funded with native gas — see `gora8 wallet fund` and `gora8 chains list`.", awaitingGas))
		}
	}

	ui.Info("Run `gora8 agents list` to see all your agents.")
	ui.Info(fmt.Sprintf("Run `gora8 mandate %s` to see and verify its spending Mandate.", resp.Agent.ID))
	ui.Info("Run `gora8 chains list` to see every supported chain, and `gora8 chains add` to opt into Ethereum.")
	if deployWalletAddress == "" {
		ui.Info(fmt.Sprintf(
			"Wherever %s actually runs (if that's not this machine), run `npx gora8-signer start %s` there too — "+
				"that's what holds the key and answers gora8's signing requests.",
			agentConfig.Name, signerName,
		))
	}
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
// initSigner shells out to gora8-signer (an npm package — see
// SELF_CUSTODY_ARCHITECTURE.md and signer-ts/ in the gora8 monorepo)
// to generate the agent's own EVM key locally and return its address.
// Uses `npx --yes` rather than requiring a prior global install, same
// convention as setupAgentSDK's npm/pip calls below. The key itself
// never touches this process's stdout/stderr or any gora8 API call —
// only the derived address does.
func initSigner(name string) (string, error) {
	cmd := exec.Command("npx", "--yes", "gora8-signer", "init", name)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	address := strings.TrimSpace(string(output))
	if address == "" {
		return "", fmt.Errorf("gora8-signer init produced no address")
	}
	return address, nil
}

// slugify turns an agent's display name into a filesystem/keychain-safe
// identifier for gora8-signer's local storage — lowercase, ASCII
// letters/digits/hyphens only, so it's stable across the shells and
// OSes a developer might run `gora8-signer start <name>` from later.
func slugify(name string) string {
	var b strings.Builder
	lastWasHyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasHyphen = false
		case !lastWasHyphen:
			b.WriteRune('-')
			lastWasHyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "agent"
	}
	return slug
}

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
		manifestPath := filepath.Join(dir, "requirements.txt")
		appendLineIfMissing(manifestPath, "gora8-agent")
		pkgs := pipPackagesFor(manifestPath)
		spin := ui.NewSpinner("Installing gora8-agent (pip)...")
		spin.Start()
		if runIn("pip", append([]string{"install", "-q"}, pkgs...)...) != nil && runIn("pip3", append([]string{"install", "-q"}, pkgs...)...) != nil {
			spin.Fail("Couldn't install gora8-agent automatically")
			ui.Info("Added to requirements.txt — run: pip install -r requirements.txt")
			return
		}
		spin.Stop(strings.Join(pkgs, " ") + " installed and added to requirements.txt")

	case hasFile("pyproject.toml"):
		pkgs := pipPackagesFor(filepath.Join(dir, "pyproject.toml"))
		spin := ui.NewSpinner("Installing gora8-agent (pip)...")
		spin.Start()
		if runIn("pip", append([]string{"install", "-q"}, pkgs...)...) != nil && runIn("pip3", append([]string{"install", "-q"}, pkgs...)...) != nil {
			spin.Fail("Couldn't install gora8-agent automatically")
			ui.Info("Run manually: pip install " + strings.Join(pkgs, " "))
			return
		}
		spin.Stop(strings.Join(pkgs, " ") + " installed")
		ui.Info("Add them to pyproject.toml's own dependency list too, for reproducible installs elsewhere (poetry add / uv add).")

	default:
		ui.Info("No requirements.txt/pyproject.toml/package.json found here — install the SDK yourself: pip install gora8-agent (Python) or npm install gora8-agent (Node)")
	}
}

// frameworkExtras maps a framework package name a project might already
// depend on to gora8-adapters' own matching extra (see
// adapters-python/pyproject.toml in github.com/gora8/cli's
// adapters-python/ — moved there from goraOS specifically because this
// is onboarding tooling for an existing framework agent, not a runtime
// capability). Deliberately keyed on the framework's own package name,
// not gora8's — this only ever fires if the project already declares
// that dependency itself, so a project using none of these gets nothing
// extra installed.
var frameworkExtras = map[string]string{
	"langgraph":         "langgraph",
	"crewai":            "crewai",
	"openai-agents":     "openai-agents",
	"google-adk":        "google-adk",
	"agno":              "agno",
	"semantic-kernel":   "semantic-kernel",
	"autogen-agentchat": "autogen",
	"autogen-ext":       "autogen",
}

// pipPackagesFor always includes gora8-agent, plus
// gora8-adapters[<extra>] for every recognized framework already named
// in the given manifest — no prompt, no flag, nothing to answer. A
// plain substring scan (like appendLineIfMissing), not a real
// requirements.txt/pyproject.toml parser: good enough to catch "this
// project already depends on langgraph" without needing per-format
// parsing logic for two different manifest shapes.
func pipPackagesFor(manifestPath string) []string {
	pkgs := []string{"gora8-agent"}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return pkgs
	}
	content := string(data)
	seenExtras := map[string]bool{}
	for pkgName, extra := range frameworkExtras {
		if strings.Contains(content, pkgName) && !seenExtras[extra] {
			seenExtras[extra] = true
			pkgs = append(pkgs, fmt.Sprintf("gora8-adapters[%s]", extra))
		}
	}
	return pkgs
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
	deployCmd.Flags().StringVar(&deployWalletAddress, "wallet-address", "", "Use an already-generated EVM address instead of running gora8-signer init locally (e.g. if you generated it elsewhere)")
}
