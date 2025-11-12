package census

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	imt "github.com/vocdoni/lean-imt-go"
	imtcensus "github.com/vocdoni/lean-imt-go/census"
	imtcircuit "github.com/vocdoni/lean-imt-go/circuit"
)

const testCSPSeed = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"

func CensusProofsForCircuitTest(votes []state.Vote, origin types.CensusOrigin, pid *types.ProcessID) (*big.Int, statetransition.CensusProofs, error) {
	log.Printf("generating testing census with '%s' origin", origin.String())
	var root *big.Int
	merkleProofs := [types.VotesPerBatch]imtcircuit.MerkleProof{}
	cspProofs := [types.VotesPerBatch]csp.CSPProof{}
	switch origin {
	case types.CensusOriginMerkleTreeOffchainStaticV1:
		// generate the census merkle tree and set the census root
		census, err := CensusMerkleTreeForTest(votes)
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error generating census merkle tree: %w", err)
		}
		var ok bool
		if root, ok = census.Root(); !ok {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error getting census merkle tree root")
		}
		// generate the merkle tree census proofs for each voter and fill the
		// csp proofs with dummy data
		for i := range types.VotesPerBatch {
			if i < len(votes) {
				addr := common.BigToAddress(votes[i].Address)
				mkproof, err := census.GenerateProof(addr)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("error generating census proof for address %s: %w", addr.Hex(), err)
				}
				merkleProofs[i] = imtcircuit.CensusProofToMerkleProof(mkproof)
			} else {
				merkleProofs[i] = statetransition.DummyMerkleProof()
			}
			cspProofs[i] = statetransition.DummyCSPProof()
		}
	default:
		// instance a csp for testing
		eddsaCSP, err := csp.New(origin, []byte(testCSPSeed))
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to create csp: %w", err)
		}
		// get the root and generate the csp proofs for each voter
		root = eddsaCSP.CensusRoot().Root.BigInt().MathBigInt()
		for i := range types.VotesPerBatch {
			// add dummy merkle proof
			merkleProofs[i] = statetransition.DummyMerkleProof()
			if i < len(votes) {
				// generate csp proof for the voter address
				addr := common.BytesToAddress(votes[i].Address.Bytes())
				cspProof, err := eddsaCSP.GenerateProof(pid, addr)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to generate census proof: %w", err)
				}
				// convert to gnark csp proof
				gnarkCSPProof, err := csp.CensusProofToCSPProof(types.CensusOriginCSPEdDSABN254V1.CurveID(), cspProof)
				if err != nil {
					return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to convert census proof to gnark proof: %w", err)
				}
				cspProofs[i] = *gnarkCSPProof
			} else {
				cspProofs[i] = statetransition.DummyCSPProof()
			}
		}
	}
	return root, statetransition.CensusProofs{
		MerkleProofs: merkleProofs,
		CSPProofs:    cspProofs,
	}, nil
}

func CensusMerkleTreeForTest(votes []state.Vote) (*imtcensus.CensusIMT, error) {
	// generate the census with voters information
	votersData := map[*big.Int]*big.Int{}
	for _, v := range votes {
		votersData[v.Address] = v.Weight
	}

	// Create a unique directory name to avoid lock conflicts
	// Include timestamp and process info for uniqueness
	timestamp := time.Now().UnixNano()
	censusDir := fmt.Sprintf("../assets/census_%d", timestamp)

	// Ensure the assets directory exists
	if err := os.MkdirAll("../assets", 0o755); err != nil {
		return nil, fmt.Errorf("failed to create assets directory: %w", err)
	}

	// Initialize the census merkle tree
	censusTree, err := imtcensus.NewCensusIMTWithPebble(censusDir, imt.PoseidonHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create census IMT: %w", err)
	}

	// Clean up the census directory when done
	defer func() {
		if err := censusTree.Close(); err != nil {
			log.Printf("Warning: failed to close census IMT: %v", err)
		}
		if err := os.RemoveAll(censusDir); err != nil {
			log.Printf("Warning: failed to cleanup census directory %s: %v", censusDir, err)
		}
	}()

	bAddresses, bWeights := []common.Address{}, []*big.Int{}
	for address, weight := range votersData {
		bAddresses = append(bAddresses, common.BigToAddress(address))
		bWeights = append(bWeights, weight)
	}
	if err := censusTree.AddBulk(bAddresses, bWeights); err != nil {
		return nil, fmt.Errorf("failed to add bulk to census IMT: %w", err)
	}
	return censusTree, nil
}

// ServeCensusMerkleTreeForTest starts an HTTP server to serve the census
// merkle tree dump for testing purposes. It returns the URL where the census
// can be accessed. The server will run until the provided context is canceled.
func ServeCensusMerkleTreeForTest(ctx context.Context, votes []state.Vote) (*big.Int, string, error) {
	// create the census merkle tree
	census, err := CensusMerkleTreeForTest(votes)
	if err != nil {
		return nil, "", fmt.Errorf("error generating census merkle tree: %w", err)
	}
	// dump the census merkle tree to assets for external use
	dump, err := census.Dump()
	if err != nil {
		return nil, "", fmt.Errorf("error dumping census merkle tree: %w", err)
	}
	// convert to json
	dumpJSON, err := json.Marshal(dump)
	if err != nil {
		return nil, "", fmt.Errorf("error marshaling census dump to json: %w", err)
	}

	// create a handler to serve the census dump
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(dumpJSON); err != nil {
			log.Printf("Warning: failed to write census dump response: %v", err)
		}
	})

	// get an available port
	port, err := freePort()
	if err != nil {
		return nil, "", fmt.Errorf("error getting free port: %w", err)
	}

	addr := fmt.Sprintf("localhost:%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// start the server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Warning: failed to serve census merkle tree: %v", err)
		}
	}()

	// shutdown server on context cancel
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Warning: error shutting down server: %v", err)
		}
	}()

	dumpURL := fmt.Sprintf("http://%s", addr)
	return dump.Root, dumpURL, nil
}

func freePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}
