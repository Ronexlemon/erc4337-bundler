package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type UserOperation struct{
	Sender   common.Address `json:"sender"`
	Nonce    *big.Int       `json:"nonce"`
	InitCode []byte          `json:"initCode"`
	CallData  []byte         `json:"callData"`
	CallGasLimit         *big.Int       `json:"callGasLimit"`
	VerificationGasLimit *big.Int       `json:"verificationGasLimit"`
	PreVerificationGas   *big.Int       `json:"preVerificationGas"`
	MaxFeePerGas         *big.Int       `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *big.Int       `json:"maxPriorityFeePerGas"`
	PaymasterAndData     []byte         `json:"paymasterAndData"`
	Signature            []byte         `json:"signature"`

}

// RPCUserOperation is the hex-string JSON-RPC wire format (eth_sendUserOperation)
// receives all numeric/byte fields as 0x-prefixed hex strings not native types

type RPCUserOperation struct {
	Sender               common.Address `json:"sender"`
	Nonce                hexutil.Big    `json:"nonce"`
	InitCode             hexutil.Bytes  `json:"initCode"`
	CallData             hexutil.Bytes  `json:"callData"`
	CallGasLimit         hexutil.Big    `json:"callGasLimit"`
	VerificationGasLimit hexutil.Big    `json:"verificationGasLimit"`
	PreVerificationGas   hexutil.Big    `json:"preVerificationGas"`
	MaxFeePerGas         hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas hexutil.Big    `json:"maxPriorityFeePerGas"`
	PaymasterAndData     hexutil.Bytes  `json:"paymasterAndData"`
	Signature            hexutil.Bytes  `json:"signature"`
}

func (r RPCUserOperation) ToUserOperation() UserOperation{
	return UserOperation{
		Sender: r.Sender,
		Nonce: r.Nonce.ToInt(),
		InitCode: r.InitCode,
		CallData:             r.CallData,
		CallGasLimit:         r.CallGasLimit.ToInt(),
		VerificationGasLimit: r.VerificationGasLimit.ToInt(),
		PreVerificationGas:   r.PreVerificationGas.ToInt(),
		MaxFeePerGas:         r.MaxFeePerGas.ToInt(),
		MaxPriorityFeePerGas: r.MaxPriorityFeePerGas.ToInt(),
		PaymasterAndData:     r.PaymasterAndData,
		Signature:            r.Signature,

	}
}