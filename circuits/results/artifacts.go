package results

import (
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the aggregator circuit, which
// includes the proving and verification keys.
var Artifacts = circuits.NewCircuitArtifacts(
	"resultsverifier",
	params.ResultsVerifierCurve,
	&circuits.Artifact{
		RemoteURL: config.ResultsVerifierCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierCircuitHash),
	},
	&circuits.Artifact{
		RemoteURL: config.ResultsVerifierProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierProvingKeyHash),
	},
	&circuits.Artifact{
		RemoteURL: config.ResultsVerifierVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.ResultsVerifierVerificationKeyHash),
	},
)

var ProverOptions = []backend.ProverOption{solidity.WithProverTargetSolidityVerifier(backend.GROTH16)}

var VerifierOptions = []backend.VerifierOption{solidity.WithVerifierTargetSolidityVerifier(backend.GROTH16)}
