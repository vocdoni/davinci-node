package statetransition

import (
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// Artifacts contains the circuit artifacts for the state transition circuit,
// which includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	&circuits.Artifact{
		Name:      "state-transition ccs",
		RemoteURL: config.StateTransitionCircuitURL,
		Hash:      types.HexStringToHexBytes(config.StateTransitionCircuitHash),
	},
	&circuits.Artifact{
		Name:      "state-transition proving key",
		RemoteURL: config.StateTransitionProvingKeyURL,
		Hash:      types.HexStringToHexBytes(config.StateTransitionProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "state-transition verification key",
		RemoteURL: config.StateTransitionVerificationKeyURL,
		Hash:      types.HexStringToHexBytes(config.StateTransitionVerificationKeyHash),
	},
)
