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
	"github.com/consensys/gnark/std/math/cmp"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
)

type AggregatorCircuit struct {
	ValidProofs        frontend.Variable                                              `gnark:",public"`
	ProofsInputsHashes [circuits.VotesPerBatch]emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	Proofs             [circuits.VotesPerBatch]groth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]
	VerificationKey    groth16.VerifyingKey[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT] `gnark:"-"`
}

// checkProofs checks that the proofs are valid and that the number of valid
// proofs is the expected. The verification of the proofs is done using the
// provided verification key and the public inputs of the witnesses. The number
// of valid proofs is calculated by counting the number of valid votes. A vote
// is considered valid if the first limb of the first public input in the
// witness is 1, otherwise it is considered invalid. The number of valid votes
// is calculated by adding the result of the AND operation between the last
// valid vote and the current vote. The number of valid votes is the expected
// number of valid proofs. Only the first n proofs can be valid, so the
// counting stops after the first invalid proof.
func (c AggregatorCircuit) checkProofs(api frontend.API) {
	// initialize the verifier of the BLS12-377 curve
	verifier, err := groth16.NewVerifier[sw_bls12377.ScalarField, sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](api)
	if err != nil {
		circuits.FrontendError(api, "failed to create BLS12-377 verifier", err)
	}
	// verify each proof with the provided public inputs and the fixed
	// verification key
	for i := range len(c.Proofs) {
		isValid := cmp.IsLess(api, i, c.ValidProofs)
		// create the witness for the proof
		witness := groth16.Witness[sw_bls12377.ScalarField]{
			Public: []emulated.Element[sw_bls12377.ScalarField]{
				{Limbs: []frontend.Variable{isValid, 0, 0, 0}},
			},
		}
		for j, inputsHashLimb := range c.ProofsInputsHashes[i].Limbs {
			dummyLimb := 0
			if j == 0 {
				dummyLimb = 1
			}
			finalLimb := api.Select(isValid, inputsHashLimb, dummyLimb)
			witness.Public = append(witness.Public, emulated.Element[sw_bls12377.ScalarField]{
				Limbs: []frontend.Variable{finalLimb, 0, 0, 0},
			})
		}
		// if the proof is valid, the first limb of the first input in the
		// witness should be 1, otherwise it should be 0
		// but only accepts the n first valid proofs so, to update the
		// number of valid votes correctly, we add the result of the AND
		// operation between the last valid vote and the current vote
		// count the number of valid votes
		// verify the proof
		if err := verifier.AssertProof(c.VerificationKey, c.Proofs[i], witness, groth16.WithCompleteArithmetic()); err != nil {
			circuits.FrontendError(api, "failed to verify proof", err)
		}
	}
}

func (c AggregatorCircuit) Define(api frontend.API) error {
	// check the proofs
	c.checkProofs(api)
	return nil
}
