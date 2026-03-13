package voteverifier

import (
	"github.com/consensys/gnark/backend"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"

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

var ProverOptions = []backend.ProverOption{
	stdgroth16.GetNativeProverOptions(params.AggregatorCurve.ScalarField(), params.VoteVerifierCurve.ScalarField()),
}

var VerifierOptions = []backend.VerifierOption{
	stdgroth16.GetNativeVerifierOptions(params.AggregatorCurve.ScalarField(), params.VoteVerifierCurve.ScalarField()),
}
