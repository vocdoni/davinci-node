package results

import (
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/merkleproof"
)

var failbackMaxValue = frontend.Variable(2 << 24) // 2^24

// HashFn is the hash function used in the circuit. It should the equivalent
// hash function used in the state package (state.HashFn).
var HashFn = poseidon.MultiHash

type ResultsVerifierCircuit struct {
	MaxValue        frontend.Variable       // public
	ResultsAddProof merkleproof.MerkleProof // public
	ResultsSubProof merkleproof.MerkleProof // public
	CiphertextsAdd  circuits.Ballot         // public
	CiphertextsSub  circuits.Ballot         // public
	ResultsAdd      circuits.Ballot         // public
	ResultsSub      circuits.Ballot         // public
}

func (c *ResultsVerifierCircuit) Define(api frontend.API) error {
	_ = api.Select(api.IsZero(c.MaxValue), failbackMaxValue, c.MaxValue)
	return nil
}
