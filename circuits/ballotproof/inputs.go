package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

// BallotProofInputs struct contains the required inputs to compose the
// data to generate the witness for a ballot proof using the circom circuit.
type BallotProofInputs struct {
	ProcessID     types.ProcessID   `json:"processId"`
	Address       types.HexBytes    `json:"address"`
	EncryptionKey []*types.BigInt   `json:"encryptionKey"`
	K             *types.BigInt     `json:"k"`
	BallotMode    *types.BallotMode `json:"ballotMode"`
	Weight        *types.BigInt     `json:"weight"`
	FieldValues   []*types.BigInt   `json:"fieldValues"`
}

// VoteID generates a unique identifier for the vote based on the process ID,
// address and k value. This ID is used to sign the vote and prove ownership.
// It returns the vote ID as a HexBytes type or an error if the inputs are
// invalid or something goes wrong during the generation of the ID. It calls
// the VoteID function with the process ID, address and k value converted to
// the appropriate types.
func (b *BallotProofInputs) VoteID() (*types.BigInt, error) {
	if b == nil {
		return nil, fmt.Errorf("ballot proof inputs cannot be nil")
	}
	return circuits.VoteID(b.ProcessID, common.BytesToAddress(b.Address), b.K)
}

// VoteIDForSign returns the vote ID in a format suitable for signing and
// verify the signature inside the circuit. It pads the vote ID to ensure it
// is of the correct length for signing.
func (b *BallotProofInputs) VoteIDForSign() (types.HexBytes, error) {
	voteID, err := b.VoteID()
	if err != nil {
		return nil, fmt.Errorf("error generating vote ID: %v", err.Error())
	}
	// return crypto.BigIntToFFToSign(voteID.MathBigInt(), params.VoteVerifierCurve.ScalarField()), nil
	return crypto.PadToSign(voteID.Bytes()), err
}

// BallotInputsHash helper function calculates the hash of the public inputs
// of the ballot proof circuit. This hash is used to verify the proof generated
// by the user and is also used to generate the voteID. The hash is calculated
// using the poseidon hash function and includes the process ID, ballot mode,
// encryption key, address, ballot and weight, in that particular order. The
// function transforms the inputs to the correct format:
//   - processID and address are converted to FF
//   - encryption key is converted to twisted edwards form
//   - ballot mode is converted to circuit ballot mode
//   - ballot is converted to twisted edwards form
func BallotInputsHashGnark(
	processID types.ProcessID,
	ballotMode *types.BallotMode,
	encryptionKey ecc.Point,
	address types.HexBytes,
	voteID *types.BigInt,
	ballot *elgamal.Ballot,
	weight *types.BigInt,
) (*types.BigInt, error) {
	// check if unconverted parameters are in the field
	if !voteID.IsInField(params.BallotProofCurve.ScalarField()) {
		return nil, fmt.Errorf("voteID is not in the scalar field")
	}
	if !weight.IsInField(params.BallotProofCurve.ScalarField()) {
		return nil, fmt.Errorf("weight is not in the scalar field")
	}
	// safe address and processID
	ffAddress := address.BigInt().ToFF(params.BallotProofCurve.ScalarField())
	ffProcessID := processID.BigInt().ToFF(params.BallotProofCurve.ScalarField())
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
	// hash the inputs with poseidon
	ballotInputHash, err := poseidon.MultiPoseidon(inputsHash...)
	if err != nil {
		return nil, fmt.Errorf("error hashing inputs: %v", err.Error())
	}
	return (*types.BigInt)(ballotInputHash), nil
}

func BallotInputsHashIden3(
	processID types.ProcessID,
	ballotMode *types.BallotMode,
	encryptionKey ecc.Point,
	address types.HexBytes,
	voteID *types.BigInt,
	ballot *elgamal.Ballot, // Expected to be in RTE format (snarkjs and circom output)
	weight *types.BigInt,
) (*types.BigInt, error) {
	// check if unconverted parameters are in the field
	if !voteID.IsInField(params.BallotProofCurve.ScalarField()) {
		return nil, fmt.Errorf("voteID is not in the scalar field")
	}
	if !weight.IsInField(params.BallotProofCurve.ScalarField()) {
		return nil, fmt.Errorf("weight is not in the scalar field")
	}
	// safe address and processID
	ffAddress := address.BigInt().ToFF(params.BallotProofCurve.ScalarField())
	ffProcessID := processID.BigInt().ToFF(params.BallotProofCurve.ScalarField())

	// ballot mode as circuit ballot mode
	circuitBallotMode := circuits.BallotModeToCircuit(ballotMode)

	// convert the encryption key to twisted edwards form
	encryptionKeyXTE, encryptionKeyYTE := format.FromRTEtoTE(encryptionKey.Point())

	// compose a list with the inputs of the circuit to hash them
	inputsHash := []*big.Int{ffProcessID.MathBigInt()}                // process id
	inputsHash = append(inputsHash, circuitBallotMode.Serialize()...) // ballot mode serialized
	inputsHash = append(inputsHash,
		encryptionKeyXTE,       // encryption key x (TE - no conversion)
		encryptionKeyYTE,       // encryption key y (TE - no conversion)
		ffAddress.MathBigInt(), // address
		voteID.MathBigInt(),    // vote ID
	)
	// ballot (from reduced twisted edwards to twisted edwards form)
	inputsHash = append(inputsHash, ballot.BigInts()...)

	// weight
	inputsHash = append(inputsHash, weight.MathBigInt())

	// hash the inputs with poseidon
	ballotInputHash, err := poseidon.MultiPoseidon(inputsHash...)
	if err != nil {
		return nil, fmt.Errorf("error hashing inputs: %v", err.Error())
	}
	return (*types.BigInt)(ballotInputHash), nil
}

// GenerateBallotProofInputs composes the data to generate the inputs required
// to generate the witness for a ballot proof using the circom circuit and also
// the data required to cast a vote sending it to the sequencer API. It receives
// the BallotProofWasmInputs struct and returns the BallotProofWasmResult
// struct. This method parses the public encryption key for the desired process
// and encrypts the ballot fields with the secret K provided.
func GenerateBallotProofInputs(
	inputs *BallotProofInputs,
) (*BallotProofInputsResult, error) {
	// pad the field values to the number of circuits.FieldsPerBallot
	fields := [params.FieldsPerBallot]*big.Int{}
	for i := range fields {
		if i < len(inputs.FieldValues) {
			fields[i] = inputs.FieldValues[i].MathBigInt()
		} else {
			fields[i] = big.NewInt(0)
		}
	}
	// if no k is provided, generate a random one
	if inputs.K == nil {
		k, err := elgamal.RandK()
		if err != nil {
			return nil, fmt.Errorf("error generating random k: %w", err)
		}
		inputs.K = new(types.BigInt).SetBigInt(k)
	}
	// compose the encryption key with the coords from the inputs
	encryptionKey := new(bjj.BJJ).SetPoint(inputs.EncryptionKey[0].MathBigInt(), inputs.EncryptionKey[1].MathBigInt())
	// encrypt the ballot
	ballot, err := elgamal.NewBallot(encryptionKey).Encrypt(fields, encryptionKey, inputs.K.MathBigInt())
	if err != nil {
		return nil, fmt.Errorf("error encrypting ballot: %w", err)
	}
	// get encryption key point for circom
	circomEncryptionKeyX, circomEncryptionKeyY := format.FromRTEtoTE(encryptionKey.Point())
	// ballot mode as circuit ballot mode
	ballotMode := circuits.BallotModeToCircuit(inputs.BallotMode)
	// calculate the vote ID
	voteID, err := inputs.VoteID()
	if err != nil {
		return nil, fmt.Errorf("error generating vote ID: %w", err)
	}
	voteIDForSign, err := inputs.VoteIDForSign()
	if err != nil {
		return nil, fmt.Errorf("error generating vote ID for sign: %w", err)
	}
	// calculate the ballot inputs hash
	ballotInputsHash, err := BallotInputsHashGnark(
		inputs.ProcessID,
		inputs.BallotMode,
		encryptionKey,
		inputs.Address,
		voteID,
		ballot,
		inputs.Weight,
	)
	if err != nil {
		return nil, fmt.Errorf("error calculating ballot input hash: %w", err)
	}
	return &BallotProofInputsResult{
		ProcessID:        inputs.ProcessID,
		Address:          inputs.Address,
		Weight:           inputs.Weight,
		Ballot:           ballot.FromRTEtoTE(),
		BallotInputsHash: ballotInputsHash,
		VoteID:           voteIDForSign,
		CircomInputs: &CircomInputs{
			Fields:         circuits.BigIntArrayToNInternal(fields[:], params.FieldsPerBallot),
			NumFields:      new(types.BigInt).SetBigInt(ballotMode.NumFields),
			UniqueValues:   new(types.BigInt).SetBigInt(ballotMode.UniqueValues),
			MaxValue:       new(types.BigInt).SetBigInt(ballotMode.MaxValue),
			MinValue:       new(types.BigInt).SetBigInt(ballotMode.MinValue),
			MaxValueSum:    new(types.BigInt).SetBigInt(ballotMode.MaxValueSum),
			MinValueSum:    new(types.BigInt).SetBigInt(ballotMode.MinValueSum),
			CostExponent:   new(types.BigInt).SetBigInt(ballotMode.CostExponent),
			CostFromWeight: new(types.BigInt).SetBigInt(ballotMode.CostFromWeight),
			Address:        inputs.Address.BigInt().ToFF(params.BallotProofCurve.ScalarField()),
			Weight:         inputs.Weight,
			ProcessID:      inputs.ProcessID.BigInt().ToFF(params.BallotProofCurve.ScalarField()),
			VoteID:         voteID,
			EncryptionKey:  types.SliceOf([]*big.Int{circomEncryptionKeyX, circomEncryptionKeyY}, types.BigIntConverter),
			K:              inputs.K,
			Cipherfields:   circuits.BigIntArrayToNInternal(ballot.FromRTEtoTE().BigInts(), params.FieldsPerBallot*elgamal.BigIntsPerCiphertext),
			InputsHash:     ballotInputsHash,
		},
	}, nil
}
