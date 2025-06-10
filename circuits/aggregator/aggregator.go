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
	"github.com/vocdoni/gnark-crypto-primitives/emulated/bn254/twistededwards/mimc7"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/types"
)

type AggregatorCircuit struct {
	ValidProofs        frontend.Variable                      `gnark:",public"`
	InputsHash         emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	ProofsInputsHashes [types.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]
	Proofs             [types.VotesPerBatch]groth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]
	VerificationKey    groth16.VerifyingKey[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT] `gnark:"-"`
}

// checkInputsHash recalculates the inputs hash using the MiMC hash function
// and compares it with the expected inputs hash. The inputs hash is calculated
// by hashing the inputs of the proofs.
func (c *AggregatorCircuit) checkInputsHash(api frontend.API) {
	// instantiate the MiMC hash function
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create MiMC hash function", err)
		return
	}
	// write the inputs hash to the MiMC hash function
	if err := hFn.Write(c.ProofsInputsHashes[:]...); err != nil {
		circuits.FrontendError(api, "failed to write inputs hash", err)
		return
	}
	// compare with the expected inputs hash
	hFn.AssertSumIsEqual(c.InputsHash)
}

// calculateWitnesses calculates the witnesses for the proofs. The first
// limb of the first input in the witness is set to 1 if the proof is valid,
// otherwise it is set to 0. The rest of the limbs are set to the inputs hash
// limbs.
func (c *AggregatorCircuit) calculateWitnesses(api frontend.API) []groth16.Witness[sw_bls12377.ScalarField] {
	// compose the witness for the inputs
	witnesses := []groth16.Witness[sw_bls12377.ScalarField]{}
	for i := range len(c.Proofs) {
		isValid := cmp.IsLess(api, i, c.ValidProofs)
		// create the witness for the proof
		witness := groth16.Witness[sw_bls12377.ScalarField]{
			Public: []emulated.Element[sw_bls12377.ScalarField]{
				{Limbs: []frontend.Variable{isValid, 0, 0, 0}},
			},
		}
		// if the proof is valid, the first limb of the first input in the
		// witness should be 1, otherwise it should be 0
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
		// add the witness to the list of witnesses
		witnesses = append(witnesses, witness)
	}
	return witnesses
}

// checkProofs checks that the proofs are valid and that the number of valid
// proofs is the expected. The verification of the proofs is done using the
// provided verification key and the public inputs of the witnesses. The number
// of valid proofs is calculated by counting the number of valid votes.
func (c *AggregatorCircuit) checkProofs(api frontend.API) {
	// initialize the verifier of the BLS12-377 curve
	verifier, err := groth16.NewVerifier[sw_bls12377.ScalarField, sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](api)
	if err != nil {
		circuits.FrontendError(api, "failed to create BLS12-377 verifier", err)
	}
	// verify each proof with the provided public inputs and the fixed
	// verification key
	witnesses := c.calculateWitnesses(api)
	for i := range len(c.Proofs) {
		// verify the proof
		if err := verifier.AssertProof(c.VerificationKey, c.Proofs[i], witnesses[i], groth16.WithCompleteArithmetic()); err != nil {
			circuits.FrontendError(api, "failed to verify proof", err)
		}
	}
}

func (c *AggregatorCircuit) Define(api frontend.API) error {
	c.checkInputsHash(api)
	// check the proofs
	c.checkProofs(api)
	return nil
}
