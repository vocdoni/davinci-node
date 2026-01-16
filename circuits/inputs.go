package circuits

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
)

// BallotProofNPubInputs is the number of public inputs for the ballot proof
// circom circuit.
const BallotProofNPubInputs = 1

// CircomInputs returns all values that are hashed to produce the public input
// needed to verify CircomProof, in a predefined order:
//
//	Process.ID
//	Process.BallotMode
//	Process.EncryptionKey (in Twisted Edwards format)
//	EmulatedVote.Address
//	EmulatedVote.Ballot (in Twisted Edwards format)
//	userWeight
func CircomInputs(api frontend.API,
	process Process[emulated.Element[sw_bn254.ScalarField]],
	vote EmulatedVote[sw_bn254.ScalarField],
) []emulated.Element[sw_bn254.ScalarField] {
	inputs := []emulated.Element[sw_bn254.ScalarField]{}
	inputs = append(inputs, process.SerializeForBallotProof(api)...)
	inputs = append(inputs, vote.SerializeForBallotProof(api)...)

	return inputs
}

// EmulatedVoteVerifierInputs returns all values that are hashed to produce the
// public input needed to verify VoteVerifier, in a predefined order and as
// emulated elements of the BN254 curve. The inputs are:
//
//	Process.ID
//	Process.CensusRoot
//	Process.BallotMode (in RTE format)
//	Process.EncryptionKey
//	EmulatedVote.Address
//	EmulatedVote.Ballot (in RTE format)
func EmulatedVoteVerifierInputs(
	process Process[emulated.Element[sw_bn254.ScalarField]],
	vote EmulatedVote[sw_bn254.ScalarField],
) []emulated.Element[sw_bn254.ScalarField] {
	inputs := []emulated.Element[sw_bn254.ScalarField]{}
	inputs = append(inputs, process.Serialize()...)
	inputs = append(inputs, vote.Serialize()...)
	return inputs
}

// VoteVerifierInputs returns the inputs hashed for VoteVerifier in this order:
//
//	Process.ID
//	Process.BallotMode
//	Process.EncryptionKey (in Twisted Edwards format)
//	Vote.Address
//	Vote.VoteID
//	Vote.Ballot (in Twisted Edwards format)
//	Vote.UserWeight
func VoteVerifierInputs(
	api frontend.API,
	process Process[frontend.Variable],
	vote Vote[frontend.Variable],
) []frontend.Variable {
	inputs := []frontend.Variable{}
	inputs = append(inputs, process.ID)
	inputs = append(inputs, process.BallotMode.Serialize()...)
	encKeyX, encKeyY := format.FromRTEtoTEVar(api, process.EncryptionKey.PubKey[0], process.EncryptionKey.PubKey[1])
	inputs = append(inputs, encKeyX, encKeyY)
	inputs = append(inputs, vote.Address, vote.VoteID)
	for _, ciphertext := range vote.Ballot {
		c1x, c1y := format.FromRTEtoTEVar(api, ciphertext.C1.X, ciphertext.C1.Y)
		c2x, c2y := format.FromRTEtoTEVar(api, ciphertext.C2.X, ciphertext.C2.Y)
		inputs = append(inputs, c1x, c1y, c2x, c2y)
	}
	inputs = append(inputs, vote.VoteWeight)
	return inputs
}
