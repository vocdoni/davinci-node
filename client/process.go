package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/types"
)

// CreateEncryptionKeys method returns a new types.EncryptionKey from the
// sequencer to be used during the process creation.
func (c *Client) CreateEncryptionKeys() (*types.EncryptionKey, error) {
	// Make the request to create the encryption keys
	body, code, err := c.httpClient.Request(http.MethodPost, nil, nil, api.NewEncryptionKeysEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create process: %v", err)
	} else if code != http.StatusOK {
		return nil, fmt.Errorf("failed to create process, status code: %d", code)
	}
	// Decode process response
	var resp types.ProcessEncryptionKeysResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode process response: %v", err)
	}
	return &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}, nil
}

// OnChainProcess method returns the process information that is in the
// contract.
func (c *Client) OnChainProcess(pid types.ProcessID) (*types.Process, error) {
	if !pid.IsValid() {
		return nil, fmt.Errorf("invalid process id")
	}
	runtime, err := c.runtimes.RuntimeForProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime: %w", err)
	}
	return runtime.Contracts.Process(pid)
}

// EncryptionKeys retrieves the encryption key for the given process ID from
// the sequencer. It request the process information to the sequencer API and
// extracts the encryption key from the response. If any error occurs during
// the request or unmarshalling, it will be returned.
// The encryption key can be used to encrypt ballots for the specified process.
func (c *Client) EncryptionKeys(pid types.ProcessID) (*types.EncryptionKey, error) {
	// Get the encryption keys from the sequencer
	processEndpoint := api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, pid.String())
	processResponse, status, err := c.httpClient.Request(http.MethodGet, nil, nil, processEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get process info from sequencer: %v", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("failed to get process info from sequencer, status code: %d", status)
	}
	var processInfo api.ProcessResponse
	if err := json.Unmarshal(processResponse, &processInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal process info: %v", err)
	}
	return processInfo.EncryptionKey, nil
}

// CreateMetadata method publishes the metadata provided using the sequencer
// API. It returins the metadata hash, its URL in the sequencer and an error
// if it occurs.
func (c *Client) CreateMetadata(metadata types.Metadata) (types.HexBytes, string, error) {
	// Publish the metadata in the sequencer
	body, code, err := c.httpClient.Request(http.MethodPost, metadata, nil, api.MetadataSetEndpoint)
	if err != nil {
		return types.HexBytes{}, "", fmt.Errorf("failed to set metadata: %v", err)
	} else if code != http.StatusOK {
		return types.HexBytes{}, "", fmt.Errorf("failed to set metadata, status code: %d", code)
	}
	// Decode the metadata hash
	var resp api.SetMetadataResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil {
		return types.HexBytes{}, "", fmt.Errorf("failed to decode set metadata response: %v", err)
	}
	// Compose the metadata URI
	uri, err := url.JoinPath(c.config.SequencerEndpoint, api.EndpointWithParam(api.MetadataGetEndpoint, api.MetadataHashParam, resp.Hash.String()))
	if err != nil {
		return types.HexBytes{}, "", fmt.Errorf("failed to join url: %v", err)
	}
	return resp.Hash, uri, nil
}

// CreateProcess creates a new process with the given configuration. It is
// required, it creates the census and publish the metadata.
func (c *Client) CreateProcess(config *ProcessConfig) (types.ProcessID, error) {
	// Check if config is valid for create process action
	if !config.Valid(CreateProcessAction) {
		return types.ProcessID{}, fmt.Errorf("invalid config for create process action")
	}
	// Get runtime for the chainID
	runtime, err := c.runtimes.RuntimeForChainID(config.ChainID)
	if err != nil {
		return types.ProcessID{}, fmt.Errorf("failed to get runtime: %v", err)
	}
	// Check if valid census is provided or create a new one with the given config
	var census *types.Census
	if !config.CensusConfig.Valid() {
		if census, err = c.CreateCensus(config); err != nil {
			return types.ProcessID{}, fmt.Errorf("failed to create census: %v", err)
		}
	}
	// Create encryption keys
	encryptionKeys, err := c.CreateEncryptionKeys()
	if err != nil {
		return types.ProcessID{}, fmt.Errorf("failed to create encryption keys: %v", err)
	}
	// Check process start time and duration
	startTime := time.Now().Add(1 * time.Minute)
	if config.StartTime != "" {
		if startTime, err = time.Parse(time.RFC3339, config.StartTime); err != nil {
			return types.ProcessID{}, fmt.Errorf("failed to parse start time: %v", err)
		}
		if startTime.IsZero() || startTime.Before(time.Now()) {
			return types.ProcessID{}, fmt.Errorf("start time must be in the future")
		}
	}
	duration := MinProcessDuration
	if config.Duration != "" {
		if duration, err = time.ParseDuration(config.Duration); err != nil {
			return types.ProcessID{}, fmt.Errorf("failed to parse duration: %v", err)
		}
		if duration < MinProcessDuration {
			return types.ProcessID{}, fmt.Errorf("duration must be at least %v", MinProcessDuration)
		}
	}
	// If metadata is provided, create it in the sequencer and get the metadata URI
	var metadataURI string
	if !config.Metadata.Empty() {
		if _, metadataURI, err = c.CreateMetadata(config.Metadata); err != nil {
			return types.ProcessID{}, fmt.Errorf("failed to create metadata: %v", err)
		}
	}
	newProcess := &types.Process{
		OrganizationID: runtime.Contracts.AccountAddress(),
		EncryptionKey:  encryptionKeys,
		StartTime:      startTime,
		Duration:       duration,
		MetadataURI:    metadataURI,
		BallotMode:     config.BallotMode,
		MaxVoters:      types.NewInt(config.MaxVoters),
		Census:         census,
	}
	// Create process in the contracts
	pid, txHash, err := runtime.Contracts.CreateProcess(newProcess)
	if err != nil {
		return types.ProcessID{}, fmt.Errorf("failed to create process in contracts: %v", err)
	}

	// Wait for the process creation transaction to be mined
	if err = runtime.Contracts.WaitTxByHash(*txHash, time.Minute*2); err != nil {
		return types.ProcessID{}, fmt.Errorf("failed to wait for process creation tx: %v", err)
	}

	// Wait for the process to be registered in the sequencer
	processCtx, cancel := context.WithTimeout(c.ctx, 2*time.Minute)
	defer cancel()
	for processReady := false; !processReady; {
		select {
		case <-time.After(time.Second * 5):
			pBytes, status, err := c.httpClient.Request(http.MethodGet, nil, nil, api.EndpointWithParam(api.ProcessEndpoint, api.ProcessURLParam, pid.String()))
			if err == nil && status == http.StatusOK {
				proc := &api.ProcessResponse{}
				if err := json.Unmarshal(pBytes, proc); err != nil {
					return types.ProcessID{}, fmt.Errorf("failed to unmarshal process response: %v", err)
				}
				processReady = proc.IsAcceptingVotes
			}
		case <-processCtx.Done():
			return types.ProcessID{}, fmt.Errorf("process creation timeout: %v", processCtx.Err())
		}
	}
	return pid, nil
}

// StopProcess stops the process with the given process ID by setting its
// status to Ended in the smart contracts. It sends a transaction to update
// the process status and waits for the transaction to be mined. If any error
// occurs during the process, it will be returned.
func (c *Client) StopProcess(pid types.ProcessID) error {
	runtime, err := c.runtimes.RuntimeForProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to get runtime: %w", err)
	}
	tx, err := runtime.Contracts.SetProcessStatus(pid, types.ProcessStatusEnded)
	if err != nil {
		return fmt.Errorf("failed to stop process in contracts: %w", err)
	}
	return runtime.Contracts.WaitTxByHash(*tx, time.Minute)
}

// UpdateCensus method updates the process census identified by the provided
// ProcessID with the given census. It does not create the census, it should
// be created before use this method.
func (c *Client) UpdateCensus(processID types.ProcessID, census types.Census) error {
	if !census.Valid() {
		return fmt.Errorf("invalid census")
	}

	if !processID.IsValid() {
		return fmt.Errorf("invalid process id")
	}

	runtime, err := c.runtimes.RuntimeForProcess(processID)
	if err != nil {
		return fmt.Errorf("failed to get runtime: %w", err)
	}

	txHash, err := runtime.Contracts.SetProcessCensus(processID, census)
	if err != nil {
		return fmt.Errorf("failed to update process census: %w", err)
	}
	return runtime.Contracts.WaitTxByHash(*txHash, time.Second*15)
}

// UpdateMaxVoters method updates the max voters of the process identified by
// the ProcessID provided.
func (c *Client) UpdateMaxVoters(processID types.ProcessID, maxVoters int) error {
	if !processID.IsValid() {
		return fmt.Errorf("invalid process id")
	}

	if maxVoters <= 0 {
		return fmt.Errorf("max voters must be greater than 0")
	}

	runtime, err := c.runtimes.RuntimeForProcess(processID)
	if err != nil {
		return fmt.Errorf("failed to get runtime: %w", err)
	}

	currentProcess, err := runtime.Contracts.Process(processID)
	if err != nil {
		return fmt.Errorf("failed to get current process: %w", err)
	}
	currentMaxVoters := currentProcess.MaxVoters.MathBigInt().Int64()
	if maxVoters < int(currentMaxVoters) {
		return fmt.Errorf("new max voters (%d) is less than current max voters (%d)", maxVoters, currentMaxVoters)
	}
	txHash, err := runtime.Contracts.SetProcessMaxVoters(processID, types.NewInt(maxVoters))
	if err != nil {
		return fmt.Errorf("failed to set process max voters: %w", err)
	}
	return runtime.Contracts.WaitTxByHash(*txHash, time.Second*15)
}

// OnchainProcessVotersCount method returns the VotersCount attribute
// of the process identified by the ProcessID provided from the onchain
// data.
func (c *Client) OnchainProcessVotersCount(processID types.ProcessID) (int, error) {
	if !processID.IsValid() {
		return 0, fmt.Errorf("invalid process id")
	}

	runtime, err := c.runtimes.RuntimeForProcess(processID)
	if err != nil {
		return 0, fmt.Errorf("failed to get runtime: %w", err)
	}

	process, err := runtime.Contracts.Process(processID)
	if err != nil {
		return 0, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.VotersCount == nil {
		return 0, nil
	}
	return int(process.VotersCount.MathBigInt().Int64()), nil
}

// OnchainProcessOverwrittenVotesCount method returns the OverwrittenVotesCount
// attribute of the process identified by the ProcessID provided from the
// onchain data.
func (c *Client) OnchainProcessOverwrittenVotesCount(processID types.ProcessID) (int, error) {
	if !processID.IsValid() {
		return 0, fmt.Errorf("invalid process id")
	}

	runtime, err := c.runtimes.RuntimeForProcess(processID)
	if err != nil {
		return 0, fmt.Errorf("failed to get runtime: %w", err)
	}

	process, err := runtime.Contracts.Process(processID)
	if err != nil {
		return 0, fmt.Errorf("failed to get process: %w", err)
	}
	if process == nil || process.OverwrittenVotesCount == nil {
		return 0, nil
	}
	return int(process.OverwrittenVotesCount.MathBigInt().Int64()), nil
}
