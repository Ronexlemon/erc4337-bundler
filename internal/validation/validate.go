package validation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	"erc4337-bundler/pkg/types"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
)

type ValidationResult struct {
	PreOpGas      *big.Int
	Prefund       *big.Int
	SigFailed     bool
	ValidAfter    uint64
	ValidUntil    uint64
	PaymasterInfo *PaymasterInfo // nil if no paymaster used
}

type PaymasterInfo struct {
	Address common.Address
	Stake   *big.Int
	Deposit *big.Int
}

func SimulateValidation(ctx context.Context, client *ethclient.Client, entryPoint common.Address, epABI abi.ABI, op types.UserOperation) (*ValidationResult, error) {
	callData, err := epABI.Pack("simulateValidation", op)
	if err != nil {
		return nil, fmt.Errorf("pack SimulateValidation: %w", err)
	}

	msg := ethereum.CallMsg{To: &entryPoint, Data: callData}
	_, err = client.CallContract(ctx, msg, nil)

	if err == nil {
		return nil, errors.New("simulateValidation did not revert as expected")
	}

	revertData, ok := extractRevertData(err)
	if !ok {
		return nil, fmt.Errorf("no revert data: %w", err)
	}

	result, decodeErr := decodeValidationResult(epABI, revertData)
	if decodeErr != nil {
		// A revert that ISN'T ValidationResult means real validation failure
		// (bad signature, paymaster rejected it, AA2x/AA3x errors, etc.)
		return nil, fmt.Errorf("validation reverted: %s", decodeRevertReason(revertData))
	}
	return result, nil
}

// --- helpers ---

// rpcDataError mirrors the interface most go-ethereum RPC transports
// implement (rpc.DataError) without importing the internal rpc package
// directly, so we can type-assert on it generically.
type rpcDataError interface {
	Error() string
	ErrorData() interface{}
}

// extractRevertData pulls the raw revert bytes out of the error returned by
// eth_call. Most nodes return this as the `data` field of the JSON-RPC error,
// which go-ethereum surfaces via an ErrorData() method on the error.
func extractRevertData(err error) ([]byte, bool) {
	var dataErr rpcDataError
	if !errors.As(err, &dataErr) {
		return nil, false
	}

	raw := dataErr.ErrorData()
	if raw == nil {
		return nil, false
	}

	switch v := raw.(type) {
	case string:
		data, decodeErr := hexutil.Decode(v)
		if decodeErr != nil {
			return nil, false
		}
		return data, true
	case []byte:
		return v, true
	default:
		return nil, false
	}
}

// EntryPoint's ReturnInfo / StakeInfo tuples, matching the shapes used in
// the ValidationResult / ValidationResultWithAggregation custom errors:
//
//	struct ReturnInfo {
//	    uint256 preOpGas;
//	    uint256 prefund;
//	    bool sigFailed;
//	    uint48 validAfter;
//	    uint48 validUntil;
//	    bytes paymasterContext;
//	}
//	struct StakeInfo {
//	    uint256 stake;
//	    uint256 unstakeDelaySec;
//	}
type returnInfo struct {
	PreOpGas         *big.Int
	Prefund          *big.Int
	SigFailed        bool
	ValidAfter       *big.Int
	ValidUntil       *big.Int
	PaymasterContext []byte
}

type stakeInfo struct {
	Stake           *big.Int
	UnstakeDelaySec *big.Int
}

// decodeValidationResult finds which custom error the revert data
// corresponds to (by 4-byte selector) and, if it's one of the
// ValidationResult variants, decodes it into a ValidationResult.
func decodeValidationResult(epABI abi.ABI, data []byte) (*ValidationResult, error) {
	if len(data) < 4 {
		return nil, errors.New("revert data too short to contain a selector")
	}
	selector := data[:4]

	errDef, ok := findErrorBySelector(epABI, selector)
	if !ok {
		return nil, fmt.Errorf("unknown error selector: 0x%x", selector)
	}

	switch errDef.Name {
	case "ValidationResult":
		return decodeValidationResultTuple(errDef, data[4:])
	case "ValidationResultWithAggregation":
		return decodeValidationResultTuple(errDef, data[4:])
	default:
		return nil, fmt.Errorf("revert is not a ValidationResult error: %s", errDef.Name)
	}
}

func findErrorBySelector(epABI abi.ABI, selector []byte) (abi.Error, bool) {
	for _, e := range epABI.Errors {
		if bytes.Equal(e.ID.Bytes()[:4], selector) {
			return e, true
		}
	}
	return abi.Error{}, false
}

// decodeValidationResultTuple unpacks:
//
//	(ReturnInfo returnInfo, StakeInfo senderInfo, StakeInfo factoryInfo, StakeInfo paymasterInfo)
//
// which is the shared shape of ValidationResult / ValidationResultWithAggregation's
// leading fields (the aggregator variant has one extra trailing field we ignore here).
func decodeValidationResultTuple(errDef abi.Error, data []byte) (*ValidationResult, error) {
	values, err := errDef.Inputs.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("unpack %s: %w", errDef.Name, err)
	}
	if len(values) < 4 {
		return nil, fmt.Errorf("unexpected field count in %s: got %d", errDef.Name, len(values))
	}

	ret, err := decodeReturnInfo(values[0])
	if err != nil {
		return nil, fmt.Errorf("decode returnInfo: %w", err)
	}

	paymasterStake, err := decodeStakeInfo(values[3])
	if err != nil {
		return nil, fmt.Errorf("decode paymasterInfo: %w", err)
	}

	result := &ValidationResult{
		PreOpGas:   ret.PreOpGas,
		Prefund:    ret.Prefund,
		SigFailed:  ret.SigFailed,
		ValidAfter: ret.ValidAfter.Uint64(),
		ValidUntil: ret.ValidUntil.Uint64(),
	}

	// Only attach paymaster info if the op actually used a paymaster
	// (nonzero stake/delay in that slot).
	if paymasterStake.Stake != nil && paymasterStake.Stake.Sign() > 0 {
		result.PaymasterInfo = &PaymasterInfo{
			Stake: paymasterStake.Stake,
		}
	}

	return result, nil
}

// decodeReturnInfo re-marshals the dynamically-typed tuple value returned by
// abi.Unpack into our concrete returnInfo struct, since abigen-less unpacking
// gives back values via reflection rather than our named type directly.
func decodeReturnInfo(v interface{}) (*returnInfo, error) {
	packed, err := repackTuple(v)
	if err != nil {
		return nil, err
	}
	if len(packed) < 5 {
		return nil, fmt.Errorf("returnInfo tuple has %d fields, expected 5+", len(packed))
	}
	ri := &returnInfo{}
	var ok bool
	if ri.PreOpGas, ok = toBigInt(packed[0]); !ok {
		return nil, errors.New("preOpGas: unexpected type")
	}
	if ri.Prefund, ok = toBigInt(packed[1]); !ok {
		return nil, errors.New("prefund: unexpected type")
	}
	if ri.SigFailed, ok = packed[2].(bool); !ok {
		return nil, errors.New("sigFailed: unexpected type")
	}
	if ri.ValidAfter, ok = toBigInt(packed[3]); !ok {
		return nil, errors.New("validAfter: unexpected type")
	}
	if ri.ValidUntil, ok = toBigInt(packed[4]); !ok {
		return nil, errors.New("validUntil: unexpected type")
	}
	return ri, nil
}

func decodeStakeInfo(v interface{}) (*stakeInfo, error) {
	packed, err := repackTuple(v)
	if err != nil {
		return nil, err
	}
	if len(packed) < 2 {
		return nil, fmt.Errorf("stakeInfo tuple has %d fields, expected 2", len(packed))
	}
	si := &stakeInfo{}
	var ok bool
	if si.Stake, ok = toBigInt(packed[0]); !ok {
		return nil, errors.New("stake: unexpected type")
	}
	if si.UnstakeDelaySec, ok = toBigInt(packed[1]); !ok {
		return nil, errors.New("unstakeDelaySec: unexpected type")
	}
	return si, nil
}

// repackTuple uses reflection to turn the anonymous struct value go-ethereum's
// abi package produces for a tuple into an ordered []interface{} of its fields,
// so we don't have to depend on an exact generated struct type.
func repackTuple(v interface{}) ([]interface{}, error) {
	rv := reflect.ValueOf(v)

	// Unpacked tuple values usually come back as a struct, but handle a
	// pointer-to-struct too, just in case.
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct for tuple, got %T", v)
	}

	out := make([]interface{}, rv.NumField())
	for i := 0; i < rv.NumField(); i++ {
		out[i] = rv.Field(i).Interface()
	}
	return out, nil
}

func toBigInt(v interface{}) (*big.Int, bool) {
	switch n := v.(type) {
	case *big.Int:
		return n, true
	case big.Int:
		return &n, true
	default:
		return nil, false
	}
}

// decodeRevertReason best-effort decodes a revert payload into a human
// readable string: standard Error(string), Panic(uint256), or a known
// EntryPoint custom error (e.g. FailedOp(uint256,string)). Falls back to hex.
func decodeRevertReason(data []byte) string {
	if len(data) == 0 {
		return "no revert data"
	}

	// Standard require()/revert("msg") -> Error(string) selector 0x08c379a0
	if reason, err := abi.UnpackRevert(data); err == nil {
		return reason
	}

	if len(data) >= 4 {
		return fmt.Sprintf("unrecognized revert (selector 0x%x): 0x%x", data[:4], data)
	}
	return fmt.Sprintf("unrecognized revert: 0x%x", data)
}