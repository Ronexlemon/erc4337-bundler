package types


import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func packUserOp(op UserOperation) []byte {
	// Pack the "core" fields (hash of initCode/callData/paymasterAndData, not raw bytes)
	args := abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("uint256")},
		{Type: mustType("bytes32")},
		{Type: mustType("bytes32")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("bytes32")},
	}
	packed, _ := args.Pack(
		op.Sender,
		op.Nonce,
		crypto.Keccak256Hash(op.InitCode),
		crypto.Keccak256Hash(op.CallData),
		op.CallGasLimit,
		op.VerificationGasLimit,
		op.PreVerificationGas,
		op.MaxFeePerGas,
		op.MaxPriorityFeePerGas,
		crypto.Keccak256Hash(op.PaymasterAndData),
	)
	return packed
}

func GetUserOpHash(op UserOperation, entryPoint common.Address, chainID *big.Int) common.Hash {
	inner := crypto.Keccak256Hash(packUserOp(op))
	args := abi.Arguments{{Type: mustType("bytes32")}, {Type: mustType("address")}, {Type: mustType("uint256")}}
	final, _ := args.Pack(inner, entryPoint, chainID)
	return crypto.Keccak256Hash(final)
}

func mustType(t string) abi.Type {
	ty, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return ty
}