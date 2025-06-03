package statetransition

import (
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the state transition circuit,
// which includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	circuits.StateTransitionCurve,
	&circuits.Artifact{
		Name:      "state-transition ccs",
		RemoteURL: config.StateTransitionCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.StateTransitionCircuitHash),
	},
	&circuits.Artifact{
		Name:      "state-transition proving key",
		RemoteURL: config.StateTransitionProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.StateTransitionProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "state-transition verification key",
		RemoteURL: config.StateTransitionVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.StateTransitionVerificationKeyHash),
	},
)
