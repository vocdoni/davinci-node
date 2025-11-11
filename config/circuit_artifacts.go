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
	// BallotProofCircuitURL is the URL for the ballot proof circuit WASM file
	BallotProofCircuitURL = fmt.Sprintf("%s/%s/%s.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofCircuitHash)
	// BallotProofCircuitHash is the hash of the ballot proof circuit
	BallotProofCircuitHash = "bf6577f5cdbde35312f512b028b0a63e89414ef637e4a83e9659530c737b5d7c"
	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/%s.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofProvingKeyHash)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "e4e64a474dde6d5a14e4ab46c54ec87eee5b786a5e90d59a160a17ede6c39f92"
	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/%s.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofVerificationKeyHash)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "7633ab73ac0dbe7909ab524c8ad7b83cd90f0031e5ba117635475063ef4fba21"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "b15ac7daab9f03e8bd2fba3d113c17655afd4aa73c40efe67ba17d2e32b05952"
	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "1da77937fa05482fda4ddab59002f2ee743ec7d2c8fca1116fdeb08771d6cbe9"
	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "56859cbaa1e00a59ca6e39f50751288d4cca236eb833225c9d403f90520b7d3e"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "4a9f2981eded5222527e472dfecdf7c2eb29dd3402518e1e4ce844b72cc2a05e"
	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "74e623a9f6b11a127b94cb992f0f1c5c3ea124c5acb912f2e0a0fc3efaf230c0"
	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "0a698b87de7104751cc88e37f7e2376f381bb38c912c7f78ef71ab1019f21c10"

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionCircuitHash is the hash of the statetransition circuit
	StateTransitionCircuitHash = "d1386184a1f6a3ed62a777a3a4a600af0249805af6d16a754417568bb99d0b65"
	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionProvingKeyHash is the hash of the statetransition proving key
	StateTransitionProvingKeyHash = "7fa5475263018a09b457daf45caaecf30b2545305db604f494c45f900a54c811"
	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)
	// StateTransitionVerificationKeyHash is the hash of the statetransition verification key
	StateTransitionVerificationKeyHash = "ecada4cf9a469865cff6885ebb6b07a53f13f76d6724839809664f5541ffb993"

	// ResultsVerifierCircuitURL is the URL for the statetransition circuit
	ResultsVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierCircuitHash)
	// ResultsVerifierCircuitHash is the hash of the statetransition circuit
	ResultsVerifierCircuitHash = "af53053fb22e8e4825d48b9aec37819525aec35801e900b46d3dd8e161c64676"
	// ResultsVerifierProvingKeyURL is the URL for the resultsverifier proving key
	ResultsVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierProvingKeyHash)
	// ResultsVerifierProvingKeyHash is the hash of the resultsverifier proving key
	ResultsVerifierProvingKeyHash = "4952e8e98733c358190060974bb6f9e228656b9976790b3e7cd4de3ed945914a"
	// ResultsVerifierVerificationKeyURL is the URL for the resultsverifier verification key
	ResultsVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierVerificationKeyHash)
	// ResultsVerifierVerificationKeyHash is the hash of the resultsverifier verification key
	ResultsVerifierVerificationKeyHash = "9158744483ddb6e6331a6c30c753f2416daddb2edfdc5aa7bdee74195ac01ca4"

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = fmt.Sprintf("%s/%s/davinci_crypto_%s.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmHelperHash[len(BallotProofWasmHelperHash)-4:])
	// BallotProofWasmHelperHash is the hash of the WASM helper
	BallotProofWasmHelperHash = "929d6aa12fb214c3f8d75f4f9432119dacfaf72ac359c2dc3a5249297b6e104a"
	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = fmt.Sprintf("%s/%s/wasm_exec_%s.js", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmExecJsHash[len(BallotProofWasmExecJsHash)-4:])
	// BallotProofWasmExecJsHash is the hash of the WASM exec JS
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)
