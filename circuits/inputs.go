package circuits

import (
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
)

// BallotProofNPubInputs is the number of public inputs for the ballot proof
// circom circuit.
const BallotProofNPubInputs = 3

// BallotHash returns the inputs hashed for BallotHash in this order:
//
//	Process.ID
//	Process.BallotMode
//	Process.EncryptionKey (in Twisted Edwards format)
//	Vote.Address
//	Vote.VoteID
//	Vote.Ballot (in Twisted Edwards format)
//	Vote.UserWeight
func BallotHash(
	api frontend.API,
	process Process[frontend.Variable],
	vote Vote[frontend.Variable],
) []frontend.Variable {
	inputs := []frontend.Variable{}
	inputs = append(inputs, process.ID)
	inputs = append(inputs, process.BallotMode)
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
