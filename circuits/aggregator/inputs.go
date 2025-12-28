package aggregator

import (
	"math/big"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

type AggregatorInputs struct {
	Proofs                 [types.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]
	ProofsInputHash        [types.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]
	AggBallots             []*storage.AggregatorBallot
	VerifiedBallots        []*storage.VerifiedBallot
	ProcessedKeys          [][]byte
	ProofsInputsHashInputs []*big.Int
}

// InputsHash hashes all subhashes and returns the final hash
func (ai *AggregatorInputs) InputsHash() (*big.Int, error) {
	hashes := ai.ProofsInputsHashInputs
	// Padding with 1s to fill the array
	for len(hashes) < types.VotesPerBatch {
		hashes = append(hashes, big.NewInt(1))
	}
	finalHash, err := mimc7.Hash(hashes, nil)
	if err != nil {
		return nil, err
	}
	return finalHash, nil
}
