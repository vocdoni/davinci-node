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
	BallotProofCircuitHash         = "5533928fc3a028f677a6dab2e9b02df7d6d7b9c0fed334f36a500bf36678c10a"
	BallotProofProvingKeyHash      = "f4a9ad3537ebf9295d1409b8ce3ebb49e80247526956bfdb04d80ebc5fed58f7"
	BallotProofVerificationKeyHash = "fb94a56b5b1e90df5ae7f22ea60e836792a6a5d5b50382f6faa154df0175778e"

	VoteVerifierCircuitHash         = "5e955e7a63f299648ceaa2026dec7348a441bd1738eef1ec01a221d236a9414d"
	VoteVerifierProvingKeyHash      = "9d17ca05c9ee797be4ca2360577ab67531f21906915e87bdc41f001d1ee53e77"
	VoteVerifierVerificationKeyHash = "8ccc7f7632867f79df8ad6943c7ec281b705434e6d7a1e81e9d85b31e617a295"

	AggregatorCircuitHash         = "3613ed3b97b50cef8cc94c814cf4d2bdc429f4f5424255874f5c846b34778acb"
	AggregatorProvingKeyHash      = "b36a1fe9edef695df091a24248147c7b5cfc7d60787f5eaabffcd49857f20595"
	AggregatorVerificationKeyHash = "c36659988a2c4c0ed57680d1c3a1966cc88fdc861917dcfd44a8eaf8c368da18"

	StateTransitionCircuitHash         = "593953559138de99bcdb6827fac195e0e44513b0e2dc905f074ed4c763baed8d"
	StateTransitionProvingKeyHash      = "a0fae1c57660e8203d3e497c5691c839b10d1fce8f0c1e0cb25a1d1f7728ff16"
	StateTransitionVerificationKeyHash = "6808982cec3a2c2bb23556d253422c63de6cb13be4e26ac08f48edc36a62ad7a"

	ResultsVerifierCircuitHash         = "386646c4ab455b71afa2bd8a8f03e3ad1913e81972c8eb4a14455393846c00a3"
	ResultsVerifierProvingKeyHash      = "448592882f39f7e5ef17ad70cdee5a23f95b1337ec72ae1ac36266a306cc6bea"
	ResultsVerifierVerificationKeyHash = "a3ff300fe0143bc8238fac0c8db5be74aca3ec8fa00f6701b81c365fc447551e"
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
