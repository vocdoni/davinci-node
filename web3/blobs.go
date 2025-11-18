package web3

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	kzg4844 "github.com/crate-crypto/go-eth-kzg"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844" // for the struct types
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/web3/txmanager"
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
	contractABI *abi.ABI,
	method string,
	args []any,
	blobsSidecar *types.BlobTxSidecar,
) (*types.Transaction, error) {
	// Nonce
	nonce, err := c.cli.PendingNonceAt(ctx, c.AccountAddress())
	if err != nil {
		return nil, err
	}
	return c.NewEIP4844TransactionWithNonce(ctx, to, contractABI, method, args, blobsSidecar, nonce)
}

// NewEIP4844TransactionWithNonce method creates and signs a new EIP-4844. It
// calculates gas limits and fee caps, and returns the signed transaction.
// The provided nonce is used (caller must ensure it's correct).
//
// Requirements:
//   - `to` MUST be non-nil per EIP-4844.
//   - `contractABI` MUST be non-nil.
//   - `method` MUST be a valid method in the ABI.
//   - `c.signer` MUST be non-nil (private key set).
func (c *Contracts) NewEIP4844TransactionWithNonce(
	ctx context.Context,
	to common.Address,
	contractABI *abi.ABI,
	method string,
	args []any,
	blobsSidecar *types.BlobTxSidecar,
	nonce uint64,
) (*types.Transaction, error) {
	if contractABI == nil {
		return nil, fmt.Errorf("nil contract ABI")
	}
	if (to == common.Address{}) {
		return nil, fmt.Errorf("empty to address")
	}
	if method == "" {
		return nil, fmt.Errorf("empty method")
	}
	if c.signer == nil {
		return nil, fmt.Errorf("no signer defined")
	}

	// ABI-encode call data
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ABI: %w", err)
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
	inner := &types.BlobTx{
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
		Sidecar:    blobsSidecar, // attach sidecar for gossip
	}

	signedTx, err := types.SignNewTx((*ecdsa.PrivateKey)(c.signer), types.NewCancunSigner(cID), inner)
	if err != nil {
		return nil, fmt.Errorf("failed to sign new tx: %w", err)
	}
	return signedTx, nil
}

// BuildBlobsSidecarV0 converts raw blobs -> commitments/proofs using crate-crypto.
// Returns a geth Sidecar (types.BlobTxSidecar with Version=0) and versioned blob hashes.
func BuildBlobsSidecarV0(raw [][]byte) (*types.BlobTxSidecar, []common.Hash, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("no blobs")
	}
	ctx, err := kzg4844.NewContext4096Secure()
	if err != nil {
		return nil, nil, fmt.Errorf("kzg ctx: %w", err)
	}

	blobs := make([]gethkzg.Blob, len(raw))
	comms := make([]gethkzg.Commitment, len(raw))
	proofs := make([]gethkzg.Proof, len(raw))

	for i, b := range raw {
		if len(b) != params.BlobTxFieldElementsPerBlob*params.BlobTxBytesPerFieldElement {
			return nil, nil, fmt.Errorf("blob %d wrong size: got %d", i, len(b))
		}
		// cast []byte -> crate blob then to geth blob bytes
		var crateBlob kzg4844.Blob
		copy(crateBlob[:], b)

		commit, err := ctx.BlobToKZGCommitment(&crateBlob, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("commitment %d: %w", i, err)
		}
		proof, err := ctx.ComputeBlobKZGProof(&crateBlob, commit, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("proof %d: %w", i, err)
		}

		// convert to geth types
		copy(blobs[i][:], b)
		copy(comms[i][:], commit[:])
		copy(proofs[i][:], proof[:])
	}

	sc := &types.BlobTxSidecar{
		Blobs:       blobs,
		Commitments: comms,
		Proofs:      proofs,
	}
	return sc, sc.BlobHashes(), nil
}

// BuildBlobsSidecar converts raw blobs -> commitments/cell proofs using crate-crypto.
// Returns a geth Sidecar (types.BlobTxSidecar) with Version 1 cell proofs and versioned blob hashes.
// This function creates Version 1 sidecars with cell proofs for EIP-7594 (Fusaka upgrade).
func BuildBlobsSidecar(raw [][]byte) (*types.BlobTxSidecar, []common.Hash, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("no blobs")
	}
	ctx, err := kzg4844.NewContext4096Secure()
	if err != nil {
		return nil, nil, fmt.Errorf("kzg ctx: %w", err)
	}

	blobs := make([]gethkzg.Blob, len(raw))
	comms := make([]gethkzg.Commitment, len(raw))
	proofs := make([]gethkzg.Proof, len(raw)*kzg4844.CellsPerExtBlob)

	for i, b := range raw {
		if len(b) != params.BlobTxFieldElementsPerBlob*params.BlobTxBytesPerFieldElement {
			return nil, nil, fmt.Errorf("blob %d wrong size: got %d", i, len(b))
		}
		// cast []byte -> crate blob then to geth blob bytes
		var crateBlob kzg4844.Blob
		copy(crateBlob[:], b)

		commit, err := ctx.BlobToKZGCommitment(&crateBlob, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("commitment %d: %w", i, err)
		}

		// Compute cell proofs for EIP-7594 (Fusaka upgrade)
		_, cellProofs, err := ctx.ComputeCellsAndKZGProofs(&crateBlob, 0)
		if err != nil {
			return nil, nil, fmt.Errorf("cell proofs %d: %w", i, err)
		}

		// convert to geth types
		copy(blobs[i][:], b)
		copy(comms[i][:], commit[:])

		// Copy all cell proofs for this blob
		for j := range cellProofs {
			copy(proofs[i*kzg4844.CellsPerExtBlob+j][:], cellProofs[j][:])
		}
	}

	// Create Version 1 sidecar directly with cell proofs
	sc := types.NewBlobTxSidecar(
		types.BlobSidecarVersion1,
		blobs,
		comms,
		proofs,
	)

	return sc, sc.BlobHashes(), nil
}

// BlobByCommitment gets the blob bytes matching `commitmentHex` (0x...) for tx `txHash`
// using the provided Consensus (beacon) API base URL.
func (c *Contracts) BlobByCommitment(
	ctx context.Context,
	txHash common.Hash,
	commitmentHex string,
) ([]byte, error) {
	if c.Web3ConsensusAPIEndpoint == "" {
		return nil, fmt.Errorf("no consensus API endpoint configured")
	}
	ethcli, err := c.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("eth client: %w", err)
	}
	receipt, err := ethcli.TransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("tx receipt: %w", err)
	}
	if receipt.BlockHash == (common.Hash{}) {
		return nil, fmt.Errorf("tx not mined yet")
	}

	// EL header -> parent beacon root (EIP-4788)
	hdr, err := ethcli.HeaderByHash(ctx, receipt.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("header by hash: %w", err)
	}
	parentRoot := hdr.ParentBeaconRoot
	if parentRoot == nil {
		return nil, fmt.Errorf("parent beacon root missing (client too old?)")
	}

	// Ask CL for the header of that parent root => get its slot
	type beaconHeaderResp struct {
		Data struct {
			Header struct {
				Message struct {
					Slot string `json:"slot"`
				} `json:"message"`
				Root string `json:"root"`
			} `json:"header"`
		} `json:"data"`
	}
	var bh beaconHeaderResp
	urlHdr := fmt.Sprintf("%s/eth/v1/beacon/headers/%s", strings.TrimRight(c.Web3ConsensusAPIEndpoint, "/"), parentRoot.Hex())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlHdr, nil)
	if err != nil {
		return nil, fmt.Errorf("new header req: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("beacon header GET: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // ignore error on close
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("beacon header error %d: %s", resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(&bh); err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	parentSlot, err := strconv.ParseUint(bh.Data.Header.Message.Slot, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("slot parse: %w", err)
	}
	targetSlot := parentSlot + 1 // slot of our execution block’s beacon root

	// Fetch blob sidecars for that slot
	urlSide := fmt.Sprintf("%s/eth/v1/beacon/blob_sidecars/%d", strings.TrimRight(c.Web3ConsensusAPIEndpoint, "/"), targetSlot)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, urlSide, nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("beacon sidecars GET: %w", err)
	}
	defer func() {
		_ = resp2.Body.Close() // ignore error on close
	}()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return nil, fmt.Errorf("beacon sidecars error %d: %s", resp2.StatusCode, string(body))
	}

	var sidecars struct {
		Data []struct {
			Blob          string `json:"blob"`
			KZGCommitment string `json:"kzg_commitment"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&sidecars); err != nil {
		return nil, fmt.Errorf("decode sidecars: %w", err)
	}

	needle := strings.ToLower(strings.TrimPrefix(commitmentHex, "0x"))
	for _, sc := range sidecars.Data {
		if strings.ToLower(strings.TrimPrefix(sc.KZGCommitment, "0x")) == needle {
			return hexutil.Decode(sc.Blob)
		}
	}
	return nil, fmt.Errorf("commitment %s not found in slot %d sidecars", commitmentHex, targetSlot)
}
