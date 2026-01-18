package ballotproof

import _ "embed"

var (
	// CircomCircuitWasm is the ballot proof Circom circuit compiled to WASM.
	//go:embed circom_assets/ballot_proof.wasm
	CircomCircuitWasm []byte

	// CircomProvingKey is the Groth16 proving key for the ballot proof circuit.
	//go:embed circom_assets/ballot_proof_pkey.zkey
	CircomProvingKey []byte

	// CircomVerificationKey is the verification key for the ballot proof circuit.
	//go:embed circom_assets/ballot_proof_vkey.json
	CircomVerificationKey []byte
)
