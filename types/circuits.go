package types

// used across different circuits
const (
	// CensusTreeMaxLevels is the maximum number of levels in the census merkle tree.
	CensusTreeMaxLevels = 160
	// StateTreeMaxLevels is the maximum number of levels in the state merkle tree.
	StateTreeMaxLevels = 64
	// CensusKeyMaxLen is the maximum length of a census key in bytes.
	CensusKeyMaxLen = CensusTreeMaxLevels / 8
	// StateKeyMaxLen is the maximum length of a state key in bytes.
	StateKeyMaxLen = StateTreeMaxLevels / 8
	// FieldsPerBallot is the number of fields in a ballot.
	FieldsPerBallot = 8
	// MaxValuePerBallotField is the maximum value per field in a ballot.
	MaxValuePerBallotField = 2 << 16 // 65536
	// VotesPerBatch is the number of votes per zkSnark batch.
	VotesPerBatch = 60
)
