package ballotproof

import (
	"fmt"

	"github.com/iden3/go-rapidsnark/prover"
	"github.com/iden3/go-rapidsnark/witness"
)

// GenerateProof compiles the provided circom wasm circuit, generates the
// witness for the inputs provided and generates the proof using the provided
// proving key. It returns the proof and the public signals of the proof. It
// uses Rapidsnark and Groth16 prover to generate the proof.
func GenerateProof(wasm, pk, inputs []byte) (string, string, error) {
	finalInputs, err := witness.ParseInputs(inputs)
	if err != nil {
		return "", "", fmt.Errorf("circom inputs: %w", err)
	}
	// instance witness calculator
	calc, err := witness.NewCircom2WitnessCalculator(wasm, true)
	if err != nil {
		return "", "", fmt.Errorf("instance witness calculator: %w", err)
	}
	// calculate witness
	w, err := calc.CalculateWTNSBin(finalInputs, true)
	if err != nil {
		return "", "", fmt.Errorf("calculate witness: %w", err)
	}
	// generate proof
	return prover.Groth16ProverRaw(pk, w)
}

// GenerateProofWithDefaults compiles the CircomCircuitWasm circom circuit,
// generates the witness and generates the proof using the inputs provided
// and the CircomProvingKey. It returns the proof and the public signals of
// the proof. It uses Rapidsnark and Groth16 prover to generate the proof.
func GenerateProofWithDefaults(inputs []byte) (string, string, error) {
	return GenerateProof(CircomCircuitWasm, CircomProvingKey, inputs)
}
