package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/circuits"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

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
	fields := [types.FieldsPerBallot]*big.Int{}
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
	ballotInputsHash, err := BallotInputsHash(
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
		Ballot:           ballot.FromRTEtoTE(),
		BallotInputsHash: ballotInputsHash,
		VoteID:           voteIDForSign,
		CircomInputs: &CircomInputs{
			Fields:         circuits.BigIntArrayToNInternal(fields[:], types.FieldsPerBallot),
			NumFields:      new(types.BigInt).SetBigInt(ballotMode.NumFields),
			UniqueValues:   new(types.BigInt).SetBigInt(ballotMode.UniqueValues),
			MaxValue:       new(types.BigInt).SetBigInt(ballotMode.MaxValue),
			MinValue:       new(types.BigInt).SetBigInt(ballotMode.MinValue),
			MaxValueSum:    new(types.BigInt).SetBigInt(ballotMode.MaxValueSum),
			MinValueSum:    new(types.BigInt).SetBigInt(ballotMode.MinValueSum),
			CostExponent:   new(types.BigInt).SetBigInt(ballotMode.CostExponent),
			CostFromWeight: new(types.BigInt).SetBigInt(ballotMode.CostFromWeight),
			Address:        inputs.Address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()),
			Weight:         inputs.Weight,
			ProcessID:      inputs.ProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()),
			VoteID:         voteID,
			EncryptionKey:  types.SliceOf([]*big.Int{circomEncryptionKeyX, circomEncryptionKeyY}, types.BigIntConverter),
			K:              inputs.K,
			Cipherfields:   circuits.BigIntArrayToNInternal(ballot.FromRTEtoTE().BigInts(), types.FieldsPerBallot*elgamal.BigIntsPerCiphertext),
			InputsHash:     ballotInputsHash,
		},
	}, nil
}
