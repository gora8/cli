package card

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A2ACard is a valid A2A (Agent-to-Agent) agent card.
type A2ACard struct {
	ProtocolVersion    string           `json:"protocolVersion"`
	Name               string           `json:"name"`
	Description        string           `json:"description"`
	URL                string           `json:"url"`
	Version            string           `json:"version"`
	Capabilities       CardCapabilities `json:"capabilities"`
	Skills             []Skill          `json:"skills"`
	DefaultInputModes  []string         `json:"defaultInputModes"`
	DefaultOutputModes []string         `json:"defaultOutputModes"`
	Provider           *Provider        `json:"provider,omitempty"`
}

// CardCapabilities describes what the agent can do at the protocol level.
type CardCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
}

// Skill represents a single capability the agent offers.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// Provider holds optional provider metadata.
type Provider struct {
	Organization string `json:"organization,omitempty"`
	URL          string `json:"url,omitempty"`
}

// AgentYAML mirrors the structure of agent.yaml for card generation.
type AgentYAML struct {
	// ID is absent from a freshly-scaffolded agent.yaml — `gora8 deploy`
	// writes it back in after the first successful registration, so a
	// later `gora8 deploy` on the same project updates that agent instead
	// of registering a brand-new one (see cmd/deploy.go's runDeploy).
	ID           string           `yaml:"id,omitempty" json:"id,omitempty"`
	Name         string           `yaml:"name"        json:"name"`
	Description  string           `yaml:"description" json:"description"`
	Version      string           `yaml:"version"     json:"version"`
	Endpoint     string           `yaml:"endpoint"    json:"endpoint"`
	Capabilities []YAMLCapability `yaml:"capabilities" json:"capabilities"`
	Pricing      YAMLPricing      `yaml:"pricing"     json:"pricing"`
	Policy       YAMLPolicy       `yaml:"policy"      json:"policy"`
	Registries   []string         `yaml:"registries"  json:"registries"`
}

type YAMLCapability struct {
	ID          string `yaml:"id"          json:"id"`
	Description string `yaml:"description" json:"description"`
}

type YAMLPricing struct {
	Model    string `yaml:"model"    json:"model"`
	Amount   string `yaml:"amount"   json:"amount"`
	Currency string `yaml:"currency" json:"currency"`
}

// Which INBOUND calls this agent takes — not a financial safeguard (being
// paid isn't a risk), it throttles call volume/workload exposure. See
// YAMLSpending for the real financial guardrail (outbound hiring spend).
type YAMLAcceptance struct {
	PerTransactionLimit float64 `yaml:"per_transaction_limit,omitempty" json:"perTransactionLimit,omitempty"`
	DailyCap            float64 `yaml:"daily_cap,omitempty"             json:"dailyCap,omitempty"`
	MonthlyCap          float64 `yaml:"monthly_cap,omitempty"           json:"monthlyCap,omitempty"`
	ApprovalThreshold   float64 `yaml:"approval_above,omitempty"        json:"approvalThreshold,omitempty"`
}

// How much this agent may pay OUT of its own wallet when it hires another
// agent to help complete a task — real money leaving the wallet.
type YAMLSpending struct {
	PerTransactionLimit float64 `yaml:"per_transaction_limit,omitempty" json:"perTransactionLimit,omitempty"`
	DailyCap            float64 `yaml:"daily_cap,omitempty"             json:"dailyCap,omitempty"`
	MonthlyCap          float64 `yaml:"monthly_cap,omitempty"           json:"monthlyCap,omitempty"`
}

type YAMLPolicy struct {
	Acceptance YAMLAcceptance `yaml:"acceptance" json:"acceptance"`
	Spending   YAMLSpending   `yaml:"spending"   json:"spending"`
	Currency   string         `yaml:"currency"   json:"currency"`
}

// Generate produces a valid A2A agent card from an AgentYAML definition.
func Generate(agent *AgentYAML) *A2ACard {
	skills := make([]Skill, 0, len(agent.Capabilities))
	for _, cap := range agent.Capabilities {
		skill := Skill{
			ID:          cap.ID,
			Name:        humanizeName(cap.ID),
			Description: cap.Description,
			Tags:        inferTags(cap.ID),
		}
		skills = append(skills, skill)
	}

	version := agent.Version
	if version == "" {
		version = "1.0.0"
	}

	return &A2ACard{
		ProtocolVersion: "1.0",
		Name:            agent.Name,
		Description:     agent.Description,
		URL:             agent.Endpoint,
		Version:         version,
		Capabilities: CardCapabilities{
			Streaming:         false,
			PushNotifications: false,
		},
		Skills:             skills,
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}
}

// Validate checks that the required fields of an A2ACard are present and valid.
func Validate(card *A2ACard) error {
	if card.Name == "" {
		return fmt.Errorf("agent card is missing 'name'")
	}
	if card.URL == "" {
		return fmt.Errorf("agent card is missing 'url' (endpoint)")
	}
	if card.ProtocolVersion == "" {
		return fmt.Errorf("agent card is missing 'protocolVersion'")
	}
	if len(card.Skills) == 0 {
		return fmt.Errorf("agent card has no skills/capabilities defined")
	}
	return nil
}

// ToJSON serialises the card to indented JSON.
func (c *A2ACard) ToJSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// humanizeName converts a dotted capability ID like "research.web" into a
// human-readable name like "Web Research".
func humanizeName(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) >= 2 {
		// Reverse and title-case each part.
		out := make([]string, len(parts))
		for i, p := range parts {
			out[len(parts)-1-i] = strings.Title(p) //nolint:staticcheck
		}
		return strings.Join(out, " ")
	}
	return strings.Title(id) //nolint:staticcheck
}

// inferTags extracts tag words from a dotted capability ID.
func inferTags(id string) []string {
	return strings.Split(id, ".")
}
