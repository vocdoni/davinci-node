package params

import gethparams "github.com/ethereum/go-ethereum/params"

const (
	// Number of field elements stored in a single data blob
	BlobTxFieldElementsPerBlob = gethparams.BlobTxFieldElementsPerBlob
	// Size in bytes of a field element
	BlobTxBytesPerFieldElement = gethparams.BlobTxBytesPerFieldElement
)
