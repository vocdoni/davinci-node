package results

import (
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// Artifacts contains the circuit artifacts for the aggregator circuit, which
// includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	circuits.ResultsVerifierCurve,
	&circuits.Artifact{
		Name:      "results verifier ccs",
		RemoteURL: config.ResultsVerifierCircuitURL,
		Hash:      types.HexStringToHexBytes(config.ResultsVerifierCircuitHash),
	},
	&circuits.Artifact{
		Name:      "results verifier proving key",
		RemoteURL: config.ResultsVerifierProvingKeyURL,
		Hash:      types.HexStringToHexBytes(config.ResultsVerifierProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "results verifier verification key",
		RemoteURL: config.ResultsVerifierVerificationKeyURL,
		Hash:      types.HexStringToHexBytes(config.ResultsVerifierVerificationKeyHash),
	},
)
