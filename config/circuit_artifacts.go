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
	BallotProofCircuitHash = "a463f5a999f8e409cba8a8e376a6c6c9621a1ccb1d656ebb6804a0719a3530e9"

	// BallotProofProvingKeyURL is the URL for the ballot proof proving key
	BallotProofProvingKeyURL = fmt.Sprintf("%s/%s/ballot_proof_pkey.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofProvingKeyHash is the hash of the ballot proof proving key
	BallotProofProvingKeyHash = "15c56a44f78445d428076377c2254d96e03d590b4efdc7457380baa75f395f9b"

	// BallotProofVerificationKeyURL is the URL for the ballot proof verification key
	BallotProofVerificationKeyURL = fmt.Sprintf("%s/%s/ballot_proof_vkey.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	// BallotProofVerificationKeyHash is the hash of the ballot proof verification key
	BallotProofVerificationKeyHash = "3e7a0b24250c6fea97c0950445cf104091c00bfd32796e8e8753955ab015429a"

	// VoteVerifierCircuitURL is the URL for the vote verifier circuit
	VoteVerifierCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	// VoteVerifierCircuitHash is the hash of the vote verifier circuit
	VoteVerifierCircuitHash = "8f572ad52e5f64705a0ceddf82cfc335f2cf316065084d5a6031923b5c9c1adc"

	// VoteVerifierProvingKeyURL is the URL for the vote verifier proving key
	VoteVerifierProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	// VoteVerifierProvingKeyHash is the hash of the vote verifier proving key
	VoteVerifierProvingKeyHash = "4adb3a90d7fa23d2c2ecfebf5170b23e97d1588f50bdd08b0718ce56f5a7e6fb"

	// VoteVerifierVerificationKeyURL is the URL for the vote verifier verification key
	VoteVerifierVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	// VoteVerifierVerificationKeyHash is the hash of the vote verifier verification key
	VoteVerifierVerificationKeyHash = "e3246c656a23b67bf69384b7e07230d1f83e931afeabf5aaf1ec2186465c1681"

	// AggregatorCircuitURL is the URL for the aggregator circuit
	AggregatorCircuitURL = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	// AggregatorCircuitHash is the hash of the aggregator circuit
	AggregatorCircuitHash = "d79675c1614a2bb256d5154e9acadd390f09f95b765e6580b598f717e86c95d1"

	// AggregatorProvingKeyURL is the URL for the aggregator proving key
	AggregatorProvingKeyURL = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	// AggregatorProvingKeyHash is the hash of the aggregator proving key
	AggregatorProvingKeyHash = "3c86a52b5fe395c759093f044315a906311f7edf1215e96bde0668eadd87c343"

	// AggregatorVerificationKeyURL is the URL for the aggregator verification key
	AggregatorVerificationKeyURL = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	// AggregatorVerificationKeyHash is the hash of the aggregator verification key
	AggregatorVerificationKeyHash = "6cbe50adefefeb829b08e8496912a0eda1db8348efc14cca85eb1f770d3184c0"
)
