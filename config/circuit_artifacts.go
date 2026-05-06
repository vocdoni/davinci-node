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
	BallotProofCircuitHash         = "fb0856e46cb630114f7068010f1ab4edcb43a2a491e56518dba0102cfd6926be"
	BallotProofProvingKeyHash      = "f5809dd04a10a1546a1a80276ed27881ebc9bfb0272c2cac6a03abf11b5543d4"
	BallotProofVerificationKeyHash = "d45aa6c83b8df847e9c7bbd0cce299c791ce18a2203473e9a1a89e73818e85c6"

	VoteVerifierCircuitHash         = "28b1f64ef1829d923c64d2eb6691452432f93051b525dc60f9b6b790ebe0ee4a"
	VoteVerifierProvingKeyHash      = "3b4fc020f93dcaf2ae21516e7fa99f0694ada2f399454d051cb1b5b1e8cd0b41"
	VoteVerifierVerificationKeyHash = "4e92f69599a1989e206528009685c6dfb6383f5901ed076ed49cca1f7c08b1d8"

	AggregatorCircuitHash         = "78b5a17118844ad62f6c0e8ebcfb9e97e023ada045bc134b82b6bfc209e5294b"
	AggregatorProvingKeyHash      = "5b82085bfb5ace8445b26bc5a36261f5d2acd5e40eec6daa1073a6ac67699fc5"
	AggregatorVerificationKeyHash = "0ceb6c195ff0689aa203e66c0346d16e22bdc7dab2d9b6d663a2f56e465c1037"

	StateTransitionCircuitHash         = "5ea7ae0a06de6d2cac5b798d52df2f8f162cb15eaf5cc00a088484ae47805185"
	StateTransitionProvingKeyHash      = "d429a8d4d8eeea800cfca60a1adc27f4f14e30dd693a067f8e0f4cb22f64a60a"
	StateTransitionVerificationKeyHash = "045817c9eb387f829c133ec7156a86c26e8ce0cb9194722057e6efc771561ee8"

	ResultsVerifierCircuitHash         = "6ac4cd0f7a15a105ff9fb80a5698cb6fd4b8c0e5a882851e28db91373aba6a35"
	ResultsVerifierProvingKeyHash      = "6323dadb3595fca48b4cb35b1e77229d79a128a4786115f33847cddcdc941ab4"
	ResultsVerifierVerificationKeyHash = "c4dec16c9cb19fc8e251a97743a7870d3d0a7aae2f375a8acb9b12997b8aac24"
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
)
