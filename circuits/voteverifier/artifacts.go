package voteverifier

import (
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the vote verifier circuit,
// which includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	circuits.VoteVerifierCurve,
	&circuits.Artifact{
		Name:      "vote-verifier ccs",
		RemoteURL: config.VoteVerifierCircuitURL,
		Hash:      types.HexStringToHexBytes(config.VoteVerifierCircuitHash),
	},
	&circuits.Artifact{
		Name:      "vote-verifier proving key",
		RemoteURL: config.VoteVerifierProvingKeyURL,
		Hash:      types.HexStringToHexBytes(config.VoteVerifierProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "vote-verifier verification key",
		RemoteURL: config.VoteVerifierVerificationKeyURL,
		Hash:      types.HexStringToHexBytes(config.VoteVerifierVerificationKeyHash),
	},
)
