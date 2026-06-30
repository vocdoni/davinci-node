package client

import (
	"fmt"

	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

// OnchainProcessResults method returns the results of the process identified by
// the ProcessID provided from onchain data. It returns an error if something
// fails and nil if no results are available of the process is not in the results
// status.
func (c *Client) OnchainProcessResults(pid types.ProcessID) ([]*types.BigInt, error) {
	if !pid.IsValid() {
		return nil, fmt.Errorf("invalid process id")
	}

	runtime, err := c.runtimes.RuntimeForProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime: %w", err)
	}

	process, err := runtime.Contracts.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil, nil
	}
	return process.Result, nil
}

// CalculateExpectedResults helper function calculates the expected results
// for the slices of ballot fields provided. It can be used to check that the
// process results are correct.
func CalculateExpectedResults(fieldValuesPerVoter [][]*types.BigInt) []*types.BigInt {
	expectedResults := [params.FieldsPerBallot]*types.BigInt{}
	for i := range expectedResults {
		expectedResults[i] = types.NewInt(0)
	}

	for _, fieldValues := range fieldValuesPerVoter {
		for i, fieldValue := range fieldValues {
			expectedResults[i] = expectedResults[i].Add(expectedResults[i], fieldValue)
		}
	}
	return expectedResults[:]
}
