package ballotproof

import (
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/types"
)

// Artifacts contains the circuit artifacts for the ballot proof verification,
// it only contains the verification key because the proving key is used by
// the voter to generate the proof.
var Artifacts = circuits.NewCircuitArtifacts(
	circuits.BallotProofCurve,
	&circuits.Artifact{
		Name:      "ballot-proof wasm",
		RemoteURL: config.BallotProoCircuitURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.BallotProofCircuitHash),
	},
	&circuits.Artifact{
		Name:      "ballot-proof proving key",
		RemoteURL: config.BallotProofProvingKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.BallotProofProvingKeyHash),
	},
	&circuits.Artifact{
		Name:      "ballot-proof verification key",
		RemoteURL: config.BallotProofVerificationKeyURL,
		Hash:      types.HexStringToHexBytesMustUnmarshal(config.BallotProofVerificationKeyHash),
	})
