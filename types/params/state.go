package params

const (
	// StateTreeMaxLevels is the maximum number of levels in the state merkle tree.
	StateTreeMaxLevels = 64
)

// state config keys
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
	ConfigMin uint64 = 0x0000_0000_0000_0000 //
	ConfigMax uint64 = 0x0000_0000_0000_000F // 4 bits
	BallotMin uint64 = 0x0000_0000_0000_0010 // = ConfigMax + 1
	BallotMax uint64 = 0x7FFF_FFFF_FFFF_FFFF // = VoteIDMin - 1
	VoteIDMin uint64 = 0x8000_0000_0000_0000 // = 1<<63 = 0b1000...000 (64 bits)
	VoteIDMax uint64 = 0xFFFF_FFFF_FFFF_FFFF // = 1<<64 - 1

	CensusAddressBitLen = 16 // bits

	CensusIndexMax = BallotMax>>CensusAddressBitLen - ConfigMax // 0x0000_7FFF_FFFF_FFF0
)
