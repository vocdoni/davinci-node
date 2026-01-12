package web3

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/rs/zerolog"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3/txmanager"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2api "github.com/attestantio/go-eth2-client/api"
	eth2http "github.com/attestantio/go-eth2-client/http"
)

// applyGasMultiplier applies the gas multiplier to a base fee value.
// It multiplies the base fee by the multiplier and returns the result.
// The multiplier is a float64 value (e.g., 1.0 = no change, 2.0 = double).
func applyGasMultiplier(baseFee *big.Int, multiplier float64) *big.Int {
	if multiplier <= 0 {
		multiplier = 1.0
	}
	// Convert multiplier to big.Float for precision
	mult := new(big.Float).SetFloat64(multiplier)
	// Convert baseFee to big.Float
	baseFeeFloat := new(big.Float).SetInt(baseFee)
	// Multiply
	result := new(big.Float).Mul(baseFeeFloat, mult)
	// Convert back to big.Int (truncating decimals)
	resultInt, _ := result.Int(nil)

	log.Debugw("applied gas multiplier",
		"baseFee", baseFee.String(),
		"multiplier", multiplier,
		"result", resultInt.String())

	return resultInt
}

// NewEIP4844Transaction method creates and signs a new EIP-4844 (type-3)
// transaction by calculating the nonce from the RPC and returning the result
// of NewEIP4844TransactionWithNonce.
func (c *Contracts) NewEIP4844Transaction(
	ctx context.Context,
	to common.Address,
	data []byte,
	blobsSidecar *types.BlobTxSidecar,
) (*gethtypes.Transaction, error) {
	// Nonce
	nonce, err := c.cli.PendingNonceAt(ctx, c.AccountAddress())
	if err != nil {
		return nil, err
	}
	return c.NewEIP4844TransactionWithNonce(ctx, to, data, nonce, blobsSidecar)
}

// NewEIP4844TransactionWithNonce method creates and signs a new EIP-4844. It
// calculates gas limits and fee caps, and returns the signed transaction.
// The provided nonce is used (caller must ensure it's correct).
//
// Requirements:
//   - `to` MUST be non-nil per EIP-4844.
//   - `method` MUST be a valid method in the ABI.
//   - `c.signer` MUST be non-nil (private key set).
func (c *Contracts) NewEIP4844TransactionWithNonce(
	ctx context.Context,
	to common.Address,
	data []byte,
	nonce uint64,
	blobsSidecar *types.BlobTxSidecar,
) (*gethtypes.Transaction, error) {
	if (to == common.Address{}) {
		return nil, fmt.Errorf("empty to address")
	}
	if c.signer == nil {
		return nil, fmt.Errorf("no signer defined")
	}

	// Estimate execution gas, include blob hashes so any contract logic that
	// references them (e.g. checks) isn't under-estimated.
	gas, err := c.txManager.EstimateGas(ctx, ethereum.CallMsg{
		From:       c.AccountAddress(),
		To:         &to,
		Data:       data,
		BlobHashes: blobsSidecar.BlobHashes(),
	}, txmanager.DefaultGasEstimateOpts, txmanager.DefaultCancelGasFallback)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas: %w", err)
	}

	// Fee building
	// Tip suggestion (EIP-1559)
	tipCap, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tip cap: %w", err)
	}

	// Base fee for *execution gas* from latest block
	h, err := c.cli.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block: %w", err)
	}
	baseFee := h.BaseFee // can be nil on pre-London, but not on mainnet today

	// Choose a reasonable safety multiplier for max fee per gas.
	// Common pattern: maxFee = (baseFee*2 + tip) * multiplier
	baseMaxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	baseMaxFee.Add(baseMaxFee, tipCap)
	maxFee := applyGasMultiplier(baseMaxFee, c.GasMultiplier)

	// Base fee for *blob gas* (separate market). Use RPC eth_blobBaseFee.
	blobBaseFee, err := c.cli.BlobBaseFee(ctx)
	if err != nil {
		return nil, fmt.Errorf("blob base fee: %w", err)
	}
	// Apply gas multiplier: (blobBaseFee * 2) * multiplier
	baseBlobFeeCap := new(big.Int).Mul(blobBaseFee, big.NewInt(2))
	blobFeeCap := applyGasMultiplier(baseBlobFeeCap, c.GasMultiplier)

	// Build & sign the blob transaction
	cID := new(big.Int).SetUint64(c.ChainID)
	inner := &gethtypes.BlobTx{
		ChainID:    uint256.MustFromBig(cID),
		Nonce:      nonce, // Use provided nonce
		GasTipCap:  uint256.MustFromBig(tipCap),
		GasFeeCap:  uint256.MustFromBig(maxFee),
		Gas:        gas,
		To:         to,
		Value:      uint256.NewInt(0),
		Data:       data,
		BlobFeeCap: uint256.MustFromBig(blobFeeCap), // REQUIRED for blobs
		BlobHashes: blobsSidecar.BlobHashes(),
		Sidecar:    blobsSidecar.AsGethSidecar(), // attach sidecar for gossip
	}

	signedTx, err := gethtypes.SignNewTx((*ecdsa.PrivateKey)(c.signer), gethtypes.NewCancunSigner(cID), inner)
	if err != nil {
		return nil, fmt.Errorf("failed to sign new tx: %w", err)
	}
	return signedTx, nil
}

// TransactionWithReceipt returns the full tx including it's receipt
// of an already mined transaction, identified by txHash.
func (c *Contracts) TransactionWithReceipt(ctx context.Context, txHash common.Hash,
) (*gethtypes.Transaction, *gethtypes.Receipt, error) {
	ethcli, err := c.cli.EthClient()
	if err != nil {
		return nil, nil, fmt.Errorf("eth client: %w", err)
	}
	// EL: txHash -> receipt
	receipt, err := ethcli.TransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, nil, fmt.Errorf("tx receipt: %w", err)
	}
	if receipt.BlockHash == (common.Hash{}) {
		return nil, nil, fmt.Errorf("tx not mined yet")
	}
	// EL: txHash -> full tx
	tx, _, err := ethcli.TransactionByHash(ctx, txHash)
	if err != nil {
		return nil, nil, fmt.Errorf("tx: %w", err)
	}

	return tx, receipt, nil
}

// BlobSidecarsOfBlock returns the blob sidecars stored in consensus layer,
// of a block identified by a blockHash.
func (c *Contracts) BlobSidecarsOfBlock(ctx context.Context, blockHash common.Hash) ([]*types.BlobSidecar, error) {
	// EL: RPC client
	ethcli, err := c.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("eth client: %w", err)
	}

	// EL: block hash -> header -> parent beacon root (EIP-4788)
	hdr, err := ethcli.HeaderByHash(ctx, blockHash)
	if err != nil {
		return nil, fmt.Errorf("header by hash: %w", err)
	}
	if hdr.ParentBeaconRoot == nil {
		return nil, fmt.Errorf("parent beacon root missing (EL client too old?)")
	}

	// CL: Beacon client
	bc, err := eth2http.New(ctx,
		eth2http.WithAddress(strings.TrimRight(c.Web3ConsensusAPIEndpoint, "/")),
		eth2http.WithLogLevel(zerolog.DebugLevel), // zerolog.TraceLevel is useful for debugging
	)
	if err != nil {
		return nil, fmt.Errorf("beacon client: %w", err)
	}

	// CL: resolve parent root -> parent slot
	// Block IDs can be roots, slots, or keywords; use the root string directly.
	var parentSlot uint64
	if provider, isProvider := bc.(eth2client.BeaconBlockHeadersProvider); isProvider {
		headers, err := provider.BeaconBlockHeader(ctx, &eth2api.BeaconBlockHeaderOpts{
			Block: hdr.ParentBeaconRoot.Hex(),
		})
		if err != nil {
			return nil, fmt.Errorf("beacon headers(%s): %w", hdr.ParentBeaconRoot, err)
		}
		parentSlot = uint64(headers.Data.Header.Message.Slot)
	}
	slot := parentSlot + 1 // slot of our EL block

	// CL: fetch blob sidecars for that slot
	var sidecars []*types.BlobSidecar
	if provider, isProvider := bc.(eth2client.BlobSidecarsProvider); isProvider {
		resp, err := provider.BlobSidecars(ctx, &eth2api.BlobSidecarsOpts{
			Block: fmt.Sprintf("%d", slot),
		})
		if err != nil {
			return nil, fmt.Errorf("blob sidecars(slot=%d): %w", slot, err)
		}
		for _, sc := range resp.Data {
			sidecars = append(sidecars, types.NewBlobSidecarFromDeneb(sc))
		}
	}

	return sidecars, nil
}

// BlobsByTxHash returns all the blobs sidecars of a tx, given a `txHash`.
func (c *Contracts) BlobsByTxHash(
	ctx context.Context,
	txHash common.Hash,
) ([]*types.BlobSidecar, error) {
	tx, txReceipt, err := c.TransactionWithReceipt(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("tx parent beacon root: %w", err)
	}
	if tx.Type() != gethtypes.BlobTxType {
		return nil, fmt.Errorf("not a blob tx (type=%d)", tx.Type())
	}

	sidecars, err := c.BlobSidecarsOfBlock(ctx, txReceipt.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("fetch blob sidecars: %w", err)
	}

	// filter to keep only the blobs related to this transaction
	hashes := tx.BlobHashes()
	blobs := make([]*types.BlobSidecar, 0, len(sidecars))
	for _, sc := range sidecars {
		if sc != nil && slices.Contains(hashes, sc.VersionedBlobHash()) {
			blobs = append(blobs, sc)
		}
	}
	return blobs, nil
}
