package helpers

import (
	"fmt"

	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func FetchResultsOnChain(contracts *web3.Contracts, pid types.ProcessID) ([]*types.BigInt, error) {
	process, err := contracts.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil, nil
	}
	return process.Result, nil
}

func CalculateExpectedResults(fieldValuesPerVoter [][]*types.BigInt) []*types.BigInt {
	expectedResults := []*types.BigInt{
		types.NewInt(0),
		types.NewInt(0),
		types.NewInt(0),
		types.NewInt(0),
		types.NewInt(0),
		types.NewInt(0),
		types.NewInt(0),
		types.NewInt(0),
	}

	for _, fieldValues := range fieldValuesPerVoter {
		for i, fieldValue := range fieldValues {
			expectedResults[i] = expectedResults[i].Add(expectedResults[i], fieldValue)
		}
	}
	return expectedResults
}
