package ballotproof

import circomartifacts "github.com/vocdoni/davinci-circom/artifacts"

var (
	// CircomCircuitWasm contains the ballot proof Circom circuit compiled to WASM.
	CircomCircuitWasm = circomartifacts.BallotProofWasm

	// CircomProvingKey contains the Groth16 proving key for the ballot proof circuit.
	CircomProvingKey = circomartifacts.BallotProofProvingKey

	// CircomVerificationKey contains the Groth16 verification key for the ballot proof circuit.
	CircomVerificationKey = circomartifacts.BallotProofVerificationKey
)
