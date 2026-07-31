package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentplane/cli/internal/config"
)

// Client is an HTTP client for the agentplane API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a new API client from the given config.
func New(cfg *config.Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.APIURL, "/"),
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// APIError represents a structured error from the API.
type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}

// do executes a request and decodes the JSON response into out (if non-nil).
func (c *Client) do(method, path string, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agentctl/"+Version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return &APIError{
			StatusCode: 401,
			Message:    "Not authenticated. Run: agentctl auth login",
			Body:       string(respBody),
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return &APIError{
			StatusCode: 404,
			Message:    "Resource not found. Check the ID and try again.",
			Body:       string(respBody),
		}
	}

	if resp.StatusCode >= 400 {
		// Try to extract message from API response.
		var apiErr struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if json.Unmarshal(respBody, &apiErr) == nil {
			if apiErr.Message != "" {
				msg = apiErr.Message
			} else if apiErr.Error != "" {
				msg = apiErr.Error
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
			Body:       string(respBody),
		}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// ── Auth ─────────────────────────────────────────────────────────────────────

// MeResponse is the response from GET /v1/me.
type MeResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Plan  string `json:"plan"`
}

func (c *Client) GetMe() (*MeResponse, error) {
	var me MeResponse
	if err := c.do("GET", "/v1/me", nil, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// ── Agents ───────────────────────────────────────────────────────────────────

// Agent represents an agent resource.
type Agent struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Endpoint     string  `json:"endpoint"`
	DashboardURL string  `json:"dashboard_url"`
	Earnings30d  float64 `json:"earnings_30d"`
	Transactions int     `json:"transactions"`
	LastActive   string  `json:"last_active"`
	CreatedAt    string  `json:"created_at"`
}

// DeployRequest is the payload for POST /v1/agents.
type DeployRequest struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Version      string       `json:"version"`
	Endpoint     string       `json:"endpoint"`
	Capabilities []Capability `json:"capabilities"`
	Pricing      Pricing      `json:"pricing"`
	Policy       PolicyConfig `json:"policy"`
	Registries   []string     `json:"registries"`
	A2ACard      interface{}  `json:"a2a_card,omitempty"`
}

type Capability struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

type Pricing struct {
	Model    string `json:"model"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type PolicyConfig struct {
	LimitPerTransaction string `json:"limit_per_transaction"`
	LimitDaily          string `json:"limit_daily"`
	LimitMonthly        string `json:"limit_monthly,omitempty"`
	ApprovalAbove       string `json:"approval_above,omitempty"`
	Currency            string `json:"currency"`
}

// DeployResponse is the response from POST /v1/agents.
type DeployResponse struct {
	Agent        Agent  `json:"agent"`
	DashboardURL string `json:"dashboard_url"`
	WalletAddr   string `json:"wallet_address"`
}

func (c *Client) DeployAgent(req *DeployRequest) (*DeployResponse, error) {
	var resp DeployResponse
	if err := c.do("POST", "/v1/agents", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListAgents() ([]Agent, error) {
	var result struct {
		Agents []Agent `json:"agents"`
	}
	if err := c.do("GET", "/v1/agents", nil, &result); err != nil {
		return nil, err
	}
	return result.Agents, nil
}

func (c *Client) GetAgent(id string) (*Agent, error) {
	var agent Agent
	if err := c.do("GET", "/v1/agents/"+id, nil, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

func (c *Client) PauseAgent(id string) error {
	return c.do("POST", "/v1/agents/"+id+"/pause", nil, nil)
}

func (c *Client) ResumeAgent(id string) error {
	return c.do("POST", "/v1/agents/"+id+"/resume", nil, nil)
}

func (c *Client) DeleteAgent(id string) error {
	return c.do("DELETE", "/v1/agents/"+id, nil, nil)
}

// ── Publish ──────────────────────────────────────────────────────────────────

type PublishRequest struct {
	Registries []string `json:"registries"`
}

type PublishResult struct {
	Registry string `json:"registry"`
	Status   string `json:"status"`
	URL      string `json:"url"`
	Error    string `json:"error,omitempty"`
}

type PublishResponse struct {
	Results []PublishResult `json:"results"`
}

func (c *Client) PublishAgent(agentID string, req *PublishRequest) (*PublishResponse, error) {
	var resp PublishResponse
	if err := c.do("POST", "/v1/agents/"+agentID+"/publish", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListRegistries returns the currently available publish audiences from the
// server, rather than hardcoding a name list in the CLI.
func (c *Client) ListRegistries() ([]string, error) {
	var result struct {
		Registries []string `json:"registries"`
	}
	if err := c.do("GET", "/v1/registries", nil, &result); err != nil {
		return nil, err
	}
	return result.Registries, nil
}

// ── Earnings ─────────────────────────────────────────────────────────────────

type EarningsPeriod struct {
	Label        string  `json:"label"`
	Amount       float64 `json:"amount"`
	Transactions int     `json:"transactions"`
}

type EarningsResponse struct {
	AgentID      string           `json:"agent_id"`
	AgentName    string           `json:"agent_name"`
	Currency     string           `json:"currency"`
	Total        float64          `json:"total"`
	Periods      []EarningsPeriod `json:"periods"`
	Transactions int              `json:"transactions"`
}

func (c *Client) GetEarnings(agentID, period string) (*EarningsResponse, error) {
	path := "/v1/earnings"
	if agentID != "" {
		path = "/v1/agents/" + agentID + "/earnings"
	}
	if period != "" {
		path += "?period=" + url.QueryEscape(period)
	}
	var resp EarningsResponse
	if err := c.do("GET", path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── SEO ──────────────────────────────────────────────────────────────────────

type SEOIssue struct {
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

type SEOSuggestion struct {
	Priority    string `json:"priority"`
	Description string `json:"description"`
}

type SEOResponse struct {
	AgentID     string          `json:"agent_id"`
	AgentName   string          `json:"agent_name"`
	Score       int             `json:"score"`
	Issues      []SEOIssue      `json:"issues"`
	Suggestions []SEOSuggestion `json:"suggestions"`
}

func (c *Client) GetSEO(agentID string) (*SEOResponse, error) {
	var resp SEOResponse
	if err := c.do("GET", "/v1/agents/"+agentID+"/seo", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── Policy ───────────────────────────────────────────────────────────────────

type PolicyResponse struct {
	AgentID string       `json:"agent_id"`
	Policy  PolicyConfig `json:"policy"`
}

func (c *Client) GetPolicy(agentID string) (*PolicyResponse, error) {
	var resp PolicyResponse
	if err := c.do("GET", "/v1/agents/"+agentID+"/policy", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) SetPolicy(agentID string, policy *PolicyConfig) (*PolicyResponse, error) {
	var resp PolicyResponse
	if err := c.do("PATCH", "/v1/agents/"+agentID+"/policy", policy, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ── Approvals ────────────────────────────────────────────────────────────────

type Approval struct {
	ID           string  `json:"id"`
	AgentID      string  `json:"agent_id"`
	AgentName    string  `json:"agent_name"`
	Counterparty string  `json:"counterparty"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Capability   string  `json:"capability"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
}

type ApprovalsResponse struct {
	Approvals []Approval `json:"approvals"`
}

func (c *Client) ListApprovals(status string) ([]Approval, error) {
	path := "/v1/approvals"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var resp ApprovalsResponse
	if err := c.do("GET", path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Approvals, nil
}

func (c *Client) ApproveApproval(id string) error {
	return c.do("POST", "/v1/approvals/"+id+"/approve", nil, nil)
}

func (c *Client) DenyApproval(id string) error {
	return c.do("POST", "/v1/approvals/"+id+"/deny", nil, nil)
}

// ── Wallet ───────────────────────────────────────────────────────────────────

type Wallet struct {
	AgentID   string  `json:"agent_id"`
	AgentName string  `json:"agent_name"`
	Address   string  `json:"address"`
	Balance   float64 `json:"balance"`
	Pending   float64 `json:"pending"`
	Currency  string  `json:"currency"`
	Network   string  `json:"network"`
}

type WalletTransaction struct {
	ID           string  `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Direction    string  `json:"direction"` // "in" | "out"
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Counterparty string  `json:"counterparty"`
	Capability   string  `json:"capability"`
	TxHash       string  `json:"tx_hash"`
}

type WithdrawRequest struct {
	Amount    string `json:"amount"`
	ToAddress string `json:"to_address"`
}

type WithdrawResult struct {
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	ToAddress string `json:"to_address"`
	TxHash    string `json:"tx_hash"`
	Status    string `json:"status"`
}

func (c *Client) GetWallet(agentID string) (*Wallet, error) {
	var w Wallet
	if err := c.do("GET", "/v1/agents/"+agentID+"/wallet", nil, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

func (c *Client) ListWallets() ([]Wallet, error) {
	var result struct {
		Wallets []Wallet `json:"wallets"`
	}
	if err := c.do("GET", "/v1/wallets", nil, &result); err != nil {
		return nil, err
	}
	return result.Wallets, nil
}

func (c *Client) ListWalletTransactions(agentID string) ([]WalletTransaction, error) {
	var result struct {
		Transactions []WalletTransaction `json:"transactions"`
	}
	if err := c.do("GET", "/v1/agents/"+agentID+"/wallet/transactions", nil, &result); err != nil {
		return nil, err
	}
	return result.Transactions, nil
}

func (c *Client) WithdrawFunds(agentID, amount, toAddress string) (*WithdrawResult, error) {
	req := &WithdrawRequest{Amount: amount, ToAddress: toAddress}
	var result WithdrawResult
	if err := c.do("POST", "/v1/agents/"+agentID+"/wallet/withdraw", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ── Identity ─────────────────────────────────────────────────────────────────

type Identity struct {
	AgentID     string      `json:"agent_id"`
	AgentName   string      `json:"agent_name"`
	DID         string      `json:"did"`
	Method      string      `json:"method"`
	DocumentURL string      `json:"document_url"`
	KeyType     string      `json:"key_type"`
	KeyID       string      `json:"key_id"`
	CreatedAt   string      `json:"created_at"`
	LastRotated string      `json:"last_rotated"`
	Document    interface{} `json:"document"`
}

type DIDVerifyResult struct {
	Valid        bool     `json:"valid"`
	Method       string   `json:"method"`
	Endpoint     string   `json:"endpoint"`
	KeyType      string   `json:"key_type"`
	Capabilities []string `json:"capabilities"`
	Error        string   `json:"error,omitempty"`
}

type KeyRotateResult struct {
	NewKeyID     string `json:"new_key_id"`
	OldKeyExpiry string `json:"old_key_expiry"`
	DocumentURL  string `json:"document_url"`
}

func (c *Client) GetIdentity(agentID string) (*Identity, error) {
	var id Identity
	if err := c.do("GET", "/v1/agents/"+agentID+"/identity", nil, &id); err != nil {
		return nil, err
	}
	return &id, nil
}

func (c *Client) VerifyDID(did string) (*DIDVerifyResult, error) {
	var result DIDVerifyResult
	if err := c.do("GET", "/v1/identity/verify?did="+did, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) RotateKeys(agentID string) (*KeyRotateResult, error) {
	var result KeyRotateResult
	if err := c.do("POST", "/v1/agents/"+agentID+"/identity/rotate", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ── Logs ─────────────────────────────────────────────────────────────────────

type LogEntry struct {
	ID           string  `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Counterparty string  `json:"counterparty"`
	Capability   string  `json:"capability"`
	Status       string  `json:"status"`
	Duration     int     `json:"duration_ms"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Summary      string  `json:"summary"`
}

type LogsResponse struct {
	Entries []LogEntry `json:"entries"`
}

func (c *Client) GetLogs(agentID string, tail int) ([]LogEntry, error) {
	path := fmt.Sprintf("/v1/agents/%s/logs?tail=%d", agentID, tail)
	var resp LogsResponse
	if err := c.do("GET", path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}
