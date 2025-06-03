package results

import (
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the aggregator circuit, which
// includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	circuits.ResultsVerifierCurve,
	&circuits.Artifact{
		Name:      "results verifier ccs",
		RemoteURL: config.ResultsVerifierCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierCircuitHash),
	},
	&circuits.Artifact{
		Name:      "results verifier proving key",
		RemoteURL: config.ResultsVerifierProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "results verifier verification key",
		RemoteURL: config.ResultsVerifierVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierVerificationKeyHash),
	},
)
