package statetransition

import (
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/solidity"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the state transition circuit,
// which includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	"statetransition",
	params.StateTransitionCurve,
	[]backend.ProverOption{solidity.WithProverTargetSolidityVerifier(backend.GROTH16)},
	[]backend.VerifierOption{solidity.WithVerifierTargetSolidityVerifier(backend.GROTH16)},
	&circuits.Artifact{
		RemoteURL: config.StateTransitionCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.StateTransitionCircuitHash),
	},
	&circuits.Artifact{
		RemoteURL: config.StateTransitionProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.StateTransitionProvingKeyHash),
	},
	&circuits.Artifact{
		RemoteURL: config.StateTransitionVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.StateTransitionVerificationKeyHash),
	},
)
