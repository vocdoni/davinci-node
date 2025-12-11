package test

import (
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/state"
	imt "github.com/vocdoni/lean-imt-go"
	imtcensus "github.com/vocdoni/lean-imt-go/census"
)

// CensusIMTForTest creates a CensusIMT instance for testing purposes including
// the provided votes as census participants. It returns the initialized
// CensusIMT or an error if the process fails.
func CensusIMTForTest(votes []state.Vote) (*imtcensus.CensusIMT, error) {
	// generate the census with voters information
	votersData := map[*big.Int]*big.Int{}
	for _, v := range votes {
		votersData[v.Address] = v.Weight
	}

	// Create a unique directory name to avoid lock conflicts
	// Include timestamp and process info for uniqueness
	censusDir := os.TempDir() + fmt.Sprintf("/census_imt_test_%d", time.Now().UnixNano())

	// Initialize the census merkle tree
	censusTree, err := imtcensus.NewCensusIMTWithPebble(censusDir, imt.PoseidonHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create census IMT: %w", err)
	}

	// Clean up the census directory when done
	defer func() {
		if err := censusTree.Close(); err != nil {
			log.Printf("Warning: failed to close census IMT: %v", err)
		}
		if err := os.RemoveAll(censusDir); err != nil {
			log.Printf("Warning: failed to cleanup census directory %s: %v", censusDir, err)
		}
	}()

	bAddresses, bWeights := []common.Address{}, []*big.Int{}
	for address, weight := range votersData {
		bAddresses = append(bAddresses, common.BigToAddress(address))
		bWeights = append(bWeights, weight)
	}
	if err := censusTree.AddBulk(bAddresses, bWeights); err != nil {
		return nil, fmt.Errorf("failed to add bulk to census IMT: %w", err)
	}
	return censusTree, nil
}
