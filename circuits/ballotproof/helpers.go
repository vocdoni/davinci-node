package ballotproof

import (
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// CommitmentAndNullifier calculates the commitment and nullifier for a ballot
// using the address, processID, and secret. The commitment is calculated
// hashing the address, processID, and secret together using the poseidon
// hash function. The nullifier is calculated by hashing the commitment and
// secret together using the poseidon hash function. The function returns the
// commitment and nullifier as BigInt pointers, or an error if the hashing
// fails.
func CommitmentAndNullifier(address, processID, secret *types.BigInt) (*types.BigInt, *types.BigInt, error) {
	commitment, err := poseidon.Hash([]*big.Int{
		address.ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt(),
		processID.ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt(),
		secret.ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt(),
	})
	if err != nil {
		return nil, nil, err
	}
	nullifier, err := poseidon.Hash([]*big.Int{
		commitment,
		secret.ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt(),
	})
	if err != nil {
		return nil, nil, err
	}
	return (*types.BigInt)(commitment), (*types.BigInt)(nullifier), nil
}
