package circuits

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/gnark-crypto-primitives/emulated/bn254/twistededwards/mimc7"
	"github.com/vocdoni/davinci-node/types"
)

// VotersHashes is a struct that contains the hashes of the voters. It is used
// to calculate the sum of the hashes and to generate the witness for the i-th
// voter of a batch of voters.
type VotersHashes struct {
	Hashes [types.VotesPerBatch]emulated.Element[sw_bn254.ScalarField]
}

// VoterHashFn function calculates the mimc7 hash of the provided inputs. It
// returns the hash of the inputs.
func VoterHashFn(api frontend.API, inputs ...emulated.Element[sw_bn254.ScalarField]) emulated.Element[sw_bn254.ScalarField] {
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		FrontendError(api, "failed to create emulated MiMC hash function", err)
	}
	if err := hFn.Write(inputs...); err != nil {
		FrontendError(api, "failed to write inputs to emulated MiMC hash function", err)
	}
	return hFn.Sum()
}

func (vh VotersHashes) Sum(api frontend.API) emulated.Element[sw_bn254.ScalarField] {
	return VoterHashFn(api, vh.Hashes[:]...)
}

// AssertSumIsEqual method calculates the mimc7 sum of the current hashes and
// asserts that it is equal to the expected provided value.
func (vh VotersHashes) AssertSumIsEqual(api frontend.API, expected emulated.Element[sw_bn254.ScalarField]) {
	// initialize the hash function
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		FrontendError(api, "failed to create emulated MiMC hash function", err)
	}
	// write hashes and assert the sum
	if err := hFn.Write(vh.Hashes[:]...); err != nil {
		FrontendError(api, "failed to write inputs to emulated MiMC hash function", err)
	}
	hFn.AssertSumIsEqual(expected)
}

// ToWitnessBW6761 method calculates the witness using the current
// hashes.  It takes the sum of all hashes and,  after reduce it
// in its field, splits it in 4 elements, each of bw6761 element has as first
// limb each limb of the original bn254 hash.
//
//	  witness = {
//			Public: [
//				[hash.Limbs[0], 0, 0, 0, 0, 0],
//	  			[hash.Limbs[1], 0, 0, 0, 0, 0],
//				[hash.Limbs[2], 0, 0, 0, 0, 0],
//				[hash.Limbs[3], 0, 0, 0, 0, 0],
//			],
//	  }
func (vh VotersHashes) ToWitnessBW6761(api frontend.API) (
	groth16.Witness[sw_bw6761.ScalarField], error,
) {
	field, err := emulated.NewField[sw_bn254.ScalarField](api)
	if err != nil {
		FrontendError(api, "failed to create field", err)
	}
	sumInEmulatedBN254 := vh.Sum(api)
	reducedSum := field.Reduce(&sumInEmulatedBN254)
	witness := groth16.Witness[sw_bw6761.ScalarField]{}
	// split the hash in 4 elements, each of bw6761 element has as first
	// limb each limb of the original bn254 hash, including the dummy elements
	for _, limb := range reducedSum.Limbs {
		// store the new element with the final limb and 0 for the rest
		witness.Public = append(witness.Public, emulated.Element[sw_bw6761.ScalarField]{
			Limbs: []frontend.Variable{limb, 0, 0, 0, 0, 0},
		})
	}
	return witness, nil
}
