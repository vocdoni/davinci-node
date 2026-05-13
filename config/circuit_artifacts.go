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

	VoteVerifierCircuitHash         = "28f60015e6f5da5f54b5bda40f7bc47b50d3299926a06cc48296e52baf4cfad0"
	VoteVerifierProvingKeyHash      = "258a94202aefdfa42ec8a89840faf5b8ce881d7192685fafd7b66c57cbc2bbdc"
	VoteVerifierVerificationKeyHash = "2cd6f8735ea6fdfed83f1c5b50c0d13fb7f6569a15682fd055f49cdd37dfadf9"

	AggregatorCircuitHash         = "57a4771b764d30d461ad0573e777b123c125d240f7f153209b82a3efa31ab984"
	AggregatorProvingKeyHash      = "36b4a4d4cae164731c7ff36d15fa6808e22847887d14b4576c836850ca06c1ba"
	AggregatorVerificationKeyHash = "617f581fb29d08389e40fc611a1cbb95a0b93942b2254b877e57d6315f313326"

	StateTransitionCircuitHash         = "8629e84ba7dea176a19167527a249335a2bd7cada58dce89ca96d5767c90b142"
	StateTransitionProvingKeyHash      = "01c933d6aa095a15d8bba2df77762cb977204363039bdf8a220f36ef29125f1e"
	StateTransitionVerificationKeyHash = "8efa82e1df45d23345782dc4bae407ff5bab6cc654cab45b7e3d1e6ba64ea143"

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
