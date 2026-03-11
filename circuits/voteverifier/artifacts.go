package voteverifier

import (
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the vote verifier circuit,
// which includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	"voteverifier",
	params.VoteVerifierCurve,
	&circuits.Artifact{
		RemoteURL: config.VoteVerifierCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.VoteVerifierCircuitHash),
	},
	&circuits.Artifact{
		RemoteURL: config.VoteVerifierProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.VoteVerifierProvingKeyHash),
	},
	&circuits.Artifact{
		RemoteURL: config.VoteVerifierVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.VoteVerifierVerificationKeyHash),
	},
)
