package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/format"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
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

// BallotInputsHash helper function calculates the hash of the public inputs
// of the ballot proof circuit. This hash is used to verify the proof generated
// by the user and is also used to generate the voteID. The hash is calculated
// using the mimc7 hash function and includes the process ID, ballot mode,
// encryption key, address, commitment, nullifier, ballot and weight, in that
// particular order. The function transforms the inputs to the correct format:
//   - processID and address are converted to FF
//   - encryption key is converted to twisted edwards form
//   - ballot mode is converted to circuit ballot mode
//   - ballot is converted to twisted edwards form
func BallotInputsHash(
	processID types.HexBytes,
	ballotMode *types.BallotMode,
	encryptionKey ecc.Point,
	address types.HexBytes,
	commitment *types.BigInt,
	nullifier *types.BigInt,
	ballot *elgamal.Ballot,
	weight *types.BigInt,
) (*types.BigInt, error) {
	// safe address and processID
	ffAddress := address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	ffProcessID := processID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	// convert the encryption key to twisted edwards form
	encryptionKeyXTE, encryptionKeyYTE := format.FromRTEtoTE(encryptionKey.Point())
	// ballot mode as circuit ballot mode
	circuitBallotMode := circuits.BallotModeToCircuit(ballotMode)
	// compose a list with the inputs of the circuit to hash them
	inputsHash := []*big.Int{ffProcessID.MathBigInt()}                // process id
	inputsHash = append(inputsHash, circuitBallotMode.Serialize()...) // ballot mode serialized
	inputsHash = append(inputsHash,
		encryptionKeyXTE,        // encryption key x coordinate
		encryptionKeyYTE,        // encryption key y coordinate
		ffAddress.MathBigInt(),  // address
		commitment.MathBigInt(), // commitment
		nullifier.MathBigInt(),  // nullifier
	)
	// ballot (in twisted edwards form)
	inputsHash = append(inputsHash, ballot.FromRTEtoTE().BigInts()...)
	// weight
	inputsHash = append(inputsHash, weight.MathBigInt())
	// hash the inputs with mimc7
	ballotInputHash, err := mimc7.Hash(inputsHash, nil)
	if err != nil {
		return nil, fmt.Errorf("error hashing inputs: %v", err.Error())
	}
	return (*types.BigInt)(ballotInputHash), nil
}
