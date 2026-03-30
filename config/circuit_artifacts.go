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

	VoteVerifierCircuitHash         = "453a747ed632925caf3562387a9cfe35ab1eb614cf94c0381766e64a64574140"
	VoteVerifierProvingKeyHash      = "ef6fc1faeb8127daa764a450f3670d4868c3b04b0f7dbdfb7e11fcffd8880d90"
	VoteVerifierVerificationKeyHash = "474eaefe526cf7a14cff5abca84f977d37bc8f81792fec38c665c5c13cff75af"

	AggregatorCircuitHash         = "71cfaed635c49d2908f19335ec8d655355c588f044dbb119e8d270174b5d76a4"
	AggregatorProvingKeyHash      = "60389f873fae3930766c61e040cb7914f13dab9ff18ba92d96a268744fca053c"
	AggregatorVerificationKeyHash = "28ade332befee56a517c3690e8ec715b4a4ad0ae4a5c50efe7b895848d1a5167"

	StateTransitionCircuitHash         = "918f0c6dc2f6be872039a299c887d9a39ba50fc0569b20650ad05d37ae93f754"
	StateTransitionProvingKeyHash      = "95915e101554e1c339152a833111e1d224aef59d00f36a5ea9a38738b40c53bb"
	StateTransitionVerificationKeyHash = "5f2eb7751d003626925c10365bc26841862485b454bc58632c5ca42c882004b1"

	ResultsVerifierCircuitHash         = "b8d22a63d7abce00c7288310ccfebcdd032842661296bc3464d42ebfec1f4079"
	ResultsVerifierProvingKeyHash      = "b69c1febc85c14c4b6200fb390d89e92e0f85d9177d2954d7dfa47005d91b5a8"
	ResultsVerifierVerificationKeyHash = "9c7f7cf4bcffa4d30cef5d94be0a3401de8bb461d0f76b0f959a17f5e99694fe"

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
