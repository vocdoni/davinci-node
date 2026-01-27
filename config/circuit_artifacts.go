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
	BallotProofCircuitHash = "e287dbc72dbdb4a938db90b61637a0249b5d4f8c294fbcf55e4f08881883112a"
	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/%s.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofProvingKeyHash)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "982f1a850bc12325001e0c91d5238ae69b6b7ea20b3d5c1c18319d70a5dd2d65"
	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/%s.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofVerificationKeyHash)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "1675949c5628cfd347e93cfe5187805d28b5e8cb58591085b58eaa1953371991"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "2ff6171cb9375f37525d65d1b494c64815f3fc3fe941a5d48b81a21857501bc9"
	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "1d14b75537e34f8c57ab61ca81183a1894c61b8628064a17e8cff73962e52481"
	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "8f43dea416f2825b7260cbc97db48dcff5cf97d9a02f2456c6785e4337a2a437"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "a28295ad0809d62fa7b91361affb4ed6d8f9316238f9f31bc4a8146a9936edf5"
	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "65e54e4d9fbf37868df319f912349a28a85894331c3fb5636280fc4595389f01"
	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "c56af8207d194f10d1670692f0f8ede73716c59d7c6bcc8790c8796d8f76bc1f"

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionCircuitHash is the hash of the statetransition circuit
	StateTransitionCircuitHash = "df9f48bba60bfa8da957decca63dbcb5a29d4c53519768089770e34d480cb0cf"
	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionProvingKeyHash is the hash of the statetransition proving key
	StateTransitionProvingKeyHash = "266a108020a122a6d7df8d684505797acf31e3d7c63538513780df50190cb856"
	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)
	// StateTransitionVerificationKeyHash is the hash of the statetransition verification key
	StateTransitionVerificationKeyHash = "a3c1a84d855d5d79bbce4c63661de38504df85f597794060ed3dd3e01c1d8209"

	// ResultsVerifierCircuitURL is the URL for the statetransition circuit
	ResultsVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierCircuitHash)
	// ResultsVerifierCircuitHash is the hash of the statetransition circuit
	ResultsVerifierCircuitHash = "7da3dc581758815ae050b592d4a25189a44c2ec7e3e122559573a4f953a1e412"
	// ResultsVerifierProvingKeyURL is the URL for the resultsverifier proving key
	ResultsVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierProvingKeyHash)
	// ResultsVerifierProvingKeyHash is the hash of the resultsverifier proving key
	ResultsVerifierProvingKeyHash = "e33a10d6072f8ee09f80c05120017ea1fa5f587b6a211b50068d4de63d549131"
	// ResultsVerifierVerificationKeyURL is the URL for the resultsverifier verification key
	ResultsVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierVerificationKeyHash)
	// ResultsVerifierVerificationKeyHash is the hash of the resultsverifier verification key
	ResultsVerifierVerificationKeyHash = "0d4d6faa042d661415744fbe98c6af788d8586115a26fb01213b08d544610ec4"

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = fmt.Sprintf("%s/%s/davinci_crypto_%s.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmHelperHash[len(BallotProofWasmHelperHash)-4:])
	// BallotProofWasmHelperHash is the hash of the WASM helper
	BallotProofWasmHelperHash = "9ea8f6989eab0c8e848902ee4289d6b6acb076056f0391e483befbe8348bc6c8"
	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = fmt.Sprintf("%s/%s/wasm_exec_%s.js", DefaultArtifactsBaseURL, DefaultArtifactsRelease, BallotProofWasmExecJsHash[len(BallotProofWasmExecJsHash)-4:])
	// BallotProofWasmExecJsHash is the hash of the WASM exec JS
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)
