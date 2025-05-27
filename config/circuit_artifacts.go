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
	// BallotProoCircuitURL is the URL for the ballot proof circuit WASM file
	BallotProoCircuitURL = fmt.Sprintf("%s/%s/ballot_proof.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofCircuitHash is the hash of the ballot proof circuit
	BallotProofCircuitHash = "5a6f7d40c1e74c238cc282c4bcc22a0a623b6fa8426c01cd7e8ef45e34394faf"
	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/ballot_proof_pkey.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "f4bc379bb933946a558bdbe504e93037c8049fbb809fb515e452f0f370e27cef"
	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/ballot_proof_vkey.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "833c8f97ed01858e083f3c8b04965f168400a2cc205554876e49d32b14ddebe8"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "2395f99ec9afa47b343c7ed5654831acb77508cba730be2b43690a750f47b3b2"
	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "0cc65be7ecacd794c9fdea6f2a4a104a6a84c90dec8d54a7b359a6c78903ef69"
	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "627972190bb0267795efdb013ee8f90a1b13b30889e090467f29c26379031fb1"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "47df8b41e4ea7b5200cbe386da0e5eff9fe7bf83e8bd5e9e64e97537af922f5a"
	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "5af4fc88b735f111c73863756391b69c4e5e9f4aedcd7c2d89d8163887cde02f"
	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "fda6d670c32e9f2175c725c3ed134411ec2a85a7b8557266a18d0df495bb3692"

	// StateTransitionCircuitURL is the URL for the statetransition circuit
	StateTransitionCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionCircuitHash)
	// StateTransitionCircuitHash is the hash of the statetransition circuit
	StateTransitionCircuitHash = "854b7fc073159b509cbd6b6d85ce349746d9855cfc9468bcf7a076b0b9c53536"
	// StateTransitionProvingKeyURL is the URL for the statetransition proving key
	StateTransitionProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionProvingKeyHash)
	// StateTransitionProvingKeyHash is the hash of the statetransition proving key
	StateTransitionProvingKeyHash = "00f1eedfb001e2344446bddbcd1c3a181236a14a6307d2f97c300328ccfbb5e3"
	// StateTransitionVerificationKeyURL is the URL for the statetransition verification key
	StateTransitionVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, StateTransitionVerificationKeyHash)
	// StateTransitionVerificationKeyHash is the hash of the statetransition verification key
	StateTransitionVerificationKeyHash = "8d6aa9fe25d82ff269fbeb6e9db82d5fa18c0e5cacf388bac8f8b13dae8a9661"

	// ResultsVerifierCircuitURL is the URL for the statetransition circuit
	ResultsVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierCircuitHash)
	// ResultsVerifierCircuitHash is the hash of the statetransition circuit
	ResultsVerifierCircuitHash = "abdf003a2918fc7b816366acc036c686029d8b1a164ba1212c0bea472d18596d"
	// ResultsVerifierProvingKeyURL is the URL for the resultsverifier proving key
	ResultsVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierProvingKeyHash)
	// ResultsVerifierProvingKeyHash is the hash of the resultsverifier proving key
	ResultsVerifierProvingKeyHash = "dda26767b8a72edb0db22278bc3b69446b17ac6377e92a61c5214c153b917a65"
	// ResultsVerifierVerificationKeyURL is the URL for the resultsverifier verification key
	ResultsVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, ResultsVerifierVerificationKeyHash)
	// ResultsVerifierVerificationKeyHash is the hash of the resultsverifier verification key
	ResultsVerifierVerificationKeyHash = "71c909a5e7abc6f066b70b0627f2cd2d52eb1e25b083d8bbeebcabc6e84d2362"

	// BallotProofWasmHelperURL is the default URL for the WASM helper
	BallotProofWasmHelperURL = "https://github.com/vocdoni/davinci-node/raw/refs/heads/main/cmd/ballotproof-wasm/ballotproof.wasm"
	// BallotProofWasmHelperHash is the hash of the WASM helper
	BallotProofWasmHelperHash = "78e66e787ca075445da0009ff203cfb9acf18f759c787cbf2e3eade99e72fd61"
	// BallotProofWasmExecJsURL is the default URL for the WASM exec JS
	BallotProofWasmExecJsURL = "https://github.com/vocdoni/davinci-node/raw/refs/heads/main/cmd/ballotproof-wasm/wasm_exec.js"
	// BallotProofWasmExecJsHash is the hash of the WASM exec JS
	BallotProofWasmExecJsHash = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
)
