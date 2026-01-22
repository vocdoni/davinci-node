package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/spec/params"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/types"
)

// BallotProofInputs struct contains the required inputs to compose the
// data to generate the witness for a ballot proof using the circom circuit.
type BallotProofInputs struct {
	ProcessID     types.ProcessID `json:"processId"`
	Address       types.HexBytes  `json:"address"`
	EncryptionKey []*types.BigInt `json:"encryptionKey"`
	K             *types.BigInt   `json:"k"`
	BallotMode    spec.BallotMode `json:"ballotMode"`
	Weight        *types.BigInt   `json:"weight"`
	FieldValues   []*types.BigInt `json:"fieldValues"`
}

// VoteID generates a unique identifier for the vote based on the process ID,
// address and k value. This ID is used to sign the vote and prove ownership.
// It returns the vote ID as a HexBytes type or an error if the inputs are
// invalid or something goes wrong during the generation of the ID. It calls
// the VoteID function with the process ID, address and k value converted to
// the appropriate types.
func (b *BallotProofInputs) VoteID() (types.VoteID, error) {
	if b == nil {
		return 0, fmt.Errorf("ballot proof inputs cannot be nil")
	}
	if !b.ProcessID.IsValid() || len(b.Address) == 0 || b.K == nil {
		return 0, fmt.Errorf("a valid processID, address and k is required")
	}
	voteID, err := spec.VoteID(
		b.ProcessID.MathBigInt(),
		b.Address.BigInt().MathBigInt(),
		b.K.MathBigInt(),
	)
	if err != nil {
		return 0, err
	}
	return types.VoteID(voteID), nil
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
	ballotMode spec.BallotMode,
	encryptionKey ecc.Point,
	address types.HexBytes,
	voteID types.VoteID,
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
	// compose a list with the inputs of the circuit to hash them
	ballotInputHash, err := spec.BallotInputsHashRTE(
		ffProcessID.MathBigInt(),
		ballotMode,
		encryptionKeyXTE,
		encryptionKeyYTE,
		ffAddress.MathBigInt(),
		voteID.Uint64(),
		ballot.FromRTEtoTE().BigInts(),
		weight.MathBigInt(),
	)
	if err != nil {
		return nil, err
	}
	return (*types.BigInt)(ballotInputHash), nil
}

func BallotInputsHashIden3(
	processID types.ProcessID,
	ballotMode spec.BallotMode,
	encryptionKey ecc.Point,
	address types.HexBytes,
	voteID types.VoteID,
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

	// convert the encryption key to twisted edwards form
	encryptionKeyXTE, encryptionKeyYTE := format.FromRTEtoTE(encryptionKey.Point())

	ballotInputHash, err := spec.BallotInputsHashRTE(
		ffProcessID.MathBigInt(),
		ballotMode,
		encryptionKeyXTE,
		encryptionKeyYTE,
		ffAddress.MathBigInt(),
		voteID.Uint64(),
		ballot.BigInts(),
		weight.MathBigInt(),
	)
	if err != nil {
		return nil, err
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
		k, err := specutil.RandomK()
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
	ballotMode := inputs.BallotMode
	// calculate the vote ID
	voteID, err := inputs.VoteID()
	if err != nil {
		return nil, fmt.Errorf("error generating vote ID: %w", err)
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
	packedBallot, err := ballotMode.Pack()
	if err != nil {
		return nil, fmt.Errorf("error packing ballot mode: %w", err)
	}

	return &BallotProofInputsResult{
		ProcessID:        inputs.ProcessID,
		Address:          inputs.Address,
		Weight:           inputs.Weight,
		Ballot:           ballot.FromRTEtoTE(),
		BallotInputsHash: ballotInputsHash,
		VoteID:           voteID,
		CircomInputs: &CircomInputs{
			Fields:        circuits.BigIntArrayToNInternal(fields[:], params.FieldsPerBallot),
			BallotMode:    new(types.BigInt).SetBigInt(packedBallot),
			Address:       inputs.Address.BigInt().ToFF(params.BallotProofCurve.ScalarField()),
			Weight:        inputs.Weight,
			ProcessID:     inputs.ProcessID.BigInt().ToFF(params.BallotProofCurve.ScalarField()),
			VoteID:        new(types.BigInt).SetBigInt(voteID.BigInt()),
			EncryptionKey: types.SliceOf([]*big.Int{circomEncryptionKeyX, circomEncryptionKeyY}, types.BigIntConverter),
			K:             inputs.K,
			Cipherfields:  circuits.BigIntArrayToNInternal(ballot.FromRTEtoTE().BigInts(), params.FieldsPerBallot*elgamal.BigIntsPerCiphertext),
			InputsHash:    ballotInputsHash,
		},
	}, nil
}
