package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/card"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/openapi"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	deployName                string
	deployCapabilities        string
	deployPrice               string
	deployRegistries          string
	deployWalletAddress       string
	deploySolanaWalletAddress string
	deployFromOpenAPI         string
	deployOperations          string
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
  gora8 deploy --name "My Agent"    # Override agent name

  # Wrapping an existing REST API instead of an agent framework: derive
  # capabilities from its OpenAPI spec, curated to a subset, and get a
  # market price reference for the first one. Requires endpoint in
  # agent.yaml to point at a running gora8_adapters.rest wrapper
  # (adapters-python) — this flag only touches agent.yaml, it doesn't run
  # anything.
  gora8 deploy --from-openapi ./openapi.json --operations textToSpeech,listVoices`,
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
	client := api.New(cfg)

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

	// --from-openapi derives capabilities from an existing OpenAPI spec
	// instead of hand-authoring them — for a SaaS company wrapping an
	// existing multi-operation REST API (see adapters-python's rest.py,
	// which is what actually runs the wrapper this endpoint points at;
	// this flag only ever touches agent.yaml, never calls the wrapped API
	// itself). Runs before "Apply flag overrides" below so an explicit
	// --capabilities still wins if both are given.
	usedOpenAPI := deployFromOpenAPI != ""
	if usedOpenAPI {
		var allow []string
		if deployOperations != "" {
			allow = strings.Split(deployOperations, ",")
		}
		spin := ui.NewSpinner(fmt.Sprintf("Deriving capabilities from %s...", deployFromOpenAPI))
		spin.Start()
		caps, err := openapi.Capabilities(deployFromOpenAPI, allow)
		if err != nil {
			spin.Fail("Couldn't parse OpenAPI spec")
			return err
		}
		if len(caps) == 0 {
			spin.Fail("No operations found")
			return fmt.Errorf("%s declared no operations matching --operations, or none at all", deployFromOpenAPI)
		}
		agentConfig.Capabilities = make([]card.YAMLCapability, 0, len(caps))
		for _, c := range caps {
			agentConfig.Capabilities = append(agentConfig.Capabilities, card.YAMLCapability{ID: c.ID, Description: c.Description})
		}
		spin.Stop(fmt.Sprintf("%d capabilit%s derived: %s", len(caps), pluralY(len(caps)), joinCapabilityIDs(caps)))
	}

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
	} else if usedOpenAPI && len(agentConfig.Capabilities) > 0 {
		// Only a suggestion, and only when the user hasn't already told us
		// a price — gora8's pricing is per-agent, not per-capability (see
		// card.YAMLPricing), so this necessarily reads as one reference
		// point (the first derived capability), not a per-operation quote.
		suggestPriceFromReference(client, agentConfig)
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

	// --host/--host-image means the real endpoint doesn't exist yet — only
	// known once hosting provisions it, later in this same command. The
	// A2A card needs a non-empty url to validate, but the API's own
	// endpoint-reachability check (assertSafeEndpointUrl, routes/agents.ts)
	// only runs when Endpoint is non-empty — so unlike the card, the
	// actual deploy request keeps Endpoint genuinely blank rather than a
	// placeholder that would just fail that reachability check instead.
	// cardConfig is a shallow copy specifically so this placeholder never
	// leaks into agentConfig.Endpoint, which req.Endpoint below still
	// reads. This placeholder never needs a follow-up fix once hosting
	// resolves the real endpoint, checked rather than assumed: every
	// surface a real caller ever sees (routes/agents.ts's /publish,
	// /agents/:id/did.json, /agents/:id/registration.json) is rebuilt
	// fresh from the live Agent row on every request — /publish always
	// advertises gora8's own stable invoke-gateway URL, never the raw
	// endpoint directly — and the a2a_card blob this placeholder lands in
	// is written once at deploy and never read back by anything server-side.
	// It's genuinely dead data, not a discoverability bug.
	cardConfig := agentConfig
	if (deployHost || deployHostImage != "") && agentConfig.Endpoint == "" {
		placeholder := *agentConfig
		placeholder.Endpoint = "https://pending.gora8.com/" + slugify(agentConfig.Name)
		cardConfig = &placeholder
		ui.Info("No endpoint set — using a placeholder A2A card URL until hosting provisions one below.")
	}

	fmt.Println()

	// Step 1: Generate A2A card.
	spin1 := ui.NewSpinner("Generating A2A agent card...")
	spin1.Start()
	time.Sleep(300 * time.Millisecond) // Brief visual pause for UX.
	a2aCard := card.Generate(cardConfig)
	if err := card.Validate(a2aCard); err != nil {
		spin1.Fail("A2A card validation failed")
		return fmt.Errorf("invalid agent config: %w", err)
	}
	spin1.Stop("A2A agent card generated")

	// Step 1.5: Generate the agent's own EVM and Solana wallets locally —
	// gora8 never generates or holds either key (see
	// SELF_CUSTODY_ARCHITECTURE.md in the gora8 monorepo). Named after the
	// agent so `gora8-signer start <name>` (run wherever the agent's own
	// endpoint actually lives — often a different machine than the one
	// running this deploy) can find the same keys again.
	signerName := slugify(agentConfig.Name)
	var walletAddress, solanaWalletAddress string
	if deployWalletAddress != "" {
		walletAddress = deployWalletAddress
		solanaWalletAddress = deploySolanaWalletAddress
		ui.Info(fmt.Sprintf("Using provided wallet address: %s", walletAddress))
		if solanaWalletAddress == "" {
			ui.Warning("No --solana-wallet-address given — this agent's Solana wallet will be custodied by gora8 until you provide one.")
		}
	} else {
		spinSigner := ui.NewSpinner("Generating agent wallet (gora8-signer)...")
		spinSigner.Start()
		evmAddress, solAddress, err := initSigner(signerName)
		if err != nil {
			spinSigner.Fail("Couldn't generate a wallet locally")
			return fmt.Errorf(
				"gora8-signer init failed: %w\n\nInstall it first: npm install -g gora8-signer (requires Node.js), "+
					"or run 'npx gora8-signer init %s' yourself and pass the addresses with --wallet-address/--solana-wallet-address", err, signerName,
			)
		}
		walletAddress = evmAddress
		solanaWalletAddress = solAddress
		if solanaWalletAddress != "" {
			spinSigner.Stop(fmt.Sprintf("Wallets generated: %s (EVM), %s (Solana)", walletAddress, solanaWalletAddress))
		} else {
			spinSigner.Stop(fmt.Sprintf("Wallet generated: %s", walletAddress))
		}
	}

	// Step 2: Register the agent (identity, wallet, and spending policy are
	// all provisioned by this one call) — or, if agent.yaml already
	// remembers an id from a prior successful deploy (written back below),
	// update that agent instead. Without this branch, every retry —
	// including the ordinary "fix a config error and re-run" case —
	// silently registered a brand-new on-chain ERC-8004 identity for the
	// same wallet, spending real relay-wallet gas each time and leaving
	// the old agent id orphaned. Policy is deliberately not part of the
	// update path — 'gora8 policy set' owns ongoing policy changes, so a
	// redeploy can never silently overwrite a policy tuned from the
	// dashboard.
	var (
		agentID    string
		dashURL    string
		walletOut  string
		mandate    *api.MandateSyncResult
		chains     []api.ChainActivation
		actorRef   *api.ActorRef
		statusText string
	)

	if agentConfig.ID != "" {
		spin2 := ui.NewSpinner("Updating agent...")
		spin2.Start()
		updateReq := &api.UpdateAgentRequest{
			Name:        agentConfig.Name,
			Description: agentConfig.Description,
			Endpoint:    agentConfig.Endpoint,
			Pricing: api.Pricing{
				Model:    agentConfig.Pricing.Model,
				Amount:   agentConfig.Pricing.Amount,
				Currency: agentConfig.Pricing.Currency,
			},
		}
		for _, cap := range agentConfig.Capabilities {
			updateReq.Capabilities = append(updateReq.Capabilities, api.Capability{
				ID:          cap.ID,
				Description: cap.Description,
			})
		}
		agent, err := client.UpdateAgent(agentConfig.ID, updateReq)
		if err != nil {
			spin2.Fail("Update failed")
			return err
		}
		spin2.Stop("Agent updated")
		agentID, dashURL, walletOut, actorRef, statusText = agent.ID, agent.DashboardURL, walletAddress, agent.ActorRef, agent.Status
	} else {
		spin2 := ui.NewSpinner("Registering agent...")
		spin2.Start()

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
			Registries:          agentConfig.Registries,
			A2ACard:             a2aCard,
			WalletAddress:       walletAddress,
			SolanaWalletAddress: solanaWalletAddress,
		}
		for _, cap := range agentConfig.Capabilities {
			req.Capabilities = append(req.Capabilities, api.Capability{
				ID:          cap.ID,
				Description: cap.Description,
			})
		}

		resp, err := client.DeployAgent(req)
		if err != nil {
			spin2.Fail("Deployment failed")
			return err
		}
		spin2.Stop("Agent registered — identity, wallet, and spending Mandate attached")
		agentID, dashURL, walletOut = resp.Agent.ID, resp.DashboardURL, resp.WalletAddr
		mandate, chains, actorRef, statusText = resp.Mandate, resp.Chains, resp.Agent.ActorRef, resp.Agent.Status

		// Remember this agent's id so a future `gora8 deploy` here updates
		// it instead of minting a brand-new on-chain identity.
		agentConfig.ID = agentID
		if err := saveAgentYAML(agentFilePath, agentConfig); err != nil {
			ui.Warning(fmt.Sprintf(
				"Deployed, but couldn't save the agent id back to %s: %v — future deploys here will register a new agent unless you add `id: %s` to it yourself.",
				agentFilePath, err, agentID,
			))
		}
	}

	// Step 3: Publish to the configured audiences (gora8's own directory by
	// default — free and instant, no external dependency).
	spin3 := ui.NewSpinner(fmt.Sprintf("Publishing to %s...", strings.Join(agentConfig.Registries, ", ")))
	spin3.Start()
	if _, err := client.PublishAgent(agentID, &api.PublishRequest{Registries: agentConfig.Registries}); err != nil {
		spin3.Fail("Publish failed — run 'gora8 publish " + agentID + "' to retry")
	} else {
		spin3.Stop("Published")
	}

	// Step 3.5: Pro-tier hosted compute (see docs/hosting/gora8-managed-fargate.md)
	// — only runs at all if --host/--host-image was passed; runHostFlow
	// itself no-ops otherwise. Same "don't fail the whole deploy" pattern
	// as publish above: the agent is already real and usable at this
	// point (self-hosted, if endpoint was set in agent.yaml) even if
	// hosting specifically fails.
	if err := runHostFlow(client, agentID, searchPath); err != nil {
		ui.Warning(fmt.Sprintf("Hosting failed: %v — the agent itself deployed fine; retry hosting with the API directly once fixed.", err))
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
		{"Agent ID", agentID},
	}
	if statusText != "" {
		details = append(details, []string{"Status", ui.StatusColor(statusText)})
	}
	if walletOut != "" {
		details = append(details, []string{"Wallet", walletOut})
	}
	if mandate != nil {
		details = append(details, []string{"Mandate", mandate.MandateID})
		if mandate.Enforcement != nil {
			if mandate.Enforcement.Delegated {
				details = append(details, []string{"Enforcement", ui.Green("active")})
			} else {
				details = append(details, []string{"Enforcement", ui.Yellow("not yet active — run `gora8 mandate issue-onchain " + agentID + "`")})
			}
		}
	}
	if actorRef != nil {
		// Not populated by the current API — see the ActorRef doc comment
		// in internal/api/client.go. Rendered defensively so this command
		// doesn't need another code change the day it is.
		details = append(details, []string{"Identity", fmt.Sprintf("%s:%s", actorRef.Namespace, actorRef.ActorID)})
	}
	if dashURL == "" && agentID != "" {
		dashURL = fmt.Sprintf("https://app.gora8.com/agents/%s", agentID)
	}
	details = append(details, []string{"Dashboard", ui.Cyan(dashURL)})

	for _, d := range details {
		fmt.Printf("  %s  %s\n", ui.Dim(fmt.Sprintf("%-12s", d[0])), d[1])
	}
	fmt.Println()

	// One command, multiple chains: report the automatic rollout the same
	// deploy call just triggered (api/src/lib/chains.ts's
	// getAutoRolloutChains() — Ethereum isn't in it, see `gora8 chains`).
	if len(chains) > 0 {
		active, awaitingGas := 0, 0
		for _, c := range chains {
			if c.Status == "active" {
				active++
			} else if c.Status == "awaiting_gas" {
				awaitingGas++
			}
		}
		ui.Info(fmt.Sprintf("Rolled out to %d/%d additional chains.", active, len(chains)))
		if awaitingGas > 0 {
			ui.Warning(fmt.Sprintf("%d chain(s) need the agent wallet funded with native gas — see `gora8 wallet fund` and `gora8 chains list`.", awaitingGas))
		}
	}

	ui.Info("Run `gora8 agents list` to see all your agents.")
	ui.Info(fmt.Sprintf("Run `gora8 mandate %s` to see and verify its spending Mandate.", agentID))
	ui.Info("Run `gora8 chains list` to see every supported chain, and `gora8 chains add` to opt into Ethereum.")
	if deployWalletAddress == "" {
		ui.Info(fmt.Sprintf(
			"Wherever %s actually runs (if that's not this machine), run `GORA8_AGENT_ID=%s npx gora8-signer start %s` there too — "+
				"that's what holds the key and answers gora8's signing requests. GORA8_AGENT_ID must be set to exactly "+
				"this (gora8's own id, not the local key name) or the signer can never fetch its Mandate.",
			agentConfig.Name, agentID, signerName,
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
// signerInitResult mirrors gora8-signer init's JSON stdout shape
// (cli.ts) — {evmAddress, solanaAddress}, both generated together now
// that Solana self-custody exists alongside EVM's.
type signerInitResult struct {
	EvmAddress    string `json:"evmAddress"`
	SolanaAddress string `json:"solanaAddress"`
}

// initSigner shells out to gora8-signer (an npm package — see
// SELF_CUSTODY_ARCHITECTURE.md and signer-ts/ in the gora8 monorepo)
// to generate the agent's own EVM and Solana keys locally and return
// both addresses. Uses `npx --yes` rather than requiring a prior global
// install, same convention as setupAgentSDK's npm/pip calls below.
// Neither key itself ever touches this process's stdout/stderr or any
// gora8 API call — only the two derived addresses do.
func initSigner(name string) (evmAddress string, solanaAddress string, err error) {
	cmd := exec.Command("npx", "--yes", "gora8-signer", "init", name)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", "", fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", "", err
	}
	var result signerInitResult
	if err := json.Unmarshal(output, &result); err != nil {
		return "", "", fmt.Errorf("gora8-signer init produced unparseable output: %w", err)
	}
	if result.EvmAddress == "" {
		return "", "", fmt.Errorf("gora8-signer init produced no EVM address")
	}
	// A missing Solana address degrades to gora8-side custody for that
	// one wallet (see services/deploy.ts's DeployInput doc comment) —
	// worth a clear message here rather than a silent empty field,
	// since it's the one case this CLI can't fully guarantee self-custody.
	if result.SolanaAddress == "" {
		fmt.Fprintln(os.Stderr, "Warning: gora8-signer init produced no Solana address — this agent's Solana wallet will be custodied by gora8 until you re-run with an updated gora8-signer.")
	}
	return result.EvmAddress, result.SolanaAddress, nil
}

// pluralY returns "y" for a count of 1, "ies" otherwise — "1 capability"
// vs "3 capabilities".
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func joinCapabilityIDs(caps []openapi.Capability) string {
	ids := make([]string, len(caps))
	for i, c := range caps {
		ids[i] = c.ID
	}
	return strings.Join(ids, ", ")
}

// suggestPriceFromReference looks up GET /v1/market/price-reference for
// the first --from-openapi-derived capability and prints what comparable
// gora8-native agents charge. Deliberately informational only, never
// mutates agentConfig.Pricing.Amount itself — gora8's pricing is one
// amount per agent, not per capability (card.YAMLPricing), so this is
// necessarily a single reference point, not a real per-operation quote,
// and a market-derived guess overwriting a price the developer actually
// meant to set (even agent.example.yaml ships with a non-empty default)
// is worse than not offering one; --price is the explicit, unambiguous
// way to set it. Never fails the deploy — same "best-effort" pattern as
// publish/hosting below.
func suggestPriceFromReference(client *api.Client, agentConfig *card.AgentYAML) {
	reference := agentConfig.Capabilities[0].ID
	resp, err := client.GetPriceReference(reference)
	if err != nil {
		ui.Warning(fmt.Sprintf("Couldn't fetch a price reference for %q: %v — set --price yourself.", reference, err))
		return
	}
	if resp.SampleSize == 0 || resp.Median == nil {
		ui.Info(fmt.Sprintf("No comparable gora8 agents offer %q yet, so there's no price reference — using agent.yaml's own pricing (%s %s).", reference, agentConfig.Pricing.Amount, agentConfig.Pricing.Currency))
		return
	}
	ui.Info(fmt.Sprintf(
		"Price reference for %q (%d comparable agent%s): median %.2f, range %.2f–%.2f %s. "+
			"agent.yaml currently sets %s — pass --price %.2f to use the reference instead.",
		reference, resp.SampleSize, pluralS(resp.SampleSize), *resp.Median, *resp.Min, *resp.Max, agentConfig.Pricing.Currency,
		agentConfig.Pricing.Amount, *resp.Median,
	))
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
	// Returns the command's combined stdout+stderr alongside the error, so a
	// double failure below can show *why* rather than just that it failed —
	// e.g. pip refusing with PEP 668's "externally-managed-environment" on a
	// Homebrew Python was previously swallowed entirely (cmd.Run() discards
	// output that isn't explicitly wired up), leaving only a generic
	// "Couldn't install automatically" with no way to tell what to fix.
	runIn := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	switch {
	case hasFile("package.json"):
		spin := ui.NewSpinner("Installing gora8-agent (npm)...")
		spin.Start()
		if out, err := runIn("npm", "install", "gora8-agent", "--save"); err != nil {
			spin.Fail("Couldn't install gora8-agent automatically")
			if out != "" {
				ui.Info(out)
			}
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
		if _, err := runIn("pip", append([]string{"install", "-q"}, pkgs...)...); err != nil {
			if out, err := runIn("pip3", append([]string{"install", "-q"}, pkgs...)...); err != nil {
				spin.Fail("Couldn't install gora8-agent automatically")
				if out != "" {
					ui.Info(out)
				}
				ui.Info("Added to requirements.txt — run: pip install -r requirements.txt")
				return
			}
		}
		spin.Stop(strings.Join(pkgs, " ") + " installed and added to requirements.txt")

	case hasFile("pyproject.toml"):
		pkgs := pipPackagesFor(filepath.Join(dir, "pyproject.toml"))
		spin := ui.NewSpinner("Installing gora8-agent (pip)...")
		spin.Start()
		if _, err := runIn("pip", append([]string{"install", "-q"}, pkgs...)...); err != nil {
			if out, err := runIn("pip3", append([]string{"install", "-q"}, pkgs...)...); err != nil {
				spin.Fail("Couldn't install gora8-agent automatically")
				if out != "" {
					ui.Info(out)
				}
				ui.Info("Run manually: pip install " + strings.Join(pkgs, " "))
				return
			}
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

// saveAgentYAML writes agentCfg back to filePath — used once, right after
// a successful first deploy, purely to persist the id gora8 just assigned
// so a later `gora8 deploy` here updates this agent instead of registering
// a new one. Re-marshals the whole file (same pattern `gora8 init` already
// uses), so hand-added comments/formatting in an existing agent.yaml won't
// survive — an accepted tradeoff for a file this small and machine-owned.
func saveAgentYAML(filePath string, agentCfg *card.AgentYAML) error {
	out, err := yaml.Marshal(agentCfg)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filePath, err)
	}
	if err := os.WriteFile(filePath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}
	return nil
}

func init() {
	deployCmd.Flags().StringVar(&deployName, "name", "", "Override the agent name")
	deployCmd.Flags().StringVar(&deployCapabilities, "capabilities", "", "Comma-separated capability IDs to set")
	deployCmd.Flags().StringVar(&deployPrice, "price", "", "Override the price per task (e.g. 0.50)")
	deployCmd.Flags().StringVar(&deployRegistries, "registries", "gora8", "Comma-separated audiences to publish to on deploy")
	deployCmd.Flags().StringVar(&deployWalletAddress, "wallet-address", "", "Use an already-generated EVM address instead of running gora8-signer init locally (e.g. if you generated it elsewhere)")
	deployCmd.Flags().StringVar(&deploySolanaWalletAddress, "solana-wallet-address", "", "Use an already-generated Solana address instead of running gora8-signer init locally — only read when --wallet-address is also set")
	deployCmd.Flags().StringVar(&deployFromOpenAPI, "from-openapi", "", "Derive capabilities from an OpenAPI spec file (JSON or YAML) instead of hand-authoring them in agent.yaml — pair with gora8_adapters.rest (adapters-python) to actually run the wrapper this deploys")
	deployCmd.Flags().StringVar(&deployOperations, "operations", "", "Comma-separated operationIds to expose from --from-openapi (default: every operation the spec declares)")
}
