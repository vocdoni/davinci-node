// aggregator package contains the Gnark circuit defiinition that aggregates
// some votes and proves the validity of the aggregation. The circuit checks
// every single verification proof generating a single proof for the whole
// aggregation. It also checks that the number of valid votes and that the
// hash of the witnesses is the expected.
package aggregator

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/gnark-crypto-primitives/hash/emulated/bn254/poseidon"
)

type AggregatorCircuit struct {
	VotersCount     frontend.Variable                      `gnark:",public"`
	BatchHash       emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	BallotHashes    [params.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]
	Proofs          [params.VotesPerBatch]groth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]
	VerificationKey groth16.VerifyingKey[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT] `gnark:"-"`
}

// VoteMask returns the latch-based mask for real vote slots.
func (c *AggregatorCircuit) VoteMask(api frontend.API) []frontend.Variable {
	api.AssertIsLessOrEqual(0, c.VotersCount)
	api.AssertIsLessOrEqual(c.VotersCount, params.VotesPerBatch)
	mask := make([]frontend.Variable, params.VotesPerBatch)
	// if VotersCount > 0, the first vote is real
	isReal := api.Sub(1, api.IsZero(c.VotersCount))
	for i := range params.VotesPerBatch {
		mask[i] = isReal
		// if VotersCount == i+1, the next vote is dummy
		isEnd := api.IsZero(api.Sub(c.VotersCount, i+1))
		isReal = api.Mul(isReal, api.Sub(1, isEnd))
	}
	return mask
}

// checkBatchHash recalculates the batch hash using the Poseidon hash function
// and compares it with the expected batch hash. The batch hash is calculated
// by hashing the ballot hashes.
func (c *AggregatorCircuit) checkBatchHash(api frontend.API) {
	if err := poseidon.AssertMultiHashEqual(api, c.BallotHashes[:], c.BatchHash); err != nil {
		circuits.FrontendError(api, "failed to assert Poseidon batch hash", err)
		return
	}
}

// calculateWitnesses calculates the witnesses for the proofs. The first
// limb of the first input in the witness is set to 1 for real vote slots,
// otherwise it is set to 0 for dummy slots. The rest of the limbs are set to
// the inputs hash limbs.
func (c *AggregatorCircuit) calculateWitnesses(api frontend.API) []groth16.Witness[sw_bls12377.ScalarField] {
	// compose the witness for the inputs
	witnesses := []groth16.Witness[sw_bls12377.ScalarField]{}
	isRealVote := c.VoteMask(api)

	for i := range len(c.Proofs) { // len(c.Proofs) is params.VotesPerBatch
		// create the witness for the proof
		witness := groth16.Witness[sw_bls12377.ScalarField]{
			Public: []emulated.Element[sw_bls12377.ScalarField]{
				{Limbs: []frontend.Variable{isRealVote[i], 0, 0, 0}},
			},
		}
		// Real slots use the provided input hash. Dummy slots use the dummy hash.
		for j, inputsHashLimb := range c.BallotHashes[i].Limbs {
			dummyLimb := 0
			if j == 0 {
				dummyLimb = 1
			}
			finalLimb := api.Select(isRealVote[i], inputsHashLimb, dummyLimb)
			witness.Public = append(witness.Public, emulated.Element[sw_bls12377.ScalarField]{
				Limbs: []frontend.Variable{finalLimb, 0, 0, 0},
			})
		}
		// add the witness to the list of witnesses
		witnesses = append(witnesses, witness)
	}
	return witnesses
}

// checkProofs checks that the proofs are valid using the provided verification
// key and the public inputs of the witnesses. Real vote slots are counted by
// VotersCount and any remaining slots are padded with dummy proofs.
func (c *AggregatorCircuit) checkProofs(api frontend.API) {
	// initialize the verifier of the BLS12-377 curve
	verifier, err := groth16.NewVerifier[sw_bls12377.ScalarField, sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](api)
	if err != nil {
		circuits.FrontendError(api, "failed to create BLS12-377 verifier", err)
		return
	}
	// verify each proof with the provided public inputs and the fixed
	// verification key
	witnesses := c.calculateWitnesses(api)
	for i := range len(c.Proofs) {
		// verify the proof
		if err := verifier.AssertProof(c.VerificationKey, c.Proofs[i], witnesses[i],
			groth16.WithCompleteArithmetic(), groth16.WithSubgroupCheck()); err != nil {
			circuits.FrontendError(api, "failed to verify proof", err)
			return
		}
	}
}

func (c *AggregatorCircuit) Define(api frontend.API) error {
	c.checkBatchHash(api)
	c.checkProofs(api)
	return nil
}
