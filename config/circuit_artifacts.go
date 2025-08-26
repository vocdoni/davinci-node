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
	BallotProofCircuitHash = "97823742fe632dfb1f119eeddfcc327a58c8585ecf022184363e4bcfd05addf6"
	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/%s.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofProvingKeyHash)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "c4e711f7355ab5dd01677031d0bab8750ee2f1fc0917a2c8d1cb957bb7192c03"
	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/%s.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofVerificationKeyHash)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "80e69c9a7376758d7a9909b59a01c2cfa2a1daf1abce615a7fd62d1fbb3a43f5"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "04c630298497ca677dd1e4f00e369e6a1867d0f188d9077f9441d61c311f46fc"
	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "98ef68e84e1d10e9b8169a7c95ef718b0fb32acda46f14142ae814289c594868"
	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "40b5424a677b11a2e37218eb3dbd37e580a43257be16b05ca5407a38c92980a0"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "976e8b5783a5b6e8d7d83db06bfb656d7e0a32ff93e56d0e559faf75547b27b8"
	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "da0f99a052e0853f60e200d083b3b85c5f24f51243d393ac9e28ea3aa440a16a"
	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "920846a1d2a9d1085784f0550a480961e076121b02960937ab4c4ce13bfefc78"

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionCircuitHash is the hash of the statetransition circuit
	StateTransitionCircuitHash = "d2b2ff69cb12c5bbc0c1815180df3188b4f7c2a5c5cccae62cfe38f56e4ef81f"
	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionProvingKeyHash is the hash of the statetransition proving key
	StateTransitionProvingKeyHash = "44f075bb6ba0a3f0b41cf145cd142b7c929b9f0484bb83ffd88a3c5c32620f33"
	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)
	// StateTransitionVerificationKeyHash is the hash of the statetransition verification key
	StateTransitionVerificationKeyHash = "bae33f08fb31961416fae2c582004d7d506ec89c9305ee3dffeac01a0747f5c4"

	// ResultsVerifierCircuitURL is the URL for the statetransition circuit
	ResultsVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierCircuitHash)
	// ResultsVerifierCircuitHash is the hash of the statetransition circuit
	ResultsVerifierCircuitHash = "320bc166c23ad0a87ad483a795b5edea193bdca9bb51128902517ebabba9904d"
	// ResultsVerifierProvingKeyURL is the URL for the resultsverifier proving key
	ResultsVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierProvingKeyHash)
	// ResultsVerifierProvingKeyHash is the hash of the resultsverifier proving key
	ResultsVerifierProvingKeyHash = "39bfc916454140a55e35e0d958eed21001bf9661a582d029d4dd7c93b6846319"
	// ResultsVerifierVerificationKeyURL is the URL for the resultsverifier verification key
	ResultsVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierVerificationKeyHash)
	// ResultsVerifierVerificationKeyHash is the hash of the resultsverifier verification key
	ResultsVerifierVerificationKeyHash = "60c9554ac42aca2af30a88a6886d8da31d9acfeab078aa1795be15457aa1699e"

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = fmt.Sprintf("%s/%s/ballot_proof_inputs.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofWasmHelperHash is the hash of the WASM helper
	BallotProofWasmHelperHash = "7f9bdff2e042e9a9569fd40007a49f3b0cac4f2b8d9a7c1a7fe09eb0d89058c6"
	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = fmt.Sprintf("%s/%s/wasm_exec.js", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofWasmExecJsHash is the hash of the WASM exec JS
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)
