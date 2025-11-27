package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

// BallotInputsHash helper function calculates the hash of the public inputs
// of the ballot proof circuit. This hash is used to verify the proof generated
// by the user and is also used to generate the voteID. The hash is calculated
// using the mimc7 hash function and includes the process ID, ballot mode,
// encryption key, address, ballot and weight, in that particular order. The
// function transforms the inputs to the correct format:
//   - processID and address are converted to FF
//   - encryption key is converted to twisted edwards form
//   - ballot mode is converted to circuit ballot mode
//   - ballot is converted to twisted edwards form
func BallotInputsHash(
	processID types.HexBytes,
	ballotMode *types.BallotMode,
	encryptionKey ecc.Point,
	address types.HexBytes,
	voteID *types.BigInt,
	ballot *elgamal.Ballot,
	weight *types.BigInt,
) (*types.BigInt, error) {
	// check if unconverted parameters are in the field
	if !voteID.IsInField(circuits.BallotProofCurve.ScalarField()) {
		return nil, fmt.Errorf("voteID is not in the scalar field")
	}
	if !weight.IsInField(circuits.BallotProofCurve.ScalarField()) {
		return nil, fmt.Errorf("weight is not in the scalar field")
	}
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
		encryptionKeyXTE,       // encryption key x coordinate
		encryptionKeyYTE,       // encryption key y coordinate
		ffAddress.MathBigInt(), // address
		voteID.MathBigInt(),    // vote ID
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
