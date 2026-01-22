package params

import (
	"math"

	"github.com/consensys/gnark-crypto/ecc"
)

const (
	// FieldsPerBallot is the number of fields in a ballot.
	FieldsPerBallot = 8
	// MaxValuePerBallotField is the maximum value per field in a ballot.
	MaxValuePerBallotField = 2 << 16
	// VotesPerBatch is the number of votes per zkSnark batch.
	VotesPerBatch = 60
	// StateTreeMaxLevels is the maximum number of levels in the state merkle tree.
	StateTreeMaxLevels = 64
)

// Curves
const (
	BallotProofCurve     = ecc.BN254
	VoteVerifierCurve    = ecc.BLS12_377
	AggregatorCurve      = ecc.BW6_761
	StateTransitionCurve = ecc.BN254
	ResultsVerifierCurve = ecc.BN254
)

// State Config Keys
const (
	StateKeyProcessID     uint64 = 0x00
	StateKeyCensusOrigin  uint64 = 0x01
	StateKeyBallotMode    uint64 = 0x02
	StateKeyEncryptionKey uint64 = 0x03
	StateKeyResultsAdd    uint64 = 0x04
	StateKeyResultsSub    uint64 = 0x05
)

// State Namespaces
const (
	ConfigMin uint64 = 0                                        // 0x0000_0000_0000_0000
	ConfigMax uint64 = 1<<4 - 1                                 // 0x0000_0000_0000_000F
	BallotMin uint64 = ConfigMax + 1                            // 0x0000_0000_0000_0010
	BallotMax uint64 = VoteIDMin - 1                            // 0x7FFF_FFFF_FFFF_FFFF
	VoteIDMin uint64 = (math.MaxUint64 - 1<<VoteIDHashBits) + 1 // 0x8000_0000_0000_0000
	VoteIDMax uint64 = math.MaxUint64                           // 0xFFFF_FFFF_FFFF_FFFF

	VoteIDHashBits uint = 63 // bits

	CensusAddressBitLen uint = 16 // bits

	CensusIndexMax = BallotMax>>CensusAddressBitLen - ConfigMax // 0x0000_7FFF_FFFF_FFF0
)
