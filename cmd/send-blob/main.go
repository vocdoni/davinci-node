package main

import (
	"context"
	"fmt"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethparams "github.com/ethereum/go-ethereum/params"
	"github.com/spf13/pflag"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
	"github.com/vocdoni/davinci-node/web3"
	"github.com/vocdoni/davinci-node/web3/txmanager"
)

const (
	blobSize = 131072 // 4096 * 32 bytes
	timeout  = 30 * time.Second
)

func main() {
	rpcURLs := pflag.StringSlice("rpc", []string{"https://ethereum-sepolia-rpc.publicnode.com"}, "Execution-layer JSON-RPC endpoint (required)")
	privKey := pflag.String("privkey", "", "Hex-encoded Ethereum private key (required)")
	toStr := pflag.String("to", "", "Optional destination address (defaults to sender)")
	numBlobs := pflag.Int("n", 1, "Number of random blobs to include")
	wait := pflag.Bool("wait", true, "Wait for tx to be mined")
	capi := pflag.String("capi", "https://ethereum-sepolia-beacon-api.publicnode.com", "Consensus API URL (required)")

	pflag.Parse()

	if len(*rpcURLs) == 0 || *privKey == "" || *capi == "" {
		pflag.Usage()
		return
	}

	log.Init("debug", "stdout", nil)

	// Basic logging init (adjust if you have a custom init)
	log.Infow("starting sendblob")

	// 1) Init Contracts
	contracts, err := web3.New(*rpcURLs, *capi, 1.0)
	if err != nil {
		log.Fatalf("init web3: %v", err)
	}
	if err := contracts.LoadContracts(nil); err != nil {
		log.Fatalf("failed to load contracts: %w", err)
	}

	if err := contracts.SetAccountPrivateKey(*privKey); err != nil {
		log.Fatalf("set privkey: %v", err)
	}
	from := contracts.AccountAddress()

	// Init transaction manager
	txmCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	txm, err := txmanager.New(txmCtx, contracts.Web3Pool(), contracts.Client(), contracts.Signer(),
		txmanager.DefaultConfig(contracts.ChainID))
	if err != nil {
		log.Fatalf("failed to create transaction manager: %w", err)
	}
	txm.Start(txmCtx)
	contracts.SetTxManager(txm)

	// Destination address
	var to common.Address
	if *toStr == "" {
		to = from
	} else {
		to = common.HexToAddress(*toStr)
	}

	// 2) Build blobs
	blobs := make([]*types.Blob, *numBlobs)
	for i := range blobs {
		blobs[i] = RandomBlob()
	}

	var sidecar *types.BlobTxSidecar
	switch contracts.ChainID {
	case gethparams.SepoliaChainConfig.ChainID.Uint64():
		sidecar, err = types.ComputeBlobTxSidecar(types.BlobTxSidecarVersion1, blobs)
	default: // mainnet, for example
		sidecar, err = types.ComputeBlobTxSidecar(types.BlobTxSidecarVersion0, blobs)
	}
	if err != nil {
		log.Fatalf("build sidecar: %v", err)
	}

	// 3) Send tx
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	processID := types.NewProcessID(from, types.ProcessIDVersion(uint32(contracts.ChainID), to), 1)

	data, err := contracts.ProcessRegistryABI().Pack("submitStateTransition", processID, []byte{0x1}, []byte{0x1})
	if err != nil {
		log.Fatalf("failed to pack data: %w", err)
	}

	// Simulate tx to the contract to check if it will fail and get the root
	// cause of the failure if it does
	if err := contracts.SimulateProcessTransition(ctx, processID, []byte{0x1}, []byte{0x1}, sidecar); err != nil {
		log.Debugw("failed to simulate state transition", "error", err, "processID", processID.String())
	}

	tx, err := contracts.NewEIP4844Transaction(ctx, to, data, sidecar)
	if err != nil {
		log.Fatalf("failed to build blob tx: %v", err)
	}

	log.Infow("sending blob tx",
		"Nonce", tx.Nonce(),
		"BlobGas", tx.BlobGas(),
		"BlobGasFeeCap", tx.BlobGasFeeCap(),
		"Gas", tx.Gas(),
		"GasFeeCap", tx.GasFeeCap(),
		"GasTipCap", tx.GasTipCap(),
		"size", tx.Size(),
	)

	// Broadcast
	if err := contracts.Client().SendTransaction(ctx, tx); err != nil {
		log.Fatalf("send blobs tx: %v", err)
	}
	log.Infow("blob tx sent", "hash", tx.Hash().Hex(), "from", from.Hex(), "to", to.Hex())

	// Print commitments for reference
	for i, c := range sidecar.Commitments {
		log.Infow("commitment", "index", i, "hash", fmt.Sprintf("0x%x", c))
	}

	// 4) Optionally wait
	if *wait {
		if err := contracts.WaitTxByHash(tx.Hash(), 2*time.Minute); err != nil {
			log.Errorf("wait tx %s: %v", tx.Hash().Hex(), err)
		} else {
			log.Infow("tx mined", "hash", tx.Hash().Hex())
		}
	}

	// Print blob hashes for reference
	// (available from tx.Data() only after sign? easier: recompute in SendBlobTx or log during build)
	// Here, just echo size:
	log.Infow("done", "numBlobs", *numBlobs, "blobSizeBytes", blobSize, "tx", tx.Hash().Hex())

	ctx2, cancel2 := context.WithTimeout(context.Background(), timeout*2)
	defer cancel2()

	// Get blob by commitment
	for i, c := range sidecar.Commitments {
		blobs, err := contracts.BlobsByTxHash(ctx2, tx.Hash())
		if err != nil {
			log.Errorf("get blob %d by commitment 0x%x: %v", i, c, err)
			continue
		}
		found := false
		for _, blob := range blobs {
			if blob.Commitment == c {
				log.Infow("blob retrieved", "index", i, "commitment", fmt.Sprintf("0x%x", c), "size", len(blob.Blob), "preview", preview(blob.Blob[:], 32))
				found = true
			}
		}
		if !found {
			log.Errorf("blob with commitment %s not found in tx %s, it only contained these commitments: %+v", c, tx.Hash(),
				types.SliceOf(blobs, func(b *types.BlobSidecar) types.KZGCommitment { return b.Commitment }))
		}
	}
}

// Optionally, helper to hex-print a few bytes
func preview(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return hexutil.Encode(b[:n])
}

func RandomBlob() *types.Blob {
	const feSize = params.BlobTxBytesPerFieldElement // 32
	out := new(types.Blob)
	var el fr.Element
	for i := range params.BlobTxFieldElementsPerBlob {
		el.MustSetRandom()                             // uses crypto/rand.Reader
		copy(out[i*feSize:(i+1)*feSize], el.Marshal()) // big-endian canonical bytes
	}
	return out
}
