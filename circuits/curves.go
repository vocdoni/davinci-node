package circuits

import (
	"github.com/consensys/gnark-crypto/ecc"
)

var (
	BallotProofCurve     = ecc.BN254     // ecc.BN254
	VoteVerifierCurve    = ecc.BLS12_377 // ecc.BLS12_377
	AggregatorCurve      = ecc.BW6_761   // ecc.BW6_761
	StateTransitionCurve = ecc.BN254     // ecc.BN254
	ResultsVerifierCurve = ecc.BN254     // ecc.BN254
)
