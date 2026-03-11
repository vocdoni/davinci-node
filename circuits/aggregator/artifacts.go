package aggregator

import (
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the aggregator circuit, which
// includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	"aggregator",
	params.AggregatorCurve,
	&circuits.Artifact{
		RemoteURL: config.AggregatorCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.AggregatorCircuitHash),
	},
	&circuits.Artifact{
		RemoteURL: config.AggregatorProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.AggregatorProvingKeyHash),
	},
	&circuits.Artifact{
		RemoteURL: config.AggregatorVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.AggregatorVerificationKeyHash),
	},
)
