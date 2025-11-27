package test

import (
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
	imtcircuit "github.com/vocdoni/lean-imt-go/circuit"
)

// fixed seed for CSP testing
const testCSPSeed = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"

// CensusProofsForCircuitTest generates the census proofs required for the
// state transition circuit tests based on the provided votes and census
// origin. It returns the census root, the generated census proofs ready to
// be used in the statetransition circuit, and an error if the process fails.
// It supports both Merkle tree and CSP-based by initializing a CSP instance
// or generating a Merkle tree census as needed.
func CensusProofsForCircuitTest(
	votes []state.Vote,
	origin types.CensusOrigin,
	pid types.ProcessID,
) (*big.Int, statetransition.CensusProofs, error) {
	log.Printf("generating testing census with '%s' origin", origin.String())
	var root *big.Int
	merkleProofs := [params.VotesPerBatch]imtcircuit.MerkleProof{}
	cspProofs := [params.VotesPerBatch]csp.CSPProof{}
	switch {
	case origin.IsMerkleTree():
		// generate the census merkle tree and set the census root
		census, err := CensusIMTForTest(votes)
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error generating census merkle tree: %w", err)
		}
		var ok bool
		if root, ok = census.Root(); !ok {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("error getting census merkle tree root")
		}
		// generate the merkle tree census proofs for each voter and fill the
		// csp proofs with dummy data
		for i := range params.VotesPerBatch {
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
	case origin.IsCSP():
		// instance a csp for testing
		eddsaCSP, err := csp.New(origin, []byte(testCSPSeed))
		if err != nil {
			return nil, statetransition.CensusProofs{}, fmt.Errorf("failed to create csp: %w", err)
		}
		// get the root and generate the csp proofs for each voter
		root = eddsaCSP.CensusRoot().Root.BigInt().MathBigInt()
		for i := range params.VotesPerBatch {
			// add dummy merkle proof
			merkleProofs[i] = statetransition.DummyMerkleProof()
			if i < len(votes) {
				// generate csp proof for the voter address
				addr := common.BytesToAddress(votes[i].Address.Bytes())
				cspProof, err := eddsaCSP.GenerateProof(pid, addr, new(types.BigInt).SetBigInt(votes[i].Weight))
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
	default:
		return nil, statetransition.CensusProofs{}, fmt.Errorf("unsupported census origin: %s", origin.String())
	}
	return root, statetransition.CensusProofs{
		MerkleProofs: merkleProofs,
		CSPProofs:    cspProofs,
	}, nil
}
