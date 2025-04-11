//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"syscall/js"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/format"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

const (
	jsClassName         = "BallotProofWasm"
	jsBallotProofInputs = "proofInputs"
	nArgs               = 1 // hex string with BallotProofWasmInputs bytes
)

func main() {
	ballotProofClass := js.ValueOf(map[string]any{})
	// register cipherBallotFields function
	ballotProofClass.Set(jsBallotProofInputs, js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) != nArgs {
			return JSResult(nil, fmt.Errorf("Invalid number of arguments"))
		}
		inputs := new(BallotProofWasmInputs)
		if err := inputs.Unmarshal(args[0]); err != nil {
			return JSResult(nil, fmt.Errorf("Invalid inputs: %v", err))
		}
		// pad the field values to the number of circuits.FieldsPerBallot
		var fields = [circuits.FieldsPerBallot]*big.Int{}
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
			return JSResult(nil, fmt.Errorf("Error encrypting ballot: %v", err.Error()))
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
			return JSResult(nil, fmt.Errorf("Error calculating commitment and nullifier: %v", err.Error()))
		}
		// compose a list with the inputs of the circuit to hash them
		inputsHash := []*big.Int{
			// processID
			inputs.ProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt(),
		}
		// ballot mode as a big int list
		circuitBallotMode := circuits.BallotModeToCircuit(*inputs.BallotMode)
		inputsHash = append(inputsHash, circuits.BallotModeToCircuit(*inputs.BallotMode).Serialize()...)
		inputsHash = append(inputsHash,
			// encryption key
			circomEncryptionKeyX,
			circomEncryptionKeyY,
			// address
			inputs.Address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).MathBigInt(),
			// commitment
			commitment.MathBigInt(),
			// nullifier
			nullifier.MathBigInt())
		// ballot (in twisted edwards form)
		inputsHash = append(inputsHash, ballot.FromRTEtoTE().BigInts()...)
		// weight
		inputsHash = append(inputsHash, inputs.Weight.MathBigInt())
		// hash the inputs with mimc7
		circomInputHash, err := mimc7.Hash(inputsHash, nil)
		if err != nil {
			return JSResult(nil, fmt.Errorf("Error hashing inputs: %v", err.Error()))
		}
		circomInputs := &CircomInputs{
			Fields:          circuits.BigIntArrayToStringArray(fields[:], circuits.FieldsPerBallot),
			MaxCount:        circuitBallotMode.MaxCount.String(),
			ForceUniqueness: circuitBallotMode.ForceUniqueness.String(),
			MaxValue:        circuitBallotMode.MaxValue.String(),
			MinValue:        circuitBallotMode.MinValue.String(),
			MaxTotalCost:    circuitBallotMode.MaxTotalCost.String(),
			MinTotalCost:    circuitBallotMode.MinTotalCost.String(),
			CostExp:         circuitBallotMode.CostExp.String(),
			CostFromWeight:  circuitBallotMode.CostFromWeight.String(),
			Address:         inputs.Address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).String(),
			Weight:          inputs.Weight.MathBigInt().String(),
			ProcessID:       inputs.ProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).String(),
			PK:              []string{circomEncryptionKeyX.String(), circomEncryptionKeyY.String()},
			K:               inputs.K.MathBigInt().String(),
			CipherFields:    circuits.BigIntArrayToStringArray(ballot.FromRTEtoTE().BigInts(), circuits.FieldsPerBallot*elgamal.BigIntsPerCiphertext),
			Nullifier:       nullifier.String(),
			Commitment:      commitment.String(),
			Secret:          inputs.Secret.BigInt().ToFF(circuits.BallotProofCurve.ScalarField()).String(),
			InputsHash:      circomInputHash.String(),
		}
		// encode result to json to return it
		bRes, err := json.Marshal(&BallotProofWasmResult{
			CircuitInputs: circomInputs,
			SignatureHash: crypto.BigIntToFFwithPadding(circomInputHash, circuits.VoteVerifierCurve.ScalarField()),
		})
		return JSResult(string(bRes))
	}))

	// register class
	js.Global().Set(jsClassName, ballotProofClass)
	// keep the main function alive
	select {}
}

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
