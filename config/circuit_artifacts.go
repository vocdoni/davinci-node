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
	BallotProofCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofCircuitHash)
	// BallotProofCircuitHash is the hash of the ballot proof circuit
	BallotProofCircuitHash = "e287dbc72dbdb4a938db90b61637a0249b5d4f8c294fbcf55e4f08881883112a"
	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofProvingKeyHash)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "982f1a850bc12325001e0c91d5238ae69b6b7ea20b3d5c1c18319d70a5dd2d65"
	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofVerificationKeyHash)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "1675949c5628cfd347e93cfe5187805d28b5e8cb58591085b58eaa1953371991"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "2ff6171cb9375f37525d65d1b494c64815f3fc3fe941a5d48b81a21857501bc9"
	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "50551373a872d879f4a3f4f44f7623a1808af769f89a2b065c44efdb87ccfd48"
	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "dd2f12457371cfa6b66986b1a0ba1f1ed7aa8f6353047b770e913ea3c60ecf29"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "ab2d30824585c3792a0e6a11e737c42f6f69ad415267de2a76ac136fedeacd34"
	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "0d2ac228015fed55e39a282e329284887b4369c6e518dbe647ec63d49cc3252e"
	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "4bdb2f9e44d63d7db6f37cadcbd8202f8f429b6ab47310bec7ed81257b209a41"

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionCircuitHash is the hash of the statetransition circuit
	StateTransitionCircuitHash = "04aad8edff68c555d5596191a026eb678bc1c8862d97a0d58868de194a209c9f"
	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionProvingKeyHash is the hash of the statetransition proving key
	StateTransitionProvingKeyHash = "cab95875bab00072e9b086fcff69cb2236951299c57f1b46b28428215333c341"
	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)
	// StateTransitionVerificationKeyHash is the hash of the statetransition verification key
	StateTransitionVerificationKeyHash = "313215883560eb6e9f0d6bf1ffcdad568ecf013e44dd67af65b1203745cfdd93"

	// ResultsVerifierCircuitURL is the URL for the statetransition circuit
	ResultsVerifierCircuitURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierCircuitHash)
	// ResultsVerifierCircuitHash is the hash of the statetransition circuit
	ResultsVerifierCircuitHash = "7da3dc581758815ae050b592d4a25189a44c2ec7e3e122559573a4f953a1e412"
	// ResultsVerifierProvingKeyURL is the URL for the resultsverifier proving key
	ResultsVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierProvingKeyHash)
	// ResultsVerifierProvingKeyHash is the hash of the resultsverifier proving key
	ResultsVerifierProvingKeyHash = "b590ba65ecac3aa3c4cc2acdacaa4401c607720beb50c12ea0dd6d23b64e87a0"
	// ResultsVerifierVerificationKeyURL is the URL for the resultsverifier verification key
	ResultsVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierVerificationKeyHash)
	// ResultsVerifierVerificationKeyHash is the hash of the resultsverifier verification key
	ResultsVerifierVerificationKeyHash = "9cbb1b36ac8e7ee1fe32a9c7d8c11d47bb7e88672121752e46eb3ff271cabdeb"

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = fmt.Sprintf("%s/%s/davinci_crypto_%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmHelperHash[len(BallotProofWasmHelperHash)-4:])
	// BallotProofWasmHelperHash is the hash of the WASM helper
	BallotProofWasmHelperHash = "9ea8f6989eab0c8e848902ee4289d6b6acb076056f0391e483befbe8348bc6c8"
	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = fmt.Sprintf("%s/%s/wasm_exec_%s", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmExecJsHash[len(BallotProofWasmExecJsHash)-4:])
	// BallotProofWasmExecJsHash is the hash of the WASM exec JS
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)
