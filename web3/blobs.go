package web3

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"slices"
	"strings"

	kzg4844 "github.com/crate-crypto/go-eth-kzg"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844" // for the struct types
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/vocdoni/davinci-node/log"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2api "github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	eth2http "github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/deneb"
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

// SendBlobTx builds, signs and broadcasts an EIP-4844 (type-3) tx.
//   - `to` MUST be non-nil per EIP-4844.
//   - `blobs` are raw 131072-byte blobs (4096 * 32).
func (c *Contracts) SendBlobTx(
	ctx context.Context,
	to common.Address,
	sidecar *types.BlobTxSidecar,
) (*types.Transaction, [][]byte, error) {
	if c.signer == nil {
		return nil, nil, fmt.Errorf("no private key set")
	}
	if sidecar == nil {
		return nil, nil, fmt.Errorf("no blob sidecar provided")
	}
	if len(sidecar.Blobs) == 0 {
		return nil, nil, fmt.Errorf("no blobs provided")
	}
	if bytes.Equal(to[:], common.Address{}.Bytes()) {
		return nil, nil, fmt.Errorf("invalid recipient address")
	}

	// get nonce and chainID
	auth, err := c.authTransactOpts()
	if err != nil {
		return nil, nil, err
	}
	from := c.signer.Address()
	nonce := auth.Nonce.Uint64()

	// Fee caps (exec gas)
	tipCap, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("tip cap: %w", err)
	}
	baseFee, err := c.cli.SuggestGasPrice(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("base gas fee: %w", err)
	}
	// Apply gas multiplier: (baseFee * 2 + tipCap) * multiplier
	baseGasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), tipCap)
	gasFeeCap := applyGasMultiplier(baseGasFeeCap, c.GasMultiplier)

	// Blob gas cap
	blobBaseFee, err := c.cli.BlobBaseFee(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("blob base fee: %w", err)
	}
	// Apply gas multiplier: (blobBaseFee * 2) * multiplier
	baseBlobFeeCap := new(big.Int).Mul(blobBaseFee, big.NewInt(2))
	blobFeeCap := applyGasMultiplier(baseBlobFeeCap, c.GasMultiplier)

	// Estimate execution gas (must include blob fields)
	call := ethereum.CallMsg{
		From:      from,
		To:        &to,
		GasFeeCap: gasFeeCap, // 1559
		GasTipCap: tipCap,    // 1559
		// Data:       calldata,            // ABI-encoded if calling a contract
		BlobGasFeeCap: blobFeeCap,           // <= REQUIRED for 4844
		BlobHashes:    sidecar.BlobHashes(), // <= REQUIRED for 4844
	}
	gasLimit, err := c.cli.EstimateGas(ctx, call)
	if err != nil {
		return nil, nil, fmt.Errorf("estimate gas for blobs tx: %w", err)
	}

	// Create & sign blob tx
	txData := &types.BlobTx{
		ChainID:    uint256.NewInt(c.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.MustFromBig(tipCap),
		GasFeeCap:  uint256.MustFromBig(gasFeeCap),
		Gas:        gasLimit,
		To:         to,
		BlobFeeCap: uint256.MustFromBig(blobFeeCap),
		BlobHashes: sidecar.BlobHashes(),
		Sidecar:    sidecar,
	}

	unsigned := types.NewTx(txData)
	signer := types.NewCancunSigner(new(big.Int).SetUint64(c.ChainID))
	signed, err := types.SignTx(unsigned, signer, (*ecdsa.PrivateKey)(c.signer))
	if err != nil {
		return nil, nil, fmt.Errorf("sign blobs tx: %w", err)
	}

	// Broadcast
	if err := c.cli.SendTransaction(ctx, signed); err != nil {
		return nil, nil, fmt.Errorf("send blobs tx: %w", err)
	}
	commitments := [][]byte{}
	for _, c := range sidecar.Commitments {
		commitments = append(commitments, c[:])
	}
	return signed, commitments, nil
}

// NewEIP4844Transaction ABI-encodes (method,args), attaches blob sidecar, and signs a type-3 tx.
func (c *Contracts) NewEIP4844Transaction(
	ctx context.Context,
	to common.Address,
	contractABI *abi.ABI,
	method string,
	args []any,
	blobsSidecar *types.BlobTxSidecar,
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
		return nil, err
	}

	// Estimate execution gas, include blob hashes so any contract logic that
	// references them (e.g. checks) isn’t under-estimated.
	gas, err := c.cli.EstimateGas(ctx, ethereum.CallMsg{
		From:       c.AccountAddress(),
		To:         &to,
		Data:       data,
		BlobHashes: blobsSidecar.BlobHashes(),
	})
	if err != nil {
		return nil, err
	}

	// Nonce
	nonce, err := c.cli.PendingNonceAt(ctx, c.AccountAddress())
	if err != nil {
		return nil, err
	}

	// Fee building
	// Tip suggestion (EIP-1559)
	tipCap, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}

	// Base fee for *execution gas* from latest block
	h, err := c.cli.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	baseFee := h.BaseFee // can be nil on pre-London, but not on mainnet today

	// Choose a reasonable safety multiplier for max fee per gas.
	// Common pattern: maxFee = (baseFee*2 + tip) * multiplier
	baseMaxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	baseMaxFee.Add(baseMaxFee, tipCap)
	maxFee := applyGasMultiplier(baseMaxFee, c.GasMultiplier)

	// Base fee for *blob gas* (separate market). Use RPC eth_blobBaseFee.
	// NOTE: go-ethereum doesn't have a typed helper; call raw RPC:
	var blobBaseFeeHex string
	ethclient, err := c.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("cannot get eth client: %w", err)
	}
	if err := ethclient.Client().CallContext(ctx, &blobBaseFeeHex, "eth_blobBaseFee"); err != nil {
		return nil, fmt.Errorf("eth_blobBaseFee: %w", err)
	}
	blobBaseFee, _ := new(big.Int).SetString(strings.TrimPrefix(blobBaseFeeHex, "0x"), 16)
	// Apply gas multiplier: (blobBaseFee * 2) * multiplier
	baseBlobFeeCap := new(big.Int).Mul(blobBaseFee, big.NewInt(2))
	blobFeeCap := applyGasMultiplier(baseBlobFeeCap, c.GasMultiplier)

	// Build & sign the blob transaction
	cID := new(big.Int).SetUint64(c.ChainID)
	inner := &types.BlobTx{
		ChainID:    uint256.MustFromBig(cID),
		Nonce:      nonce,
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
		return nil, err
	}
	return signedTx, nil
}

// BuildBlobsSidecar converts raw blobs -> commitments/proofs using crate-crypto.
// Returns a geth Sidecar (types.BlobTxSidecar) and versioned blob hashes.
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

// BlobByCommitment gets the blob bytes matching `commitmentHex` (0x...) for tx `txHash`
// using the provided Consensus (beacon) API base URL.
func (c *Contracts) BlobByCommitment(
	ctx context.Context,
	txHash common.Hash,
) ([]deneb.Blob, error) {
	if c.Web3ConsensusAPIEndpoint == "" {
		return nil, fmt.Errorf("no consensus API endpoint configured")
	}

	// --- EL: receipt -> block hash
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

	// 1) Load tx to get its blob versioned hashes
	tx, _, err := ethcli.TransactionByHash(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("tx: %w", err)
	}
	if tx.Type() != types.BlobTxType {
		return nil, fmt.Errorf("not a blob tx (type=%d)", tx.Type())
	}
	wantedHashes := tx.BlobHashes() // []common.Hash (versioned)

	// EL header -> parent beacon root (EIP-4788)
	hdr, err := ethcli.HeaderByHash(ctx, receipt.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("header by hash: %w", err)
	}
	if hdr.ParentBeaconRoot == nil {
		return nil, fmt.Errorf("parent beacon root missing (EL client too old?)")
	}
	parentRootHex := hdr.ParentBeaconRoot.Hex()

	// --- CL: typed Beacon client
	bc, err := eth2http.New(ctx,
		eth2http.WithAddress(strings.TrimRight(c.Web3ConsensusAPIEndpoint, "/")),
		// eth2http.WithLogLevel(eth2http.LogLevelWarn), // optional
		// eth2http.WithClient(&http.Client{Timeout: 10 * time.Second}), // optional
	)
	if err != nil {
		return nil, fmt.Errorf("beacon client: %w", err)
	}

	// --- CL: resolve parent root -> parent slot (typed)
	// Block IDs can be roots, slots, or keywords; use the root string directly.
	var parentSlot uint64
	if provider, isProvider := bc.(eth2client.BeaconBlockHeadersProvider); isProvider {
		headers, err := provider.BeaconBlockHeader(ctx, &eth2api.BeaconBlockHeaderOpts{
			Block: parentRootHex,
		})
		if err != nil {
			return nil, fmt.Errorf("beacon headers(%s): %w", parentRootHex, err)
		}
		parentSlot = uint64(headers.Data.Header.Message.Slot)
	}
	slot := parentSlot + 1 // slot of our EL block’s beacon root
	// TODO: is this whole step needed? if BlobSidecars accepts parentRootHex

	// --- CL: fetch blob sidecars for that slot (typed)
	var sidecars []*deneb.BlobSidecar
	if provider, isProvider := bc.(eth2client.BlobSidecarsProvider); isProvider {
		resp, err := provider.BlobSidecars(ctx, &eth2api.BlobSidecarsOpts{
			Block: parentRootHex,
		})
		if err != nil {
			return nil, fmt.Errorf("blob sidecars(slot=%d): %w", slot, err)
		}
		sidecars = resp.Data
	}
	// --- Find the requested commitment; return its blob bytes

	blobs := []deneb.Blob{}
	for _, sc := range sidecars {
		if sc == nil {
			continue
		}
		if slices.Contains(wantedHashes, versionedBlobHash(sc.KZGCommitment)) {
			blobs = append(blobs, sc.Blob)
		}
		// DEAD CODE
		// if strings.TrimPrefix(strings.ToLower(sc.KZGCommitment.String()), "0x") ==
		// 	strings.TrimPrefix(strings.ToLower(commitmentHex), "0x") {
		// 	return &sc.Blob, nil
		// }
	}

	return blobs, nil
	// return nil, fmt.Errorf("commitment %s not found in slot %d sidecars", commitmentHex, slot)
}

func versionedBlobHash(deneb.KZGCommitment) common.Hash {
	// TODO: implement
	return common.Hash{}
}

// BlobByTxHash fetches blobs via EL RPC (preferred) and verifies against tx blobVersionedHashes.
func (c *Contracts) BlobByTxHash(ctx context.Context, txHash common.Hash) ([][]byte, error) {
	ethcli, err := c.cli.EthClient()
	if err != nil {
		return nil, fmt.Errorf("eth client: %w", err)
	}

	// 1) Load tx to get its blob versioned hashes
	tx, _, err := ethcli.TransactionByHash(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("tx: %w", err)
	}
	if tx.Type() != types.BlobTxType {
		return nil, fmt.Errorf("not a blob tx (type=%d)", tx.Type())
	}
	want := tx.BlobHashes() // []common.Hash (versioned)

	// 2) Try the EL convenience RPC
	var sidecar struct {
		Blobs       []string `json:"blobs"`
		Commitments []string `json:"commitments"`
		Proofs      []string `json:"proofs"`
	}
	raw, err := c.cli.RPCClient()
	if err != nil {
		return nil, fmt.Errorf("rpc client: %w", err)
	}

	if err := raw.CallContext(ctx, &sidecar, "eth_getBlobSidecarByTxHash", txHash.Hex()); err == nil {
		// 3) Verify & collect
		out := make([][]byte, 0, len(sidecar.Blobs))
		for i := range sidecar.Blobs {
			blobB, err := hexutil.Decode(sidecar.Blobs[i])
			if err != nil {
				return nil, err
			}
			commB, err := hexutil.Decode(sidecar.Commitments[i])
			if err != nil {
				return nil, err
			}
			// var comm kzg4844.KZGCommitment
			var comm deneb.KZGCommitment
			copy(comm[:], commB)
			got := versionedBlobHash(comm) // compare to want[i] (order is not guaranteed)
			ok := false
			for _, vh := range want {
				if bytes.Equal(got[:], vh[:]) {
					ok = true
					break
				}
			}
			if !ok {
				return nil, fmt.Errorf("sidecar commitment not in tx blobVersionedHashes")
			}
			out = append(out, blobB)
		}
		return out, nil
	}

	// 4) Fallback to your CL path for each commitment expected by the tx
	// Use your existing BlobByCommitment with each tx.BlobHashes converted back to commitment if you stored them,
	// or first fetch commitments via consensus sidecars for the right slot (similar to your current code).
	// (You can call your BlobByCommitment once, but still verify got hash ∈ want)
	receipt, err := ethcli.TransactionReceipt(ctx, txHash)
	if err != nil {
		return nil, err
	}
	hdr, err := ethcli.HeaderByHash(ctx, receipt.BlockHash)
	if err != nil {
		return nil, err
	}
	if hdr.ParentBeaconRoot == nil {
		return nil, fmt.Errorf("no parent beacon root in header")
	}
	// ... your existing parentRoot -> slot -> /eth/v1/beacon/blob_sidecars/{slot} code here ...
	// make sure to match commitments -> versioned hashes against 'want' like above.
	return nil, fmt.Errorf("CL fallback not implemented in this sketch")
}

func SlotByTxHash(ctx context.Context, el *ethclient.Client, beaconURL string, txHash common.Hash) (uint64, error) {
	// 1) EL: receipt -> block hash
	r, err := el.TransactionReceipt(ctx, txHash)
	if err != nil {
		return 0, fmt.Errorf("receipt: %w", err)
	}
	if r.BlockHash == (common.Hash{}) {
		return 0, fmt.Errorf("tx not mined")
	}

	// 2) EL: header -> parentBeaconBlockRoot (EIP-4788)
	hdr, err := el.HeaderByHash(ctx, r.BlockHash)
	if err != nil {
		return 0, fmt.Errorf("header: %w", err)
	}
	if hdr.ParentBeaconRoot == nil {
		return 0, fmt.Errorf("parent beacon root missing (EL client too old?)")
	}
	parentRoot := hdr.ParentBeaconRoot.Hex()

	// 3) CL: resolve that root to a header (and slot)
	bc, err := eth2http.New(ctx, eth2http.WithAddress(beaconURL))
	if err != nil {
		return 0, fmt.Errorf("beacon client: %w", err)
	}

	// Block ID can be the block root hex directly
	// We use the typed API so we don't hand-roll JSON
	headers, err := bc.(interface {
		BeaconBlockHeaders(ctx context.Context, opts *eth2api.BeaconBlockHeaderOpts) ([]*eth2apiv1.BeaconBlockHeader, error)
	}).BeaconBlockHeaders(ctx, &eth2api.BeaconBlockHeaderOpts{Block: parentRoot})
	if err != nil {
		return 0, fmt.Errorf("beacon headers: %w", err)
	}
	if len(headers) == 0 {
		return 0, fmt.Errorf("no beacon header for %s", parentRoot)
	}

	parentSlot := uint64(headers[0].Header.Message.Slot)

	// 4) Our EL block’s beacon slot = parent slot + 1
	return parentSlot + 1, nil
}
