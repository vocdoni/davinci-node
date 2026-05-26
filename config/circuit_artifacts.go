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

	AggregatorCircuitHash         = "965bb9303a017150189f50c1758ada085e3c5f081703079d142a04b91163a66b"
	AggregatorProvingKeyHash      = "5b6bc7df866bcc29b08569907f120c0f2ead0a296eb057db5c9d39ff76a2acf5"
	AggregatorVerificationKeyHash = "f1220518a0ab6b238b26cbba72902324e8208597a3337f629ff1be906b899069"

	StateTransitionCircuitHash         = "298b07f35deb817e6accd01233a8ef2381513237f1160954bdc50155861178a9"
	StateTransitionProvingKeyHash      = "cc3dc018ffcca72cd6bc4e75e51ad6924aa5d8eb60b02f950f4f27823ab9cbc7"
	StateTransitionVerificationKeyHash = "fba0febdf6c87aa1aeac89c0628597edc76d6551739163bf470c6a6cfd810042"

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
