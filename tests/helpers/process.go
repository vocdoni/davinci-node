package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func NewProcess(
	contracts *web3.Contracts,
	cli *client.HTTPclient,
) (types.ProcessID, *types.EncryptionKey, error) {
	// Get the next process ID from the contracts
	processID, err := contracts.NextProcessID(contracts.AccountAddress())
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to get next process ID: %w", err)
	}

	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewEncryptionKeysEndpoint)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process: %w", err)
	}
	if code != http.StatusOK {
		return types.ProcessID{}, nil, fmt.Errorf("unexpected status code creating process: %d, body: %s", code, string(body))
	}

	var resp types.ProcessEncryptionKeysResponse
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to decode process response: %w", err)
	}
	if resp.EncryptionPubKey[0] == nil || resp.EncryptionPubKey[1] == nil {
		return types.ProcessID{}, nil, fmt.Errorf("encryption public key is nil")
	}

	return processID, &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}, nil
}

func NewProcessOnChain(
	contracts *web3.Contracts,
	censusOrigin types.CensusOrigin,
	censusURI string,
	censusRoot []byte,
	ballotMode spec.BallotMode,
	encryptionKey *types.EncryptionKey,
	numVoters int,
	duration ...time.Duration,
) (types.ProcessID, error) {
	finalDuration := time.Hour
	if len(duration) > 0 {
		finalDuration = duration[0]
	}

	pid, txHash, err := contracts.CreateProcess(&types.Process{
		Status:         types.ProcessStatusReady,
		OrganizationID: contracts.AccountAddress(),
		EncryptionKey:  encryptionKey,
		StartTime:      time.Now().Add(1 * time.Minute),
		Duration:       finalDuration,
		MaxVoters:      types.NewInt(numVoters),
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     ballotMode,
		Census: &types.Census{
			CensusRoot:   censusRoot,
			CensusURI:    censusURI,
			CensusOrigin: censusOrigin,
		},
	})
	if err != nil {
		return types.ProcessID{}, fmt.Errorf("failed to create process: %w", err)
	}
	return pid, contracts.WaitTxByHash(*txHash, time.Second*15)
}

func UpdateMaxVotersOnChain(
	contracts *web3.Contracts,
	pid types.ProcessID,
	numVoters int,
) error {
	currentProcess, err := contracts.Process(pid)
	if err != nil {
		return fmt.Errorf("failed to get current process: %w", err)
	}
	currentMaxVoters := currentProcess.MaxVoters.MathBigInt().Int64()
	if numVoters < int(currentMaxVoters) {
		return fmt.Errorf("new max voters (%d) is less than current max voters (%d)", numVoters, currentMaxVoters)
	}
	txHash, err := contracts.SetProcessMaxVoters(pid, types.NewInt(numVoters))
	if err != nil {
		return fmt.Errorf("failed to set process max voters: %w", err)
	}
	return contracts.WaitTxByHash(*txHash, time.Second*15)
}

func FetchProcessVotersCountOnChain(contracts *web3.Contracts, pid types.ProcessID) (int, error) {
	process, err := contracts.Process(pid)
	if err != nil {
		return 0, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.VotersCount == nil {
		return 0, nil
	}
	return int(process.VotersCount.MathBigInt().Int64()), nil
}

func FetchProcessOnChainOverwrittenVotesCount(contracts *web3.Contracts, pid types.ProcessID) (int, error) {
	process, err := contracts.Process(pid)
	if err != nil {
		return 0, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.OverwrittenVotesCount == nil {
		return 0, nil
	}
	return int(process.OverwrittenVotesCount.MathBigInt().Int64()), nil
}

func FinishProcessOnChain(contracts *web3.Contracts, pid types.ProcessID) error {
	txHash, err := contracts.SetProcessStatus(pid, types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to set process status: %w", err)
	}
	if txHash == nil {
		return fmt.Errorf("transaction hash is nil")
	}
	if err = contracts.WaitTxByHash(*txHash, time.Second*30); err != nil {
		return fmt.Errorf("failed to wait for transaction: %w", err)
	}
	return nil
}
