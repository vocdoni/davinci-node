package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
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

// VoteID calculates the vote ID which is the mimc7 hash of the process ID,
// voter's address and a secret value k truncated to the least significant
// 160 bits. The vote ID is used to identify a vote in the system. The
// function transforms the inputs to safe values of ballot proof curve scalar
// field, then hashes them using mimc7. The resulting vote ID is a hex byte
// array. If something goes wrong during the hashing process, it returns an
// error.
func VoteID(processID types.ProcessID, address common.Address, k *types.BigInt) (*types.BigInt, error) {
	// encode the process ID and address to hex bytes
	hexAddress := types.HexBytes(address.Bytes())
	hexProcessID := types.HexBytes(processID.Marshal())
	// safe address, processID and k
	ffAddress := hexAddress.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	ffProcessID := hexProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	ffK := k.ToFF(circuits.BallotProofCurve.ScalarField())
	// calculate the vote ID hash using mimc7
	hash, err := mimc7.Hash([]*big.Int{
		ffProcessID.MathBigInt(), // process id
		ffAddress.MathBigInt(),   // address
		ffK.MathBigInt(),         // k
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error hashing vote ID inputs: %v", err.Error())
	}
	return new(types.BigInt).SetBigInt(truncateToLSB160Bits(hash)), nil
}

func truncateToLSB160Bits(input *big.Int) *big.Int {
	mask := new(big.Int).Lsh(big.NewInt(1), 160) // 1 << 160
	mask.Sub(mask, big.NewInt(1))                // (1 << 160) - 1
	return new(big.Int).And(input, mask)         // input & ((1<<160)-1)
}
