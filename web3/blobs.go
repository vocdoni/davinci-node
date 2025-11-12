package web3

import (
	"bytes"
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

	data := common.FromHex("0xd71d072ea62e32147e9c1ea76da552be6e0636f1984143af5c59723d000000000000000000000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000018027afc76d7e6cf0ece2db678632942628e88070cf6ecb6c95b924f5a988e258811b70b38057bc1cf0e8e6ca303d285a63ef89de1ed87a6f926ac81ef6b8577a282b0ba673f28011b122be013fb3e4564832e801211e1a62098916e0a5355276ab0c52c9203838f4f0dd6ce3c50420f5ce4ff447b46c0f6838cc7e6312b95677e204d0271915b247175fc2e2ed11d3b4c2153da8612103010d66e069a033b454be286893968617c8080ea3f48cba90404e70a0a51db052b5628132f6d4a55ac76f2d435e245fe43951e2ba6bc6fe351745200d994b33e7568edf232069ab7c33061ed117ba35aa1de36f9f9a1c72aab6b99510e6a23f7ccf0540e35c67d8292e7e11ba4141e7c1b864f37efc65dcffb518be44be846d4bc9add2c5bf2c3316c6902c2b63b322094dc4a383cb1778ab1db0531966883a8b3e7369069df6242851eb2ad0edbdc40c4d7b7d8afc300f1d98354d68a760b25d86010a4cdc94e1f285af05c167a9dcd6bc05de0db1e0e8e327b77872fda8dca6c64c69d6ecc1d72ecb8a00000000000000000000000000000000000000000000000000000000000002201fbd37ed179906a924d81a11009bb63fa2e3e926b0a01c632abc10c96b4d58680b3955956ba30d4222d70fecce638b2560b2c0018263fc2aa324202fa5ff3d0100000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000000016e15f312a1557cb4963f0dc9a34e256dbfaed29a58502bd7cbe11d7b0f324e00000000000000000000000000000000000000000000000029747c4876fbd1b7000000000000000000000000000000000000000000000000664a5059b9042933000000000000000000000000000000000000000000000000437d8b1f3682269d000000000000000000000000000000000000000000000000528ce0f6b730dded000000000000000000000000000000000000000000000000000000000000016000000000000000000000000000000000000000000000000000000000000001c0000000000000000000000000000000000000000000000000000000000000003091d468dd3f7f32467136a0afd1cdd5c24ff3759c9e171208a3ab6d5651c54c2784d40b9b265b5894c7bc6c575d08071b0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000308f4bea2c2db7660e333425adaa879eb717d1f2a9cbf55cafd796290b1749846d22e22a1b1c2c800e1497adfd97be707500000000000000000000000000000000")
	// Estimate execution gas (must include blob fields)
	call := ethereum.CallMsg{
		From:          from,
		To:            &to,
		GasFeeCap:     gasFeeCap, // 1559
		GasTipCap:     tipCap,    // 1559
		Data:          data,
		BlobGasFeeCap: blobFeeCap,           // <= REQUIRED for 4844
		BlobHashes:    sidecar.BlobHashes(), // <= REQUIRED for 4844
	}
	gasLimit, err := c.cli.EstimateGas(ctx, call)
	if err != nil {
		if strings.Contains(err.Error(), "execution reverted") {
			gasLimit = txmanager.DefaultGasFallback
		} else {
			return nil, nil, fmt.Errorf("estimate gas for blobs tx: %w", err)
		}
	}

	// Create & sign blob tx
	txData := &types.BlobTx{
		ChainID:    uint256.NewInt(c.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.MustFromBig(tipCap),
		GasFeeCap:  uint256.MustFromBig(gasFeeCap),
		Gas:        gasLimit,
		To:         to,
		Data:       data,
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

	// Fee building
	// Tip suggestion (EIP-1559)
	tipCap, err := c.cli.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tip cap: %w", err)
	}
	// Estimate execution gas, include blob hashes so any contract logic that
	// references them (e.g. checks) isn't under-estimated.
	gas := c.txManager.EstimateGas(ctx, ethereum.CallMsg{
		From:       c.AccountAddress(),
		To:         &to,
		GasTipCap:  tipCap,
		Data:       data,
		BlobHashes: blobsSidecar.BlobHashes(),
	}, txmanager.DefaultGasEstimateOpts, txmanager.DefaultCancelGasFallback)

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
