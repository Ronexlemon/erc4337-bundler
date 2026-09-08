package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"

	"erc4337-bundler/internal/validation"
	"erc4337-bundler/pkg/types"

	"github.com/ethereum/go-ethereum"
	//"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	//"github.com/ethereum/go-ethereum/crypto"
)

type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}



func (b *Bundler) HandleRPC(w http.ResponseWriter, r *http.Request) {
	var req RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case "eth_sendUserOperation":
		b.handleSendUserOperation(w, req)
	case "eth_estimateUserOperationGas":
		b.handleEstimateGas(w, req)
	case "eth_getUserOperationReceipt":
		b.handleGetReceipt(w, req)
	case "eth_getTransactionReceipt":
		b.handleGetTransactionReceipt(w, req)
	case "eth_supportedEntryPoints":
		writeResult(w, req.ID, []string{b.entryPoint.Hex()})
	default:
		writeError(w, req.ID, -32601, "method not found")
	}
}

func (b *Bundler) handleSendUserOperation(w http.ResponseWriter, req RPCRequest) {
	var params []json.RawMessage
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}

	var rpcOp types.RPCUserOperation
	if err := json.Unmarshal(params[0], &rpcOp); err != nil {
		writeError(w, req.ID, -32602, "invalid UserOperation")
		return
	}
	op := rpcOp.ToUserOperation()

	ctx := context.Background()
	result, err := validation.SimulateValidation(ctx, b.client, b.entryPoint, b.entryPointABI, op)
	if err != nil {
		writeError(w, req.ID, -32500, "validation failed: "+err.Error())
		return
	}
	if result.SigFailed {
		writeError(w, req.ID, -32507, "signature error")
		return
	}

	if err := b.mempool.Add(op); err != true {
		writeError(w, req.ID, -32500, "failed to add to mempool: ")
		return
	}

	hash := types.GetUserOpHash(op, b.entryPoint, b.chainID)
	writeResult(w, req.ID, hash.Hex())
}

// handleEstimateGas estimates the three UserOperation gas fields:
// preVerificationGas, verificationGasLimit, and callGasLimit.
//
// verificationGasLimit is taken from SimulateValidation's reported
// preOpGas (which covers validation overhead). callGasLimit is estimated
// by simulating the call to `sender` with `callData` via eth_estimateGas.
// preVerificationGas covers fixed L1 calldata/overhead costs and, absent a
// full calldata-cost model here, is left to a caller-supplied default —

func (b *Bundler) handleEstimateGas(w http.ResponseWriter, req RPCRequest) {
	var params []json.RawMessage
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}

	var rpcOp types.RPCUserOperation
	if err := json.Unmarshal(params[0], &rpcOp); err != nil {
		writeError(w, req.ID, -32602, "invalid UserOperation")
		return
	}
	op := rpcOp.ToUserOperation()

	ctx := context.Background()

	// verificationGasLimit: derive from a validation-only simulation.
	valResult, err := validation.SimulateValidation(ctx, b.client, b.entryPoint, b.entryPointABI, op)
	if err != nil {
		writeError(w, req.ID, -32500, "validation failed: "+err.Error())
		return
	}
	if valResult.SigFailed {
		writeError(w, req.ID, -32507, "signature error")
		return
	}

	// callGasLimit: estimate gas for sender.execute(callData) directly.
	sender := op.Sender
	callMsg := ethereum.CallMsg{
		From: b.entryPoint, // EntryPoint is the actual caller of the wallet during execution
		To:   &sender,
		Data: op.CallData,
	}
	callGas, err := b.client.EstimateGas(ctx, callMsg)
	if err != nil {
		writeError(w, req.ID, -32500, "callGasLimit estimation failed: "+err.Error())
		return
	}

	// TODO: preVerificationGas should be computed from the ABI-encoded
	// UserOperation's calldata cost (zero/non-zero byte gas costs per the
	// EntryPoint's calldata-cost model) plus a fixed overhead constant.
	// Left as a conservative placeholder here since that model isn't
	// defined in this package yet.
	preVerificationGas := big.NewInt(21000)

	resp := map[string]string{
		"preVerificationGas":   preVerificationGas.String(),
		"verificationGasLimit": valResult.PreOpGas.String(),
		"callGasLimit":         fmt.Sprintf("0x%x", callGas),
	}
	writeResult(w, req.ID, resp)
}

// handleGetReceipt looks up a UserOperationEvent emitted by the EntryPoint
// matching the given userOpHash, then attaches the underlying transaction
// receipt. Returns a nil result (not an error) if no matching event is
// found yet, per the eth_getUserOperationReceipt spec.
func (b *Bundler) handleGetReceipt(w http.ResponseWriter, req RPCRequest) {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}
	userOpHash := common.HexToHash(params[0])

	ctx := context.Background()

	eventABI, ok := b.entryPointABI.Events["UserOperationEvent"]
	if !ok {
		writeError(w, req.ID, -32603, "UserOperationEvent not found in ABI")
		return
	}

	query := ethereum.FilterQuery{
		Addresses: []common.Address{b.entryPoint},
		Topics: [][]common.Hash{
			{eventABI.ID},
			{userOpHash},
		},
	}

	logs, err := b.client.FilterLogs(ctx, query)
	if err != nil {
		writeError(w, req.ID, -32603, "log filter failed: "+err.Error())
		return
	}
	if len(logs) == 0 {
		// Not found (yet) — spec says return null, not an error.
		writeResult(w, req.ID, nil)
		return
	}

	logEntry := logs[len(logs)-1] // most recent match
	receipt, err := b.client.TransactionReceipt(ctx, logEntry.TxHash)
	if err != nil {
		writeError(w, req.ID, -32603, "failed to fetch tx receipt: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"userOpHash":      userOpHash.Hex(),
		"sender":          common.HexToAddress(logEntry.Topics[1].Hex()).Hex(),
		"nonce":           logEntry.Topics[2].Big().String(),
		"transactionHash": logEntry.TxHash.Hex(),
		"blockHash":       logEntry.BlockHash.Hex(),
		"blockNumber":     fmt.Sprintf("0x%x", logEntry.BlockNumber),
		"logs":            logs,
		"receipt":         receipt,
	}
	writeResult(w, req.ID, resp)
}

func (b *Bundler) handleGetTransactionReceipt(w http.ResponseWriter, req RPCRequest) {
	var params []string
	if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
		writeError(w, req.ID, -32602, "invalid params")
		return
	}
 
	txHash := common.HexToHash(params[0])
 
	ctx := context.Background()
	receipt, err := b.client.TransactionReceipt(ctx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			// Not mined (or doesn't exist) — spec says return null, not an error.
			writeResult(w, req.ID, nil)
			return
		}
		writeError(w, req.ID, -32603, "failed to fetch tx receipt: "+err.Error())
		return
	}
 
	writeResult(w, req.ID, receipt)
}



func writeResult(w http.ResponseWriter, id json.RawMessage, result interface{}) {
	resp := RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	// JSON-RPC errors are still transported over HTTP 200 by convention;
	// change this if your client expects HTTP-level error codes instead.
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}