package params

// used across different circuits
const (
	// FieldsPerBallot is the number of fields in a ballot.
	FieldsPerBallot = 8
	// MaxValuePerBallotField is the maximum value per field in a ballot.
	MaxValuePerBallotField = 2 << 16
	// VotesPerBatch is the number of votes per zkSnark batch.
	VotesPerBatch = 60
)
