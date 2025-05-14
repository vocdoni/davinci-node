// Package config provides configuration for circuit artifacts including URLs and hashes
// for various circuit components used in the Vocdoni system.
package config

import "fmt"

const (
	// DefaultArtifactsBaseURL is the base URL for circuit artifacts storage
	DefaultArtifactsBaseURL = "https://circuits.ams3.cdn.digitaloceanspaces.com"
	// DefaultArtifactsRelease is the release version for circuit artifacts
	DefaultArtifactsRelease = "dev"
)

var (
	// BallotProoCircuitURL is the URL for the ballot proof circuit WASM file
	BallotProoCircuitURL = fmt.Sprintf("%s/%s/ballot_proof.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofCircuitHash is the hash of the ballot proof circuit
	BallotProofCircuitHash = "5a6f7d40c1e74c238cc282c4bcc22a0a623b6fa8426c01cd7e8ef45e34394faf"

	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/ballot_proof_pkey.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "f4bc379bb933946a558bdbe504e93037c8049fbb809fb515e452f0f370e27cef"

	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/ballot_proof_vkey.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "833c8f97ed01858e083f3c8b04965f168400a2cc205554876e49d32b14ddebe8"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "2395f99ec9afa47b343c7ed5654831acb77508cba730be2b43690a750f47b3b2"

	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "8c9d2633352d193a00a2044ca80b4962217d0ef35af49dcd12c080866ee62303"

	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "5289f44cc945d9153cb9a02f83e470a7d51450ac8da076b4aaca2285191f4f22"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "9241102c18c234194bf0587e31d93bd2dfdead2a3ec9d42bdec71b4b5fc001d9"

	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "7c9052082c546f146f379ecd35d1cf6ddc3de93ffc39db024cebce70762d51fa"

	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "d1d5e08a6ee16c053a13df18e2901b7fbcfd1b13da4c9d4cabc4f70d99aa95e0"

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionCircuitHash is the hash of the statetransition circuit
	StateTransitionCircuitHash = "238b9303d43ce67cc3db71f650991c464b595b5ec401e892639d7fef553ec824"

	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionProvingKeyHash is the hash of the statetransition proving key
	StateTransitionProvingKeyHash = "c1b5d89c50f917da673ee74c17c87636309a98f88537db6b66c672353cfcafad"

	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)
	// StateTransitionVerificationKeyHash is the hash of the statetransition verification key
	StateTransitionVerificationKeyHash = "a5ff38396f7f145c33ee6bd549952ea975ec4e8d8d1583945028d2eb9805ada9"

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = "https://github.com/vocdoni/davinci-node/raw/refs/heads/main/cmd/ballotproof-wasm/ballotproof.wasm"
	// BallotProofWasmHelperHash is the hash of the WASM helper
	BallotProofWasmHelperHash = "78e66e787ca075445da0009ff203cfb9acf18f759c787cbf2e3eade99e72fd61"

	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = "https://github.com/vocdoni/davinci-node/raw/refs/heads/main/cmd/ballotproof-wasm/wasm_exec.js"
	// BallotProofWasmExecJsHash is the hash of the WASM exec JS
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)
