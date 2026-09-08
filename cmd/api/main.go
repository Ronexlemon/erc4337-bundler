package main

import (
	"context"
	"crypto/ecdsa"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"

	"erc4337-bundler/internal/bundle"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Celo Sepolia — the current primary Celo testnet (replaces Alfajores).
const (
	celoSepoliaRPC     = "https://forno.celo-sepolia.celo-testnet.org"
	celoSepoliaChainID = 11142220
)

// entryPointV06ABI is the ABI for the canonical v0.6 EntryPoint contract.
//
// TODO: this is currently a minimal placeholder covering only the methods
// and events this bundler actually calls (simulateValidation, handleOps,
// UserOperationEvent). Replace with the full official EntryPoint v0.6 ABI
// JSON before running against real UserOperations, or any additional
// method/event/error this code references later will fail to encode/decode.
const entryPointV06ABI = `[
	{"type":"function","name":"simulateValidation","stateMutability":"nonpayable","inputs":[{"name":"userOp","type":"tuple","components":[
		{"name":"sender","type":"address"},
		{"name":"nonce","type":"uint256"},
		{"name":"initCode","type":"bytes"},
		{"name":"callData","type":"bytes"},
		{"name":"callGasLimit","type":"uint256"},
		{"name":"verificationGasLimit","type":"uint256"},
		{"name":"preVerificationGas","type":"uint256"},
		{"name":"maxFeePerGas","type":"uint256"},
		{"name":"maxPriorityFeePerGas","type":"uint256"},
		{"name":"paymasterAndData","type":"bytes"},
		{"name":"signature","type":"bytes"}
	]}],"outputs":[]},
	{"type":"function","name":"handleOps","stateMutability":"nonpayable","inputs":[
		{"name":"ops","type":"tuple[]","components":[
			{"name":"sender","type":"address"},
			{"name":"nonce","type":"uint256"},
			{"name":"initCode","type":"bytes"},
			{"name":"callData","type":"bytes"},
			{"name":"callGasLimit","type":"uint256"},
			{"name":"verificationGasLimit","type":"uint256"},
			{"name":"preVerificationGas","type":"uint256"},
			{"name":"maxFeePerGas","type":"uint256"},
			{"name":"maxPriorityFeePerGas","type":"uint256"},
			{"name":"paymasterAndData","type":"bytes"},
			{"name":"signature","type":"bytes"}
		]},
		{"name":"beneficiary","type":"address"}
	],"outputs":[]},
	{"type":"error","name":"ValidationResult","inputs":[
		{"name":"returnInfo","type":"tuple","components":[
			{"name":"preOpGas","type":"uint256"},
			{"name":"prefund","type":"uint256"},
			{"name":"sigFailed","type":"bool"},
			{"name":"validAfter","type":"uint48"},
			{"name":"validUntil","type":"uint48"},
			{"name":"paymasterContext","type":"bytes"}
		]},
		{"name":"senderInfo","type":"tuple","components":[{"name":"stake","type":"uint256"},{"name":"unstakeDelaySec","type":"uint256"}]},
		{"name":"factoryInfo","type":"tuple","components":[{"name":"stake","type":"uint256"},{"name":"unstakeDelaySec","type":"uint256"}]},
		{"name":"paymasterInfo","type":"tuple","components":[{"name":"stake","type":"uint256"},{"name":"unstakeDelaySec","type":"uint256"}]}
	]},
	{"type":"event","name":"UserOperationEvent","inputs":[
		{"name":"userOpHash","type":"bytes32","indexed":true},
		{"name":"sender","type":"address","indexed":true},
		{"name":"paymaster","type":"address","indexed":true},
		{"name":"nonce","type":"uint256","indexed":false},
		{"name":"success","type":"bool","indexed":false},
		{"name":"actualGasCost","type":"uint256","indexed":false},
		{"name":"actualGasUsed","type":"uint256","indexed":false}
	]}
]`

func main() {
	client, err := ethclient.Dial(celoSepoliaRPC)
	if err != nil {
		log.Fatalf("dial rpc: %v", err)
	}

	epABI, err := abi.JSON(strings.NewReader(entryPointV06ABI))
	if err != nil {
		log.Fatalf("parse EntryPoint ABI: %v", err)
	}

	bundlerKey, err := loadBundlerPrivateKey()
	if err != nil {
		log.Fatalf("load bundler private key: %v", err)
	}
	bundlerAddress := crypto.PubkeyToAddress(bundlerKey.PublicKey)

	b := bundle.NewBundler(
		client,
		common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"), // v0.6 EntryPoint
		epABI,
		big.NewInt(celoSepoliaChainID),
		10,              // maxOpsPerBundle
		bundlerAddress,  // beneficiary — send gas refunds to the bundler itself
		bundlerKey,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.RunBundlingLoop(ctx)

	http.HandleFunc("/rpc", b.HandleRPC)
	log.Println("bundler listening on :4337 (Celo Sepolia, chainId", celoSepoliaChainID, ")")
	log.Fatal(http.ListenAndServe(":4337", nil))
}

// loadBundlerPrivateKey reads the bundler's signing key from the
// BUNDLER_PRIVATE_KEY environment variable (hex, with or without 0x prefix).
// Never hardcode a private key in source — this account pays gas and signs
// every bundle transaction.
func loadBundlerPrivateKey() (*ecdsa.PrivateKey, error) {
	hexKey := strings.TrimPrefix(os.Getenv("BUNDLER_PRIVATE_KEY"), "0x")
	if hexKey == "" {
		log.Fatal("BUNDLER_PRIVATE_KEY environment variable is not set")
	}
	return crypto.HexToECDSA(hexKey)
}