package ballotproof

import (
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// CommitmentAndNullifier calculates the commitment and nullifier for a ballot
// using the address, processID, and secret.
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
