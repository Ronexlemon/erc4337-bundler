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
	"erc4337-bundler/pkg/abi"

	contractAbi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Celo Sepolia — the current primary Celo testnet (replaces Alfajores).
const (
	celoSepoliaRPC     = "https://forno.celo-sepolia.celo-testnet.org"
	celoSepoliaChainID = 11142220
)


func main() {
	client, err := ethclient.Dial(celoSepoliaRPC)
	if err != nil {
		log.Fatalf("dial rpc: %v", err)
	}

	epABI, err := contractAbi.JSON(strings.NewReader(abi.EntryPointV06ABI))
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
func loadBundlerPrivateKey() (*ecdsa.PrivateKey, error) {
	hexKey := strings.TrimPrefix(os.Getenv("BUNDLER_PRIVATE_KEY"), "0x")
	if hexKey == "" {
		log.Fatal("BUNDLER_PRIVATE_KEY environment variable is not set")
	}
	return crypto.HexToECDSA(hexKey)
}