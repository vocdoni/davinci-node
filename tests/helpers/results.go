package helpers

import (
	"fmt"

	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func TestResultsOnChain(contracts *web3.Contracts, pid types.ProcessID) ([]*types.BigInt, error) {
	process, err := contracts.Process(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil, nil
	}
	return process.Result, nil
}
