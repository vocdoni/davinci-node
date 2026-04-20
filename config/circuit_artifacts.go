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

	VoteVerifierCircuitHash         = "a9abc91e981b0589ee40441468d7b545f7b144c19b116ce38782b7b94902febd"
	VoteVerifierProvingKeyHash      = "151d1c071adc855bdddcf6c4c89e433b24854d571deea99aaf9438cfa133fbea"
	VoteVerifierVerificationKeyHash = "2227d4222b7b71325840aa1dc582487fc6cc2ef9ff6a431c1cd17b99844041ca"

	AggregatorCircuitHash         = "b01041aaedb70d053fa59837a6516b6d995eca0998c2ca23c9b42aea5d86e922"
	AggregatorProvingKeyHash      = "09329c30dee024c54766a54d061680176122894fda3b1b71d530f80defedd0a1"
	AggregatorVerificationKeyHash = "b254195422666dd50a6b3656de9e551904b1e6897a86036926877baf91c952e3"

	StateTransitionCircuitHash         = "d6fe3e566d68ff8f9f7d0d28a21e8a575ad17ca109507c1570687c4877de1b81"
	StateTransitionProvingKeyHash      = "d8957d909801efb562a63d4c373502e98222aa2c014b9228aed0dbbf245181f1"
	StateTransitionVerificationKeyHash = "0d2ef5018a6567ab059a0ea8c2b7f1f6175b4ff1fa91b8604fdbd2ef84c54541"

	ResultsVerifierCircuitHash         = "386646c4ab455b71afa2bd8a8f03e3ad1913e81972c8eb4a14455393846c00a3"
	ResultsVerifierProvingKeyHash      = "448592882f39f7e5ef17ad70cdee5a23f95b1337ec72ae1ac36266a306cc6bea"
	ResultsVerifierVerificationKeyHash = "a3ff300fe0143bc8238fac0c8db5be74aca3ec8fa00f6701b81c365fc447551e"

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
