package main

import (
	"context"
	"fmt"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/params"
	"github.com/spf13/pflag"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/web3"
)

const (
	blobSize = 131072 // 4096 * 32 bytes
	timeout  = 30 * time.Second
)

func main() {
	rpcURL := pflag.String("rpc", "https://ethereum-sepolia-rpc.publicnode.com", "Execution-layer JSON-RPC endpoint (required)")
	privKey := pflag.String("privkey", "", "Hex-encoded Ethereum private key (required)")
	toStr := pflag.String("to", "", "Optional destination address (defaults to sender)")
	numBlobs := pflag.Int("n", 1, "Number of random blobs to include")
	wait := pflag.Bool("wait", true, "Wait for tx to be mined")
	capi := pflag.String("capi", "https://ethereum-sepolia-beacon-api.publicnode.com", "Consensus API URL (required)")
	pflag.Parse()

	if *rpcURL == "" || *privKey == "" || *capi == "" {
		pflag.Usage()
		return
	}

	log.Init("debug", "stdout", nil)

	// Basic logging init (adjust if you have a custom init)
	log.Infow("starting sendblob")

	// 1) Init Contracts
	contracts, err := web3.New([]string{*rpcURL}, *capi)
	if err != nil {
		log.Fatalf("init web3: %v", err)
	}
	if err := contracts.SetAccountPrivateKey(*privKey); err != nil {
		log.Fatalf("set privkey: %v", err)
	}
	from := contracts.AccountAddress()

	// Destination address
	var to common.Address
	if *toStr == "" {
		to = from
	} else {
		to = common.HexToAddress(*toStr)
	}

	// 2) Build blobs
	blobs := make([][]byte, *numBlobs)
	for i := range blobs {
		b := RandomBlob()
		blobs[i] = b
	}

	sidecar, _, err := web3.BuildBlobsSidecar(blobs)
	if err != nil {
		log.Fatalf("build sidecar: %v", err)
	}

	// 3) Send tx
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tx, commitments, err := contracts.SendBlobTx(ctx, to, sidecar)
	if err != nil {
		log.Fatalf("send blob tx: %v", err)
	}
	log.Infow("blob tx sent", "hash", tx.Hash().Hex(), "from", from.Hex(), "to", to.Hex())

	// Print commitments for reference
	for i, c := range commitments {
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
	for i, c := range commitments {
		blob, err := contracts.BlobByCommitment(ctx2, tx.Hash(), fmt.Sprintf("0x%x", c))
		if err != nil {
			log.Errorf("get blob %d by commitment 0x%x: %v", i, c, err)
			continue
		}
		log.Infow("blob retrieved", "index", i, "commitment", fmt.Sprintf("0x%x", c), "size", len(blob), "preview", preview(blob, 32))
	}
}

// Optionally, helper to hex-print a few bytes
func preview(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return hexutil.Encode(b[:n])
}

func RandomBlob() []byte {
	const feSize = params.BlobTxBytesPerFieldElement              // 32
	out := make([]byte, params.BlobTxFieldElementsPerBlob*feSize) // 131072
	var el fr.Element
	for i := 0; i < params.BlobTxFieldElementsPerBlob; i++ {
		el.MustSetRandom()                             // uses crypto/rand.Reader
		copy(out[i*feSize:(i+1)*feSize], el.Marshal()) // big-endian canonical bytes
	}
	return out
}
