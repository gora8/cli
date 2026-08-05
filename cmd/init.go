package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gora8/cli/internal/card"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init [directory]",
	Short: "Scaffold an agent.yaml from the project already in this directory",
	Long: `Scaffold an agent.yaml for gora8 deploy.

init looks at what's already in the given directory (defaults to the
current one) — requirements.txt, pyproject.toml, package.json — and
detects which agent framework you're using, then pre-fills what it can
(name, a starter capability). Endpoint, description, and pricing are
business decisions it can't guess, so it asks for those directly.

This does not deploy anything — run 'gora8 deploy' afterward once the
generated agent.yaml looks right.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	for _, candidate := range []string{"agent.yaml", "agent.yml"} {
		if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
			return fmt.Errorf("%s already exists in %s — remove it first or edit it directly", candidate, dir)
		}
	}

	ui.Header("Scaffolding agent.yaml")

	framework := detectFramework(dir)
	if framework != "" {
		ui.Info(fmt.Sprintf("Detected framework: %s", framework))
	} else {
		ui.Info("No known framework detected — falling back to generic defaults")
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	defaultName := humanizeDirName(filepath.Base(absDir))

	reader := bufio.NewReader(os.Stdin)

	name := promptWithDefault(reader, "Agent name", defaultName)
	description := promptWithDefault(reader, "Description", defaultDescription(framework))
	endpoint := promptWithDefault(reader, "Public HTTPS endpoint (where this agent receives requests)", "")
	for endpoint == "" {
		ui.Warning("Endpoint is required — gora8 needs a reachable URL to route requests to.")
		endpoint = promptWithDefault(reader, "Public HTTPS endpoint", "")
	}

	capID := promptWithDefault(reader, "Primary capability id (e.g. research.web)", "task.default")
	capDesc := promptWithDefault(reader, "Capability description", "Handles incoming tasks")

	model := promptPricingModel(reader)
	amount := ""
	currency := "USD"
	if model != "free" {
		amount = promptWithDefault(reader, "Price per unit (e.g. 0.50)", "0.50")
		currency = promptWithDefault(reader, "Currency", "USD")
	}

	agentConfig := &card.AgentYAML{
		Name:        name,
		Description: description,
		Version:     "1.0.0",
		Endpoint:    endpoint,
		Capabilities: []card.YAMLCapability{
			{ID: capID, Description: capDesc},
		},
		Pricing: card.YAMLPricing{
			Model:    model,
			Amount:   amount,
			Currency: currency,
		},
		Registries: []string{"gora8"},
	}

	out, err := yaml.Marshal(agentConfig)
	if err != nil {
		return fmt.Errorf("marshal agent.yaml: %w", err)
	}

	outPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	ui.Success(fmt.Sprintf("Wrote %s", outPath))
	ui.Info("Review it, then run: gora8 deploy " + dir)
	return nil
}

// detectFramework inspects common dependency manifests for known agent
// framework names. Best-effort only — a miss just falls back to generic
// defaults, it never blocks scaffolding.
func detectFramework(dir string) string {
	type marker struct {
		file   string
		needle string
		label  string
	}
	markers := []marker{
		{"requirements.txt", "agno", "Agno"},
		{"requirements.txt", "langchain", "LangChain"},
		{"requirements.txt", "crewai", "CrewAI"},
		{"requirements.txt", "autogen", "AutoGen"},
		{"requirements.txt", "llama-index", "LlamaIndex"},
		{"pyproject.toml", "agno", "Agno"},
		{"pyproject.toml", "langchain", "LangChain"},
		{"pyproject.toml", "crewai", "CrewAI"},
		{"pyproject.toml", "autogen", "AutoGen"},
		{"pyproject.toml", "llama-index", "LlamaIndex"},
		{"package.json", "langchain", "LangChain.js"},
		{"package.json", "@langchain/core", "LangChain.js"},
	}
	for _, m := range markers {
		data, err := os.ReadFile(filepath.Join(dir, m.file))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(data)), m.needle) {
			return m.label
		}
	}
	return ""
}

func defaultDescription(framework string) string {
	if framework == "" {
		return ""
	}
	return fmt.Sprintf("An AI agent built with %s", framework)
}

// humanizeDirName turns "my-cool-agent" or "my_cool_agent" into "My Cool Agent".
func humanizeDirName(name string) string {
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	words := strings.Fields(name)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func promptWithDefault(reader *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("  %s [%s]: ", label, def)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func promptPricingModel(reader *bufio.Reader) string {
	for {
		fmt.Println("  Pricing model:")
		fmt.Println("    1) per-call     — flat fee per invocation")
		fmt.Println("    2) per-token    — charged by token usage")
		fmt.Println("    3) subscription — monthly flat rate")
		fmt.Println("    4) free         — no charge to callers")
		choice := promptWithDefault(reader, "Choose 1-4", "1")
		switch strings.TrimSpace(choice) {
		case "1", "per-call":
			return "per-call"
		case "2", "per-token":
			return "per-token"
		case "3", "subscription":
			return "subscription"
		case "4", "free":
			return "free"
		default:
			ui.Warning("Please enter 1, 2, 3, or 4.")
		}
	}
}
