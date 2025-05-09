package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/format"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func WasmVoteInputs(
	inputs *BallotProofWasmInputs,
) (*BallotProofWasmResult, error) {
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
		return nil, fmt.Errorf("Error encrypting ballot: %v", err.Error())
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
		return nil, fmt.Errorf("Error calculating commitment and nullifier: %v", err.Error())
	}
	// ballot mode as circuit ballot mode
	ballotMode := circuits.BallotModeToCircuit(inputs.BallotMode)
	// safe address and processID
	ffAddress := inputs.Address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	ffProcessID := inputs.ProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	// compose a list with the inputs of the circuit to hash them
	inputsHash := []*big.Int{ffProcessID.MathBigInt()}         // process id
	inputsHash = append(inputsHash, ballotMode.Serialize()...) // ballot mode serialized
	inputsHash = append(inputsHash,
		circomEncryptionKeyX,    // encryption key x coordinate
		circomEncryptionKeyY,    // encryption key y coordinate
		ffAddress.MathBigInt(),  // address
		commitment.MathBigInt(), // commitment
		nullifier.MathBigInt(),  // nullifier
	)
	// ballot (in twisted edwards form)
	inputsHash = append(inputsHash, ballot.FromRTEtoTE().BigInts()...)
	// weight
	inputsHash = append(inputsHash, inputs.Weight.MathBigInt())
	// hash the inputs with mimc7
	circomInputHash, err := mimc7.Hash(inputsHash, nil)
	if err != nil {
		return nil, fmt.Errorf("Error hashing inputs: %v", err.Error())
	}
	return &BallotProofWasmResult{
		ProccessID:       inputs.ProcessID,
		Address:          inputs.Address,
		Commitment:       commitment,
		Nullifier:        nullifier,
		Ballot:           ballot.FromRTEtoTE(),
		BallotInputsHash: crypto.BigIntToFFwithPadding(circomInputHash, circuits.VoteVerifierCurve.ScalarField()),
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
			InputsHash:      circomInputHash.String(),
		},
	}, nil
}
