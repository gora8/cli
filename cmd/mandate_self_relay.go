package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	selfRelayTo     string
	selfRelayAmount string
	selfRelayRPCURL string
	selfRelayGasKey string
)

// execute() takes the same five arguments contracts/src/MandateEnforcer.sol
// declares — see that contract's own doc comment for why (address to,
// uint256 value, bytes data, uint256 executeNonce, bytes signature).
var executeABI = mustParseExecuteABI()

func mustParseExecuteABI() abi.ABI {
	const executeABIJSON = `[{
		"name": "execute", "type": "function", "stateMutability": "nonpayable",
		"inputs": [
			{"name": "to", "type": "address"},
			{"name": "value", "type": "uint256"},
			{"name": "data", "type": "bytes"},
			{"name": "executeNonce", "type": "uint256"},
			{"name": "signature", "type": "bytes"}
		],
		"outputs": [{"type": "bytes"}]
	}]`
	parsed, err := abi.JSON(strings.NewReader(executeABIJSON))
	if err != nil {
		panic("mandate_self_relay: invalid embedded ABI: " + err.Error())
	}
	return parsed
}

var mandateSelfRelayCmd = &cobra.Command{
	Use:   "self-relay <agent-id>",
	Short: "Withdraw USDC without gora8 ever submitting the transaction or paying gas",
	Long: `Withdraws USDC from an agent's wallet the same way 'gora8 mandate' shows
its spending authority: through MandateEnforcer.execute(), the same
EIP-7702 enforcement path every other agent spend goes through — not a
separate mechanism.

gora8 is only in one step of this: asking the agent's own signer for its
EIP-712 authorization (the signer refuses to sign anything not requested
by gora8's own authenticated key — that's a deliberate hardening, not
something this command works around). Submitting the signed transaction
and paying its gas happen entirely on your own machine, against whatever
RPC endpoint you give this command, using a gas-paying key gora8 never
sees. This is what makes "CLI works with zero gora8 backend dependency"
true for money leaving a wallet, not just for reads.

Talks to your RPC endpoint directly over plain JSON-RPC (a handful of
net/http calls) rather than pulling in a full Ethereum client library —
this command only ever needs to read a nonce/base fee and submit one
signed transaction, not run a node.

The gas-paying key comes from GORA8_SELF_RELAY_GAS_KEY (recommended —
never lands in shell history) or --gas-key. It pays this transaction's
gas only; it is never sent to gora8 and never needs to hold the
withdrawn USDC itself.

Example:
  export GORA8_SELF_RELAY_GAS_KEY=0x...
  gora8 mandate self-relay agt_abc123 \
    --to 0xRecipient... --amount 25.00 \
    --rpc-url https://sepolia.base.org`,
	Args: cobra.ExactArgs(1),
	RunE: runMandateSelfRelay,
}

func init() {
	mandateSelfRelayCmd.Flags().StringVar(&selfRelayTo, "to", "", "Recipient EVM address (required)")
	mandateSelfRelayCmd.Flags().StringVar(&selfRelayAmount, "amount", "", "USDC amount, decimal (required)")
	mandateSelfRelayCmd.Flags().StringVar(&selfRelayRPCURL, "rpc-url", "", "RPC endpoint to submit against (required)")
	mandateSelfRelayCmd.Flags().StringVar(&selfRelayGasKey, "gas-key", "", "Gas-paying private key (hex) — prefer GORA8_SELF_RELAY_GAS_KEY instead")
	_ = mandateSelfRelayCmd.MarkFlagRequired("to")
	_ = mandateSelfRelayCmd.MarkFlagRequired("amount")
	_ = mandateSelfRelayCmd.MarkFlagRequired("rpc-url")
	mandateCmd.AddCommand(mandateSelfRelayCmd)
}

func runMandateSelfRelay(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	gasKeyHex := selfRelayGasKey
	if gasKeyHex == "" {
		gasKeyHex = os.Getenv("GORA8_SELF_RELAY_GAS_KEY")
	}
	if gasKeyHex == "" {
		return fmt.Errorf("no gas-paying key: set GORA8_SELF_RELAY_GAS_KEY (preferred) or pass --gas-key")
	}
	gasKey, err := crypto.HexToECDSA(strings.TrimPrefix(gasKeyHex, "0x"))
	if err != nil {
		return fmt.Errorf("--gas-key/GORA8_SELF_RELAY_GAS_KEY isn't a valid private key: %w", err)
	}
	gasPayer := crypto.PubkeyToAddress(gasKey.PublicKey)

	apiClient := api.New(cfg)
	spin := ui.NewSpinner("Requesting a signed authorization from gora8...")
	spin.Start()
	auth, err := apiClient.SelfRelayWithdraw(agentID, selfRelayAmount, selfRelayTo)
	if err != nil {
		spin.Fail("Signing request failed")
		return err
	}
	spin.Stop("Got a signed authorization — submitting it ourselves from here")

	value, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return fmt.Errorf("server returned an unparseable value: %q", auth.Value)
	}
	executeNonce, ok := new(big.Int).SetString(auth.ExecuteNonce, 10)
	if !ok {
		return fmt.Errorf("server returned an unparseable execute_nonce: %q", auth.ExecuteNonce)
	}
	if !common.IsHexAddress(auth.To) || !common.IsHexAddress(auth.Wallet) {
		return fmt.Errorf("server returned an invalid address")
	}

	calldata, err := executeABI.Pack("execute", common.HexToAddress(auth.To), value, common.FromHex(auth.Data), executeNonce, common.FromHex(auth.Signature))
	if err != nil {
		return fmt.Errorf("encode execute() calldata: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rpc := &jsonRPCClient{url: selfRelayRPCURL, httpClient: &http.Client{Timeout: 20 * time.Second}}

	chainID, err := rpc.chainID(ctx)
	if err != nil {
		return fmt.Errorf("read chain id from %s: %w", selfRelayRPCURL, err)
	}
	if chainID.Int64() != auth.ChainID {
		return fmt.Errorf("--rpc-url is chain %d, but this authorization was signed for chain %d — they must match", chainID.Int64(), auth.ChainID)
	}

	nonce, err := rpc.pendingNonceAt(ctx, gasPayer)
	if err != nil {
		return fmt.Errorf("read %s's nonce: %w", gasPayer.Hex(), err)
	}
	tipCap, baseFee, err := rpc.suggestFees(ctx)
	if err != nil {
		return fmt.Errorf("read gas fee data: %w", err)
	}
	// 2x the current base fee as headroom against the next few blocks —
	// same margin api/src/lib/viem-tx.ts's boostedFees() uses server-side
	// for the exact same execute() call, so a self-relayed submission
	// isn't meaningfully more likely to get stuck than the relayed path.
	feeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), tipCap)

	wallet := common.HexToAddress(auth.Wallet)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: tipCap,
		GasFeeCap: feeCap,
		Gas:       300000, // generous fixed cap — execute() does one ERC-20 transfer plus its own Mandate check, never close to this
		To:        &wallet,
		Value:     big.NewInt(0),
		Data:      calldata,
	})

	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, gasKey)
	if err != nil {
		return fmt.Errorf("sign transaction: %w", err)
	}
	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return fmt.Errorf("encode signed transaction: %w", err)
	}

	spin2 := ui.NewSpinner("Submitting to " + selfRelayRPCURL + "...")
	spin2.Start()
	if err := rpc.sendRawTransaction(ctx, rawTx); err != nil {
		spin2.Fail("Submission failed")
		return err
	}

	status, err := rpc.waitForReceipt(ctx, signedTx.Hash())
	if err != nil {
		spin2.Fail("Never confirmed")
		return err
	}
	if status == 0 {
		spin2.Fail("Transaction reverted on-chain")
		return fmt.Errorf("execute() reverted — tx %s", signedTx.Hash().Hex())
	}
	spin2.Stop("Confirmed")

	fmt.Println()
	ui.Success(fmt.Sprintf("Withdrew %s USDC to %s", selfRelayAmount, selfRelayTo))
	ui.Info("Tx hash:    " + signedTx.Hash().Hex())
	ui.Info("Gas payer:  " + gasPayer.Hex())
	ui.Info("gora8 never saw this submission or paid its gas.")
	return nil
}

// jsonRPCClient is deliberately minimal — plain net/http + encoding/json
// against the standard Ethereum JSON-RPC methods, not a full client
// library. This command only ever needs five calls; pulling in
// go-ethereum's ethclient (and, transitively, its full node-oriented rpc
// package — websocket transport, OpenTelemetry, Prometheus metrics, none
// of which this CLI has any use for) would multiply this binary's
// dependency footprint for zero functional benefit. core/types/crypto/
// accounts/abi above are used unchanged — this only replaces the network
// transport, not transaction construction, signing, or ABI encoding.
type jsonRPCClient struct {
	url        string
	httpClient *http.Client
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *jsonRPCClient) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	var parsed rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	if parsed.Error != nil {
		return fmt.Errorf("%s: RPC error %d: %s", method, parsed.Error.Code, parsed.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return fmt.Errorf("%s: decode result: %w", method, err)
		}
	}
	return nil
}

func hexToBigInt(s string) (*big.Int, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		s = "0"
	}
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return nil, fmt.Errorf("not a valid hex quantity: %q", s)
	}
	return n, nil
}

func (c *jsonRPCClient) chainID(ctx context.Context) (*big.Int, error) {
	var hex string
	if err := c.call(ctx, "eth_chainId", nil, &hex); err != nil {
		return nil, err
	}
	return hexToBigInt(hex)
}

func (c *jsonRPCClient) pendingNonceAt(ctx context.Context, addr common.Address) (uint64, error) {
	var hex string
	if err := c.call(ctx, "eth_getTransactionCount", []interface{}{addr.Hex(), "pending"}, &hex); err != nil {
		return 0, err
	}
	n, err := hexToBigInt(hex)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// suggestFees returns (priority fee, base fee) — a plain
// eth_maxPriorityFeePerGas plus the latest block's baseFeePerGas, the
// same two inputs any EIP-1559 fee calculation needs. Falls back to a
// fixed 1 gwei tip if the RPC doesn't support eth_maxPriorityFeePerGas
// (some don't) — generous enough on an L2 like Base Sepolia without
// requiring a second, more complex fee-history call.
func (c *jsonRPCClient) suggestFees(ctx context.Context) (tipCap *big.Int, baseFee *big.Int, err error) {
	var tipHex string
	if err := c.call(ctx, "eth_maxPriorityFeePerGas", nil, &tipHex); err != nil {
		tipCap = big.NewInt(1_000_000_000) // 1 gwei fallback
	} else {
		tipCap, err = hexToBigInt(tipHex)
		if err != nil {
			return nil, nil, err
		}
	}

	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := c.call(ctx, "eth_getBlockByNumber", []interface{}{"latest", false}, &block); err != nil {
		return nil, nil, err
	}
	if block.BaseFeePerGas == "" {
		return nil, nil, fmt.Errorf("latest block has no baseFeePerGas — this chain doesn't appear to support EIP-1559")
	}
	baseFee, err = hexToBigInt(block.BaseFeePerGas)
	if err != nil {
		return nil, nil, err
	}
	return tipCap, baseFee, nil
}

func (c *jsonRPCClient) sendRawTransaction(ctx context.Context, raw []byte) error {
	return c.call(ctx, "eth_sendRawTransaction", []interface{}{"0x" + hex.EncodeToString(raw)}, nil)
}

// waitForReceipt polls eth_getTransactionReceipt until it's non-null,
// then returns its status field (1 success, 0 reverted) — the two
// outcomes the caller actually needs to distinguish.
func (c *jsonRPCClient) waitForReceipt(ctx context.Context, txHash common.Hash) (uint64, error) {
	for {
		var receipt *struct {
			Status string `json:"status"`
		}
		if err := c.call(ctx, "eth_getTransactionReceipt", []interface{}{txHash.Hex()}, &receipt); err != nil {
			return 0, err
		}
		if receipt != nil {
			status, err := strconv.ParseUint(strings.TrimPrefix(receipt.Status, "0x"), 16, 64)
			if err != nil {
				return 0, fmt.Errorf("unparseable receipt status %q: %w", receipt.Status, err)
			}
			return status, nil
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("timed out waiting for %s to confirm", txHash.Hex())
		case <-time.After(2 * time.Second):
		}
	}
}
