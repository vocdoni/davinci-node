package spec

import (
	"math/big"

	"github.com/vocdoni/davinci-node/spec/hash"
)

// StateRoot computes the state root hash for the process parameters.
func StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, ballotMode *big.Int) (*big.Int, error) {
	return hash.StateRoot(processID, censusOrigin, pubKeyX, pubKeyY, ballotMode)
}
