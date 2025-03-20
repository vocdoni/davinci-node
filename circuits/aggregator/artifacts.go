package aggregator

import (
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/config"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// Artifacts contains the circuit artifacts for the aggregator circuit, which
// includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	&circuits.Artifact{
		Name:      "aggregator ccs",
		RemoteURL: config.AgregatorCircuitURL,
		Hash:      types.HexStringToHexBytes(config.AggregatorCircuitHash),
	},
	&circuits.Artifact{
		Name:      "aggregator proving key",
		RemoteURL: config.AggregatorProvingKeyURL,
		Hash:      types.HexStringToHexBytes(config.AggregatorProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "aggregator verification key",
		RemoteURL: config.AggregatorVerificationKeyURL,
		Hash:      types.HexStringToHexBytes(config.AggregatorVerificationKeyHash),
	},
)
