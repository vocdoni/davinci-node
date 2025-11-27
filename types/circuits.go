package types

// used across different circuits
const (
	// StateTreeMaxLevels is the maximum number of levels in the state merkle tree.
	StateTreeMaxLevels = 64
	// CensusKeyMaxLen is the maximum length of a census key in bytes.
	CensusKeyMaxLen = 20
	// StateKeyMaxLen is the maximum length of a state key in bytes.
	StateKeyMaxLen = StateTreeMaxLevels / 8
	// VoteIDLen is the length of the vote ID in bytes (this must match circom circuit)
	VoteIDLen = 20
	// FieldsPerBallot is the number of fields in a ballot.
	FieldsPerBallot = 8
	// MaxValuePerBallotField is the maximum value per field in a ballot.
	MaxValuePerBallotField = 2 << 16 // 65536
	// VotesPerBatch is the number of votes per zkSnark batch.
	VotesPerBatch = 60
)
