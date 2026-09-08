package bundle

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"time"

	"erc4337-bundler/internal/mempool"
	"erc4337-bundler/internal/validation"
	"erc4337-bundler/pkg/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Bundler holds everything needed to validate, batch, and submit
// UserOperations on-chain. This is the single source of truth for the
// type — do not redeclare it in other packages (e.g. rpc), since methods
// can only be defined on a type from within its own package.
type Bundler struct {
	mempool           *mempool.Mempool
	entryPoint        common.Address
	client            *ethclient.Client
	entryPointABI     abi.ABI
	chainID           *big.Int
	maxOpsPerBundle   int
	beneficiary       common.Address
	bundlerAddress    common.Address
	bundlerPrivateKey *ecdsa.PrivateKey
}

// NewBundler constructs a Bundler. bundlerAddress is derived from the
// private key rather than taken as a separate parameter, since the two
// must always match — passing both independently invites a mismatch bug.
func NewBundler(
	client *ethclient.Client,
	entryPoint common.Address,
	entryPointABI abi.ABI,
	chainID *big.Int,
	maxOpsPerBundle int,
	beneficiary common.Address,
	bundlerPrivateKey *ecdsa.PrivateKey,
) *Bundler {
	bundlerAddress := crypto.PubkeyToAddress(bundlerPrivateKey.PublicKey)

	return &Bundler{
		mempool:           mempool.NewMemPool(),
		entryPoint:        entryPoint,
		client:            client,
		entryPointABI:     entryPointABI,
		chainID:           chainID,
		maxOpsPerBundle:   maxOpsPerBundle,
		beneficiary:       beneficiary,
		bundlerAddress:    bundlerAddress,
		bundlerPrivateKey: bundlerPrivateKey,
	}
}

// perOpGasOverhead is a rough fixed cost added per UserOperation in a bundle
// to account for EntryPoint bookkeeping (nonce validation, event emission,
// etc.) that isn't captured by summing the op's own gas limit fields.
//
// TODO: replace with a measured constant from your EntryPoint version —
// this value is a conservative placeholder, not derived from the ABI.
const perOpGasOverhead = 30_000

// bundleBaseGasOverhead covers the fixed cost of the handleOps call itself
// (calldata decoding, loop setup, compensation transfer) independent of
// how many ops are included.
//
// TODO: same caveat as perOpGasOverhead — placeholder pending measurement.
const bundleBaseGasOverhead = 50_000

func (b *Bundler) RunBundlingLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tryBuildAndSubmitBundle(ctx)
		}
	}
}

func (b *Bundler) tryBuildAndSubmitBundle(ctx context.Context) {
	pending := b.mempool.All()
	if len(pending) == 0 {
		return
	}

	// Re-simulate right before inclusion — state may have shifted since eth_sendUserOperation.
	var validOps []types.UserOperation
	for _, op := range pending {
		if _, err := validation.SimulateValidation(ctx, b.client, b.entryPoint, b.entryPointABI, op); err != nil {
			log.Printf("dropping stale op for %s: %v", op.Sender.Hex(), err)
			b.mempool.Remove(op.Sender.Hex(), op.Nonce.String())
			continue
		}
		validOps = append(validOps, op)
		if len(validOps) >= b.maxOpsPerBundle {
			break
		}
	}
	if len(validOps) == 0 {
		return
	}

	callData, err := b.entryPointABI.Pack("handleOps", validOps, b.beneficiary)
	if err != nil {
		log.Printf("pack handleOps: %v", err)
		return
	}

	nonce, err := b.client.PendingNonceAt(ctx, b.bundlerAddress)
	if err != nil {
		log.Printf("nonce fetch: %v", err)
		return
	}

	gasTipCap, gasFeeCap, err := b.suggestGasFees(ctx)
	if err != nil {
		log.Printf("gas fee suggestion: %v", err)
		return
	}

	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   b.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       b.estimateBundleGas(validOps),
		To:        &b.entryPoint,
		Data:      callData,
	})

	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewLondonSigner(b.chainID), b.bundlerPrivateKey)
	if err != nil {
		log.Printf("sign tx: %v", err)
		return
	}

	if err := b.client.SendTransaction(ctx, signedTx); err != nil {
		log.Printf("send bundle tx: %v", err)
		return
	}

	log.Printf("submitted bundle with %d ops, txHash=%s", len(validOps), signedTx.Hash().Hex())
	for _, op := range validOps {
		b.mempool.Remove(op.Sender.Hex(), op.Nonce.String())
	}
}

// suggestGasFees returns (gasTipCap, gasFeeCap) for an EIP-1559 transaction,
// using the node's suggested priority fee plus a 2x buffer over the current
// base fee to tolerate a couple of blocks of base-fee movement before the
// bundle lands.
func (b *Bundler) suggestGasFees(ctx context.Context) (gasTipCap, gasFeeCap *big.Int, err error) {
	tipCap, err := b.client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("suggest gas tip cap: %w", err)
	}

	header, err := b.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch latest header: %w", err)
	}
	if header.BaseFee == nil {
		return nil, nil, fmt.Errorf("chain does not report a base fee (pre-EIP-1559?)")
	}

	// feeCap = 2 * baseFee + tipCap, a common conservative heuristic.
	feeCap := new(big.Int).Add(
		new(big.Int).Mul(header.BaseFee, big.NewInt(2)),
		tipCap,
	)

	return tipCap, feeCap, nil
}

// estimateBundleGas sums each op's declared gas limits plus fixed overhead
// constants, rather than calling eth_estimateGas against the assembled
// handleOps calldata. This is intentionally conservative (likely to
// overestimate) but avoids an extra RPC round-trip and the risk of a
// simulation-time revert blocking submission entirely.
//
// TODO: consider calling client.EstimateGas against the packed handleOps
// call as a tighter (but request-costing) alternative, with this sum as a
// fallback if that call errors.
func (b *Bundler) estimateBundleGas(ops []types.UserOperation) uint64 {
	total := new(big.Int).SetUint64(bundleBaseGasOverhead)

	for _, op := range ops {
		total.Add(total, op.CallGasLimit)
		total.Add(total, op.VerificationGasLimit)
		total.Add(total, op.PreVerificationGas)
		total.Add(total, big.NewInt(perOpGasOverhead))
	}

	return total.Uint64()
}