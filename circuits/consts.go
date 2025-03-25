package circuits

// used across different circuits
const (
	// CensusTreeMaxLevels is the maximum number of levels in the census merkle tree.
	CensusTreeMaxLevels = 160
	// CensusKeyMaxLen is the maximum length of a census key in bytes.
	CensusKeyMaxLen = CensusTreeMaxLevels / 8
	// FieldsPerBallot is the number of fields in a ballot.
	FieldsPerBallot = 8
	// VotesPerBatch is the number of votes per zkSnark batch.
	VotesPerBatch = 100
)
