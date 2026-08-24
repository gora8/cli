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

	"github.com/gora8/cli/internal/config"
)

// Client is an HTTP client for the gora8 API.
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
	respBody, err := c.doRaw(method, path, body)
	if err != nil {
		return err
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// doRaw executes a request and returns the raw response body — for
// non-JSON responses (e.g. the wallet CSV export) that `do`'s JSON
// decoding can't handle.
func (c *Client) doRaw(method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gora8/"+Version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// A 401 with no API key set (send-otp/verify-otp, before any credential
	// exists) means the endpoint itself rejected the request — e.g. a wrong
	// OTP code — not that the CLI is unauthenticated. Only rewrite the
	// message to the generic "run auth login" hint when we actually had a
	// key and the server rejected it.
	if resp.StatusCode == http.StatusUnauthorized && c.apiKey != "" {
		return nil, &APIError{
			StatusCode: 401,
			Message:    "Not authenticated. Run: gora8 auth login",
			Body:       string(respBody),
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &APIError{
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
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
			Body:       string(respBody),
		}
	}

	return respBody, nil
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

// ── Device authorization flow (RFC 8628) ────────────────────────────────────
// Same pattern the Vercel/GitHub/Stripe CLIs use: get a code pair with no
// account context, send the human to a browser to tie it to a real account,
// poll until that happens. Works over SSH/headless sessions too, since the
// browser doesn't have to run on the same machine as the CLI.

// DeviceAuthorizeResponse is the response from POST /v1/auth/device/authorize.
type DeviceAuthorizeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DeviceAuthorize starts a new device authorization request. No API key is
// required — this is a public, unauthenticated endpoint.
func (c *Client) DeviceAuthorize() (*DeviceAuthorizeResponse, error) {
	var res DeviceAuthorizeResponse
	if err := c.do("POST", "/v1/auth/device/authorize", map[string]string{}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// DeviceTokenResponse is the response from POST /v1/auth/device/token, once
// approved.
type DeviceTokenResponse struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Plan  string `json:"plan"`
	} `json:"user"`
	APIKey string `json:"apiKey"`
}

// DeviceToken polls for the result of a device authorization. Returns
// (nil, nil) while still pending — that's the expected, common case during
// polling, not an error. A returned error means a terminal failure
// (expired, denied, or rate-limited) that the caller should stop polling on.
func (c *Client) DeviceToken(deviceCode string) (*DeviceTokenResponse, error) {
	respBody, err := c.doRaw("POST", "/v1/auth/device/token", map[string]string{"device_code": deviceCode})
	if err != nil {
		if apiErr, ok := err.(*APIError); ok {
			var body struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal([]byte(apiErr.Body), &body)
			if body.Error == "authorization_pending" || body.Error == "slow_down" {
				return nil, nil
			}
		}
		return nil, err
	}
	var res DeviceTokenResponse
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &res, nil
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

	// ActorRef and MandateID reference gora8's own design described in
	// TARGET_STATE.md (ERC-8004-referenced identity, on-chain
	// Authority). Both are `omitempty`/pointer, and both can genuinely be
	// absent even on a real, successful deploy — ActorRef because
	// ERC-8004 registration is best-effort and non-blocking (see
	// services/deploy.ts's registerIdentity() call — a missing
	// RELAY_WALLET_PRIVATE_KEY on the API side is the expected failure
	// mode right now, not a bug), MandateID because on-chain Mandate
	// issuance is a separate, owner-triggered action (`POST
	// /v1/agents/:id/mandate/issue-onchain`), not automatic at deploy
	// time — see that route's own doc comment for why. Nil/empty here
	// means "not yet issued/registered on-chain," not "absent from the
	// protocol" — the did:web identity and off-chain signed Mandate are
	// both already real and independently verifiable regardless.
	ActorRef *ActorRef `json:"actor_ref,omitempty"`
}

// ActorRef identifies an actor by reference to an external identity
// registry rather than a gora8-issued ID — see TARGET_STATE.md's
// Identity section, and gora8's own SPEC.md §2.
// gora8 still issues a did:web identity directly (Agent.DID, unaffected —
// see TARGET_STATE.md's Identity primitive on why did:web keeps working
// as one of the ERC-8004 registration document's endpoint types rather
// than being replaced), and now also best-effort mints a real ERC-8004
// identity at deploy time (services/deploy.ts's registerIdentity() call)
// — this is populated once that succeeds, nil/omitted until then or if
// it hasn't been attempted (e.g. RELAY_WALLET_PRIVATE_KEY not yet
// configured server-side). Field names match the API's snake_case
// convention used everywhere else (wallet_address, dashboard_url, etc.),
// not the camelCase this struct originally guessed at before the API
// side existed.
type ActorRef struct {
	Namespace string `json:"namespace"`
	Registry  string `json:"registry"`
	ActorID   string `json:"actor_id"`
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

// AcceptanceLimits control which INBOUND calls this agent takes — checked
// before settlement, before the call reaches the owner's endpoint. Not a
// financial safeguard (being paid isn't a risk); it throttles call
// volume/workload exposure and flags unusually large individual jobs for
// review (ApprovalThreshold).
type AcceptanceLimits struct {
	PerTransactionLimit float64 `json:"perTransactionLimit,omitempty"`
	DailyCap            float64 `json:"dailyCap,omitempty"`
	MonthlyCap          float64 `json:"monthlyCap,omitempty"`
	ApprovalThreshold   float64 `json:"approvalThreshold,omitempty"`
}

// SpendingLimits control how much this agent may pay OUT of its own wallet
// when it hires another agent (see `gora8 wallet transactions` for hire
// records) — the real financial guardrail, unlike AcceptanceLimits.
type SpendingLimits struct {
	PerTransactionLimit float64 `json:"perTransactionLimit,omitempty"`
	DailyCap            float64 `json:"dailyCap,omitempty"`
	MonthlyCap          float64 `json:"monthlyCap,omitempty"`
}

// PolicyConfig fields are sent/received as real JSON numbers, not strings —
// the backend's enforcement checks require `typeof x === "number"`.
//
// PATCH /v1/agents/:id/policy replaces the whole stored policy, not a
// partial merge — AllowedCounterparties is here (even though the CLI has
// no flag to set it) purely so a round-trip GetPolicy -> modify -> SetPolicy
// doesn't silently wipe out a value set from the web dashboard.
type PolicyConfig struct {
	Suspended             bool              `json:"suspended,omitempty"`
	Acceptance            *AcceptanceLimits `json:"acceptance,omitempty"`
	Spending              *SpendingLimits   `json:"spending,omitempty"`
	Currency              string            `json:"currency,omitempty"`
	AllowedCounterparties []string          `json:"allowedCounterparties,omitempty"`
}

// DeployResponse is the response from POST /v1/agents.
type DeployResponse struct {
	Agent        Agent  `json:"agent"`
	DashboardURL string `json:"dashboard_url"`
	WalletAddr   string `json:"wallet_address"`
	// Mandate enforcement activation is automatic and best-effort at
	// deploy time (see TARGET_STATE.md's Authority section) — nil here
	// means the sync itself failed (logged server-side, not the agent's
	// fault), not that it was skipped.
	Mandate *MandateSyncResult `json:"mandate,omitempty"`
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
	Registry string   `json:"registry"`
	Status   string   `json:"status"`
	URL      string   `json:"url"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
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

type InspectCaller struct {
	Counterparty string  `json:"counterparty"`
	Name         *string `json:"name"`
	Calls        int     `json:"calls"`
	TotalPaid    float64 `json:"total_paid"`
}

type InspectCapability struct {
	Capability string `json:"capability"`
	Calls      int    `json:"calls"`
}

type InspectResult struct {
	AgentID            string              `json:"agent_id"`
	AgentName          string              `json:"agent_name"`
	Status             string              `json:"status"`
	SuccessRate        *float64            `json:"success_rate"`
	ErrorRate          *float64            `json:"error_rate"`
	AvgResponseMs      *int                `json:"avg_response_ms"`
	TotalCalls         int                 `json:"total_calls"`
	EarningsTotal      float64             `json:"earnings_total"`
	AvgRevenuePerCall  *float64            `json:"avg_revenue_per_call"`
	WalletBalance      float64             `json:"wallet_balance"`
	WalletStaked       float64             `json:"wallet_staked"`
	OpenDisputes       int                 `json:"open_disputes"`
	TopCallers30d      []InspectCaller     `json:"top_callers_30d"`
	TopCapabilities30d []InspectCapability `json:"top_capabilities_30d"`
}

func (c *Client) InspectAgent(agentID string) (*InspectResult, error) {
	var result InspectResult
	if err := c.do("GET", "/v1/agents/"+agentID+"/inspect", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
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

// ── Policy ───────────────────────────────────────────────────────────────────

type PolicyResponse struct {
	AgentID string       `json:"agent_id"`
	Policy  PolicyConfig `json:"policy"`
	// A policy edit changes the agent's mandateId, so every update
	// automatically resyncs on-chain Mandate enforcement to match — see
	// DeployResponse.Mandate's doc comment above. Only present on
	// PATCH responses (GET /policy doesn't touch the chain).
	Mandate *MandateSyncResult `json:"mandate,omitempty"`
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
	// Set only for a real agent-to-agent hire (see POST /v1/agents/:id/hire)
	// — empty for an external caller paying in or a withdrawal to an
	// arbitrary address.
	CounterpartyAgentID   string `json:"counterparty_agent_id"`
	CounterpartyAgentName string `json:"counterparty_agent_name"`
	Capability            string `json:"capability"`
	TxHash                string `json:"tx_hash"`
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

// ExportWalletCSV returns a real CSV of every earnings + withdrawal
// transaction — for tax/accounting records. agentID scopes to one agent
// (empty = every agent the user owns); year is an explicit calendar year
// (empty = all-time).
func (c *Client) ExportWalletCSV(agentID, year string) ([]byte, error) {
	path := "/v1/wallets/export"
	if agentID != "" {
		path = "/v1/agents/" + agentID + "/wallet/export"
	}
	if year != "" {
		path += "?year=" + url.QueryEscape(year)
	}
	return c.doRaw("GET", path, nil)
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

// Passport is deliberately map[string]interface{}, not a typed struct —
// it's a signed document meant to be verified/consumed as-is (and
// forward-compatible with fields added later), not decomposed into Go
// fields that could silently drop something out of the signed payload.
type Passport = map[string]interface{}

func (c *Client) GetPassport(agentID string) (Passport, error) {
	var result Passport
	if err := c.do("GET", "/v1/agents/"+agentID+"/passport", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Mandate is deliberately map[string]interface{}, for the same reason as
// Passport above: it's a signed document (see gora8's own SPEC.md §4.2,
// "Mandate object") meant to be verified/consumed as-is, not
// decomposed into Go fields that could silently drop something out of the
// signed payload.
type Mandate = map[string]interface{}

// GetMandate fetches an agent's current spending Mandate — a public,
// unauthenticated endpoint (see gora8's own SPEC.md §4):
// a counterparty doesn't need a gora8 account to verify an agent's
// spending authority before dealing with it. Authenticated here anyway
// since every other CLI command requires it, but the endpoint itself
// imposes no such requirement.
func (c *Client) GetMandate(agentID string) (Mandate, error) {
	var result Mandate
	if err := c.do("GET", "/v1/agents/"+agentID+"/mandate", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// EnforcementStatus reports whether an agent's wallet is actually
// delegated to MandateEnforcer (see contracts/src/MandateEnforcer.sol) —
// a Mandate can be issued and verifiable on AuthorityRegistry without yet
// being enforced this way, which is why the two are reported separately
// rather than collapsed into one status.
type EnforcementStatus struct {
	Delegated bool   `json:"delegated"`
	TxHash    string `json:"txHash,omitempty"`
	Error     string `json:"error,omitempty"`
}

// MandateSyncResult is the shape returned by every endpoint that
// activates/resyncs on-chain Mandate enforcement — POST
// /v1/agents/:id/mandate/issue-onchain, and (embedded in their own
// response) POST /v1/agents (deploy) and PATCH /v1/agents/:id/policy.
// Issuing on AuthorityRegistry and activating enforcement are two
// different guarantees, reported separately — see TARGET_STATE.md's
// Authority section: a Mandate that's issued but not yet delegated to
// MandateEnforcer is still only verifiable, not enforced.
type MandateSyncResult struct {
	MandateID   string             `json:"mandateId"`
	Status      string             `json:"status"` // "issued" | "already-issued"
	Enforcement *EnforcementStatus `json:"enforcement,omitempty"`
}

// IssueMandateOnChain resyncs the agent's *current* Mandate (by
// mandateId, a hash of its agentId+policy) on AuthorityRegistry and, if
// needed, re-points its delegated wallet at it — see
// TARGET_STATE.md's Authority section and AuthorityRegistry.sol. This
// makes revocation status checkable in one on-chain call instead of only
// via this API's own signature.
//
// Not the primary way enforcement gets activated — `gora8 deploy` and
// `gora8 policy set` already trigger this automatically, best-effort, so
// an agent is enforcement-active without its owner ever needing to know
// this command exists. This remains as an explicit, loud retry: if that
// automatic sync failed (e.g. a transient RPC hiccup), this surfaces the
// real error instead of the deploy/policy call's own best-effort silence.
func (c *Client) IssueMandateOnChain(agentID string) (*MandateSyncResult, error) {
	var result MandateSyncResult
	if err := c.do("POST", "/v1/agents/"+agentID+"/mandate/issue-onchain", nil, &result); err != nil {
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

// ── Notifications ────────────────────────────────────────────────────────────
// Includes approval_required — the real signal for a call blocked by an
// agent's policy.approvalThreshold (see `gora8 policy set --approval-above`).
// There's no separate hold-and-approve-later flow: a blocked call is
// rejected immediately, and this is how you find out it happened.

type Notification struct {
	ID        string  `json:"id"`
	AgentID   *string `json:"agent_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Read      bool    `json:"read"`
	CreatedAt string  `json:"created_at"`
}

type NotificationsResponse struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unread_count"`
}

func (c *Client) ListNotifications() (*NotificationsResponse, error) {
	var resp NotificationsResponse
	if err := c.do("GET", "/v1/notifications", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) MarkNotificationRead(id string) error {
	return c.do("POST", "/v1/notifications/"+id+"/read", nil, nil)
}

func (c *Client) MarkAllNotificationsRead() error {
	return c.do("POST", "/v1/notifications/read-all", nil, nil)
}

type CheckoutResponse struct {
	URL string `json:"url"`
}

// CreateCheckoutSession returns a real Stripe Checkout URL — the CLI
// itself never touches payment details, it just opens the browser to
// Stripe's own hosted page. This is free-tier users' one path off the CLI
// while still being free-tier (the web dashboard proper is blocked for
// them, see api/src/middleware/auth.ts).
func (c *Client) CreateCheckoutSession() (*CheckoutResponse, error) {
	var resp CheckoutResponse
	if err := c.do("POST", "/v1/billing/checkout", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type OnrampResponse struct {
	URL string `json:"url"`
}

// CreateOnrampSession returns a real Coinbase Onramp checkout URL, scoped
// to this agent's own wallet address(es) — the CLI itself never touches
// card details or KYC, it just opens the browser to Coinbase's hosted page.
// Completion is reported to the API asynchronously via webhook, not by this
// call, so the caller has to poll the wallet balance separately to notice
// funds landing.
func (c *Client) CreateOnrampSession(agentID string) (*OnrampResponse, error) {
	var resp OnrampResponse
	if err := c.do("POST", "/v1/agents/"+agentID+"/wallet/fund", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
