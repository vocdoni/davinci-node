package config

import "fmt"

const (
	DefaultArtifactsBaseURL = "https://circuits.ams3.cdn.digitaloceanspaces.com"
	DefaultArtifactsRelease = "dev"
)

var (
	BallotProoCircuitURL   = fmt.Sprintf("%s/%s/ballot_proof.wasm", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	BallotProofCircuitHash = "a463f5a999f8e409cba8a8e376a6c6c9621a1ccb1d656ebb6804a0719a3530e9"

	BallotProofProvingKeyURL  = fmt.Sprintf("%s/%s/ballot_proof_pkey.zkey", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	BallotProofProvingKeyHash = "15c56a44f78445d428076377c2254d96e03d590b4efdc7457380baa75f395f9b"

	BallotProofVerificationKeyURL  = fmt.Sprintf("%s/%s/ballot_proof_vkey.json", DefaultArtifactsBaseURL, DefaultArtifactsRelease)
	BallotProofVerificationKeyHash = "3e7a0b24250c6fea97c0950445cf104091c00bfd32796e8e8753955ab015429a"

	VoteVerifierCircuitURL  = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierCircuitHash)
	VoteVerifierCircuitHash = "4b47eb67473a8ab612a765f8419ef26bc932e915a5af0f6c0f8904f006872405"

	VoteVerifierProvingKeyURL  = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierProvingKeyHash)
	VoteVerifierProvingKeyHash = "6c73041da4b3fc7e7274381be8053986a1f8a59b6c1e66b138219ac31e8d890f"

	VoteVerifierVerificationKeyURL  = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, VoteVerifierVerificationKeyHash)
	VoteVerifierVerificationKeyHash = "10defd9081f4871cd8460638853e482481284540edc6987b7e32bd0a616d12e4"

	AgregatorCircuitURL   = fmt.Sprintf("%s/%s/%s.ccs", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorCircuitHash)
	AggregatorCircuitHash = "e509873d96b3066e1a914aec4373337f082b08adaee4a85b708b8a73568203c3"

	AggregatorProvingKeyURL  = fmt.Sprintf("%s/%s/%s.pk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorProvingKeyHash)
	AggregatorProvingKeyHash = "d0dd6a1836865d29889b7ce56bfe013ad3a228ef2e5d6b2f9fc66dbce2e99ef5"

	AggregatorVerificationKeyURL  = fmt.Sprintf("%s/%s/%s.vk", DefaultArtifactsBaseURL, DefaultArtifactsRelease, AggregatorVerificationKeyHash)
	AggregatorVerificationKeyHash = "05bf296799e68e62aff2485e03c12007bef0a8ce218d44c5a310384912f3f71c"
)
