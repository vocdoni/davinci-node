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

// Hashes of each circuit artifacts
const (
	BallotProofCircuitHash         = "e287dbc72dbdb4a938db90b61637a0249b5d4f8c294fbcf55e4f08881883112a"
	BallotProofProvingKeyHash      = "982f1a850bc12325001e0c91d5238ae69b6b7ea20b3d5c1c18319d70a5dd2d65"
	BallotProofVerificationKeyHash = "1675949c5628cfd347e93cfe5187805d28b5e8cb58591085b58eaa1953371991"

	VoteVerifierCircuitHash         = "746c6b7acab2dbcf7db5495f127cb2deb52148919169d3cf27c36651ab86a730"
	VoteVerifierProvingKeyHash      = "8d752493649ecfe094984f94dde60fd08c6869d126c9918392664fbde54fa9dd"
	VoteVerifierVerificationKeyHash = "157a56bc67c168be33f01c7d69279bb5831e08d30f5b35f5e935a9404c2b7e84"

	AggregatorCircuitHash         = "d97433a7ce2409fc830db2f3e0543896f08bae81f0f7d08b69722860a58bd0b2"
	AggregatorProvingKeyHash      = "1815a66c8c68759500a36583f75a83dd3623b5d722f07398aa33aef82a2f400a"
	AggregatorVerificationKeyHash = "6d06e5b19528cae7bf42427b471c0415207679306154d75a2338481d4b6e62a4"

	StateTransitionCircuitHash         = "f2656063f719878519a12f673a9bb9fc73a5c1057976f186687cbd50806ec7ce"
	StateTransitionProvingKeyHash      = "ba6e46003838f8d4f21e45a0beb72501331b60ebdb6cca104eb75e574d469ad7"
	StateTransitionVerificationKeyHash = "d3b8279eb2d64678dbe8d1e41f5b7a9816424433cdbe6b5df5b0e06ff4942c80"

	ResultsVerifierCircuitHash         = "9e1ca92dde94e7308db6f8aab8e1d866c19cc83a52bbe5604306b9cff263c7ad"
	ResultsVerifierProvingKeyHash      = "541153047c2ce114eb9e5951f869edea62e21e01ffcf6ba427e4e210a594af48"
	ResultsVerifierVerificationKeyHash = "33c26c3851e9e3fb63f6e3aa09e4a1b55a167063cfb4f2bf841d78c5792fe5d2"

	BallotProofWasmHelperHash = "9ea8f6989eab0c8e848902ee4289d6b6acb076056f0391e483befbe8348bc6c8"
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)

var (
	// BallotProofCircuitURL is the URL for the ballot proof circuit WASM file
	BallotProofCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofCircuitHash)
	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofProvingKeyHash)
	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofVerificationKeyHash)

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)

	// ResultsVerifierCircuitURL is the URL for the statetransition circuit
	ResultsVerifierCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierCircuitHash)
	// ResultsVerifierProvingKeyURL is the URL for the resultsverifier proving key
	ResultsVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierProvingKeyHash)
	// ResultsVerifierVerificationKeyURL is the URL for the resultsverifier verification key
	ResultsVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierVerificationKeyHash)

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = fmt.Sprintf("%s/%s/davinci_crypto_%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmHelperHash[len(BallotProofWasmHelperHash)-4:])
	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = fmt.Sprintf("%s/%s/wasm_exec_%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmExecJsHash[len(BallotProofWasmExecJsHash)-4:])
)
