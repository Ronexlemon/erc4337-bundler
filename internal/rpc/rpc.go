package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"erc4337-bundler/internal/mempool"
	"erc4337-bundler/internal/validation"
	"erc4337-bundler/pkg/types"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
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

type Bundler struct {
	mempool       *mempool.Mempool
	entryPoint    common.Address
	client        *ethclient.Client
	entryPointABI abi.ABI
	chainID       *big.Int
	// signer key intentionally omitted here — used only where transactions
	// are actually submitted (e.g. a separate submission/bundling loop),
	// not needed for the RPC handlers below.
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

	hash := GetUserOpHash(op, b.entryPoint, b.chainID)
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

// GetUserOpHash computes the EIP-4337 userOpHash:
//
//	userOpHash = keccak256(abi.encode(hash(pack(op)), entryPoint, chainId))
//
// where pack(op) ABI-encodes the op with initCode/callData/paymasterAndData
// replaced by their keccak256 hashes (per the EntryPoint reference impl).
func GetUserOpHash(op types.UserOperation, entryPoint common.Address, chainID *big.Int) common.Hash {
	packed, err := packUserOp(op)
	if err != nil {
		// Hashing a well-formed UserOperation should never fail; a failure
		// here indicates a static ABI-encoding bug, not a runtime/user error.
		panic(fmt.Sprintf("packUserOp: %v", err))
	}
	opHash := crypto.Keccak256(packed)

	addressTy, _ := abi.NewType("address", "", nil)
	bytes32Ty, _ := abi.NewType("bytes32", "", nil)
	uint256Ty, _ := abi.NewType("uint256", "", nil)

	outerArgs := abi.Arguments{
		{Type: bytes32Ty},
		{Type: addressTy},
		{Type: uint256Ty},
	}

	var opHash32 [32]byte
	copy(opHash32[:], opHash)

	encoded, err := outerArgs.Pack(opHash32, entryPoint, chainID)
	if err != nil {
		panic(fmt.Sprintf("pack userOpHash outer encoding: %v", err))
	}

	return common.BytesToHash(crypto.Keccak256(encoded))
}

// packUserOp ABI-encodes the UserOperation with its dynamic-length fields
// (initCode, callData, paymasterAndData) replaced by their keccak256 hashes,
// matching EntryPoint.sol's internal `pack` function.
func packUserOp(op types.UserOperation) ([]byte, error) {
	addressTy, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, err
	}
	uint256Ty, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	bytes32Ty, err := abi.NewType("bytes32", "", nil)
	if err != nil {
		return nil, err
	}

	args := abi.Arguments{
		{Type: addressTy}, // sender
		{Type: uint256Ty}, // nonce
		{Type: bytes32Ty}, // keccak256(initCode)
		{Type: bytes32Ty}, // keccak256(callData)
		{Type: uint256Ty}, // callGasLimit
		{Type: uint256Ty}, // verificationGasLimit
		{Type: uint256Ty}, // preVerificationGas
		{Type: uint256Ty}, // maxFeePerGas
		{Type: uint256Ty}, // maxPriorityFeePerGas
		{Type: bytes32Ty}, // keccak256(paymasterAndData)
	}

	var hashInitCode, hashCallData, hashPaymasterAndData [32]byte
	copy(hashInitCode[:], crypto.Keccak256(op.InitCode))
	copy(hashCallData[:], crypto.Keccak256(op.CallData))
	copy(hashPaymasterAndData[:], crypto.Keccak256(op.PaymasterAndData))

	return args.Pack(
		op.Sender,
		op.Nonce,
		hashInitCode,
		hashCallData,
		op.CallGasLimit,
		op.VerificationGasLimit,
		op.PreVerificationGas,
		op.MaxFeePerGas,
		op.MaxPriorityFeePerGas,
		hashPaymasterAndData,
	)
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