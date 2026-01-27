package storage

import (
	"fmt"

	"github.com/vocdoni/davinci-node/census/censusdb"
	"github.com/vocdoni/davinci-node/types"
)

// LoadCensus loads a census from the local census database based on the
// provided census info.
func (s *Storage) LoadCensus(censusInfo *types.Census) (ref *censusdb.CensusRef, err error) {
	// No census info provided
	if censusInfo == nil {
		return nil, fmt.Errorf("no census info provided")
	}

	// CSP-based censuses are not stored locally
	if censusInfo.CensusOrigin.IsCSP() {
		return nil, nil
	}

	// Load census based on its origin
	switch censusInfo.CensusOrigin {
	case types.CensusOriginMerkleTreeOnchainDynamicV1:
		// On-chain dynamic census are identified by the census manager
		// contract address
		return s.CensusDB().LoadByAddress(censusInfo.ContractAddress)
	default:
		// Other Merkle tree-based censuses are identified by their root hash
		return s.CensusDB().LoadByRoot(censusInfo.CensusRoot)
	}
}
