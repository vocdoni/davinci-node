package storage

import (
	"fmt"

	"github.com/vocdoni/davinci-node/census/censusdb"
	"github.com/vocdoni/davinci-node/types"
)

// LoadCensus loads a census from the local census database based on the
// provided census info. The chainID is only required for dynamic on-chain
// censuses because the same census contract address can exist on multiple
// chains.
func (s *Storage) LoadCensus(chainID uint64, censusInfo *types.Census) (ref *censusdb.CensusRef, err error) {
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
		if chainID == 0 {
			return nil, fmt.Errorf("chain ID is required for dynamic on-chain census lookup")
		}
		// On-chain dynamic census are identified by the census manager
		// contract address within the process chain runtime.
		return s.CensusDB().LoadByScopedAddress(chainID, censusInfo.ContractAddress)
	default:
		// Other Merkle tree-based censuses are identified by their root hash
		return s.CensusDB().LoadByRoot(censusInfo.CensusRoot)
	}
}
