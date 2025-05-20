package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/format"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// GenerateBallotProofInputs composes the data to generate the inputs required
// to generate the witness for a ballot proof using the circom circuit and also
// the data required to cast a vote sending it to the sequencer API. It receives
// the BallotProofWasmInputs struct and returns the BallotProofWasmResult
// struct. This method parses the public encryption key for the desired process
// and encrypts the ballot fields with the secret K provided. It also generates
// the commitment and nullifier for the vote, using the address, process ID
// and the secret provided.
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
	// compose the encryption key with the coords from the inputs
	encryptionKey := new(bjj.BJJ).SetPoint(inputs.EncryptionKey[0].MathBigInt(), inputs.EncryptionKey[1].MathBigInt())
	// encrypt the ballot
	ballot, err := elgamal.NewBallot(encryptionKey).Encrypt(fields, encryptionKey, inputs.K.MathBigInt())
	if err != nil {
		return nil, fmt.Errorf("error encrypting ballot: %v", err.Error())
	}
	// get encryption key point for circom
	circomEncryptionKeyX, circomEncryptionKeyY := format.FromRTEtoTE(encryptionKey.Point())
	// calculate the commitment and nullifier
	commitment, nullifier, err := CommitmentAndNullifier(
		inputs.Address.BigInt(),
		inputs.ProcessID.BigInt(),
		inputs.Secret.BigInt(),
	)
	if err != nil {
		return nil, fmt.Errorf("error calculating commitment and nullifier: %v", err.Error())
	}
	// ballot mode as circuit ballot mode
	ballotMode := circuits.BallotModeToCircuit(inputs.BallotMode)
	// safe address and processID
	ffAddress := inputs.Address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	ffProcessID := inputs.ProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	// calculate the ballot inputs hash
	ballotInputsHash, err := BallotInputsHash(
		inputs.ProcessID,
		inputs.BallotMode,
		encryptionKey,
		inputs.Address,
		commitment,
		nullifier,
		ballot,
		inputs.Weight,
	)
	if err != nil {
		return nil, fmt.Errorf("error calculating ballot input hash: %v", err.Error())
	}
	return &BallotProofInputsResult{
		ProccessID:       inputs.ProcessID,
		Address:          inputs.Address,
		Commitment:       commitment,
		Nullifier:        nullifier,
		Ballot:           ballot.FromRTEtoTE(),
		BallotInputsHash: (*types.BigInt)(ballotInputsHash),
		VoteID:           crypto.BigIntToFFwithPadding(ballotInputsHash.MathBigInt(), circuits.VoteVerifierCurve.ScalarField()),
		CircomInputs: &CircomInputs{
			Fields:          circuits.BigIntArrayToStringArray(fields[:], types.FieldsPerBallot),
			MaxCount:        ballotMode.MaxCount.String(),
			ForceUniqueness: ballotMode.ForceUniqueness.String(),
			MaxValue:        ballotMode.MaxValue.String(),
			MinValue:        ballotMode.MinValue.String(),
			MaxTotalCost:    ballotMode.MaxTotalCost.String(),
			MinTotalCost:    ballotMode.MinTotalCost.String(),
			CostExp:         ballotMode.CostExp.String(),
			CostFromWeight:  ballotMode.CostFromWeight.String(),
			Address:         ffAddress.String(),
			Weight:          inputs.Weight.MathBigInt().String(),
			ProcessID:       ffProcessID.String(),
			PK:              []string{circomEncryptionKeyX.String(), circomEncryptionKeyY.String()},
			K:               inputs.K.MathBigInt().String(),
			Cipherfields:    circuits.BigIntArrayToStringArray(ballot.FromRTEtoTE().BigInts(), types.FieldsPerBallot*elgamal.BigIntsPerCiphertext),
			Nullifier:       nullifier.String(),
			Commitment:      commitment.String(),
			Secret:          inputs.Secret.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).String(),
			InputsHash:      ballotInputsHash.String(),
		},
	}, nil
}
