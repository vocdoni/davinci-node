package ballotproof

import (
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// Artifacts contains the circuit artifacts for the ballot proof verification,
// it only contains the verification key because the proving key is used by
// the voter to generate the proof.
var Artifacts = circuits.NewCircuitArtifacts(
	&circuits.Artifact{
		RemoteURL: config.BallotProoCircuitURL,
		Hash:      types.HexStringToHexBytes(config.BallotProofCircuitHash),
	},
	&circuits.Artifact{
		RemoteURL: config.BallotProofProvingKeyURL,
		Hash:      types.HexStringToHexBytes(config.BallotProofProvingKeyHash),
	},
	&circuits.Artifact{
		RemoteURL: config.BallotProofVerificationKeyURL,
		Hash:      types.HexStringToHexBytes(config.BallotProofVerificationKeyHash),
	})
