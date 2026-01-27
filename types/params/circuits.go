package params

import "github.com/ethereum/go-ethereum/common"

// used across different circuits
const (
	// FieldsPerBallot is the number of fields in a ballot.
	FieldsPerBallot = 8
	// MaxValuePerBallotField is the maximum value per field in a ballot.
	MaxValuePerBallotField = 2 << 16
	// VotesPerBatch is the number of votes per zkSnark batch.
	VotesPerBatch = 60
	// AddressBitLen is the length in bits of a common.Address
	AddressBitLen = common.AddressLength * 8
)
