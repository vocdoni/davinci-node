package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

var (
	// VoteIDStatuses strings constants
	VoteIDStatusPending    = storage.VoteIDStatusName(storage.VoteIDStatusPending)
	VoteIDStatusVerified   = storage.VoteIDStatusName(storage.VoteIDStatusVerified)
	VoteIDStatusAggregated = storage.VoteIDStatusName(storage.VoteIDStatusAggregated)
	VoteIDStatusProcessed  = storage.VoteIDStatusName(storage.VoteIDStatusProcessed)
	VoteIDStatusDone       = storage.VoteIDStatusName(storage.VoteIDStatusDone)
	VoteIDStatusError      = storage.VoteIDStatusName(storage.VoteIDStatusError)
	VoteIDStatusTimeout    = storage.VoteIDStatusName(storage.VoteIDStatusTimeout)
	VoteIDStatusSettled    = storage.VoteIDStatusName(storage.VoteIDStatusSettled)
)

// VoterWeight retrieves the voter weight for a given process ID and address
// from the sequencer. It makes a request to the census participant endpoint
// of the sequencer API and extracts the weight from the response. If any
// error occurs during the request or unmarshalling, it will be returned.
func (c *Client) VoterWeight(pid types.ProcessID, addr common.Address) (*types.BigInt, error) {
	participantEndpoint := api.EndpointWithParam(api.CensusParticipantEndpoint, api.ProcessURLParam, pid.String())
	participantEndpoint = api.EndpointWithParam(participantEndpoint, api.AddressURLParam, addr.Hex())

	participantResponse, status, err := c.httpClient.Request(http.MethodGet, nil, nil, participantEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get participant info from sequencer: %v", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("failed to get participant info from sequencer, status code: %d", status)
	}
	var participantInfo api.CensusParticipant
	if err := json.Unmarshal(participantResponse, &participantInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal participant info: %v", err)
	}
	return participantInfo.Weight, nil
}

// CreateRandomVotes method creates a slice of random votes for the process
// configuration provided. It returns the created votes and the fields that
// they contains.
func (c *Client) CreateRandomVotes(process *ProcessConfig) ([]api.Vote, [][]*types.BigInt, error) {
	if !process.Valid(SubmitVoteAction) {
		return nil, nil, fmt.Errorf("invalid process config: %v", process)
	}

	// Get the signers from the process config
	signers, err := process.VotersConfig.Signers()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get signers: %v", err)
	}

	// Fetch the encryption key for the process
	encKey, err := c.EncryptionKeys(process.ProcessID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get encryption key for process %s: %v", process.ProcessID.String(), err)
	}

	// Generate random votes for each participant
	var votes []api.Vote
	var fields [][]*types.BigInt
	for i, privKey := range signers {
		randFields := RandomBallotFields(process.BallotMode)

		weight := types.NewInt(1)
		if process.VotersConfig.VotersInfo[i].Weight != nil {
			weight = process.VotersConfig.VotersInfo[i].Weight
		}

		vote, err := c.NewVote(process, encKey, privKey, nil, randFields, weight)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create vote: %v", err)
		}
		votes = append(votes, vote)
		fields = append(fields, randFields)
	}

	// Return the votes ready to be sent to the sequencer
	return votes, fields, nil
}

// NewVote method method generates a vote for the voter info given based on
// the provided process configuration. If no voter secret (k) is provided, it
// generates a random one. If no encryption keys or weight is provided, it
// queries they in the sequencer..
func (c *Client) NewVote(
	process *ProcessConfig,
	encKey *types.EncryptionKey,
	privKey *ethereum.Signer,
	k *big.Int,
	fields []*types.BigInt,
	weight *types.BigInt,
) (api.Vote, error) {
	if !process.Valid(SubmitVoteAction) {
		return api.Vote{}, fmt.Errorf("invalid process config: %v", process)
	}

	// Fetch the encryption key for the process if not provided
	var err error
	if encKey == nil {
		if encKey, err = c.EncryptionKeys(process.ProcessID); err != nil {
			return api.Vote{}, fmt.Errorf("failed to get encryption key for process %s: %v", process.ProcessID.String(), err)
		}
	}

	// Get voter address
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	// If the voter secret is not provided, generate a random one
	if k == nil {
		if k, err = specutil.RandomK(); err != nil {
			return api.Vote{}, fmt.Errorf("failed to generate random k: %v", err)
		}
	}

	// Get voter weight if not provided
	if weight == nil {
		if weight, err = c.VoterWeight(process.ProcessID, address); err != nil {
			return api.Vote{}, fmt.Errorf("failed to get voter weight: %v", err)
		}
	}

	// Generate random ballot fields based on the ballot mode if no fields are provided
	if len(fields) == 0 {
		return api.Vote{}, fmt.Errorf("no fields provided")
	}

	// Compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:       address.Bytes(),
		ProcessID:     process.ProcessID,
		EncryptionKey: []*types.BigInt{encKey.X, encKey.Y},
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    process.BallotMode,
		Weight:        weight,
		FieldValues:   fields,
	}

	// Generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate ballot proof inputs: %v", err)
	}

	// Encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to encode circom inputs: %v", err)
	}

	circuit, err := c.ballotproof.RawCircuitDefinition()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to get circuit definition: %v", err)
	}
	pk, err := c.ballotproof.RawProvingKey()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to get proving key: %v", err)
	}

	// Generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotproof.GenerateProof(circuit, pk, encodedCircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate proof: %v", err)
	}

	// Convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to convert proof to gnark format: %v", err)
	}

	// Sign the hash of the circuit inputs
	signature, err := privKey.Sign(crypto.PadToSign(wasmResult.VoteID.Bytes()))
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to sign vote: %v", err)
	}

	// Create the census proof
	censusProof, err := c.CreateCensusProof(process, address, weight)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to create census proof: %v", err)
	}

	// Append the vote to the result slice
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
		VoteID:           wasmResult.VoteID,
		CensusProof:      censusProof,
	}, nil
}

// SubmitVote submits the given votes to the sequencer API. If successful, it
// returns the vote IDs of the votes. If any error occurs it will be returned.
func (c *Client) SubmitVotes(votes ...api.Vote) ([]types.VoteID, error) {
	voteIDs := make([]types.VoteID, 0, len(votes))
	for _, vote := range votes {
		// Make the request to cast the vote
		body, status, err := c.httpClient.Request(http.MethodPost, vote, nil, api.VotesEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to cast vote: %w", err)
		} else if status != http.StatusOK {
			return nil, fmt.Errorf("failed to cast vote (status code %d): %s", status, body)
		}
		voteIDs = append(voteIDs, vote.VoteID)
	}
	return voteIDs, nil
}

// EnsureVotesStatus method checks that every vote behind the vote IDs provided
// have reached the desired status in the given ProcessID.
func (c *Client) EnsureVotesStatus(pid types.ProcessID, voteIDs []types.VoteID, expectedStatus string) (bool, []types.VoteID, error) {
	// Check vote status and return whether all votes have the expected status
	allExpectedStatus := true
	failed := []types.VoteID{}

	// Check status for each vote
	processStatusEndpoint := api.EndpointWithParam(api.VoteStatusEndpoint, api.ProcessURLParam, pid.String())
	for _, voteID := range voteIDs {
		// Construct the status endpoint URL
		statusEndpoint := api.EndpointWithParam(processStatusEndpoint, api.VoteIDURLParam, voteID.String())

		// Make the request to get the vote status
		body, statusCode, err := c.httpClient.Request(http.MethodGet, nil, nil, statusEndpoint)
		if err != nil {
			return false, nil, fmt.Errorf("failed to request vote status: %w", err)
		}
		if statusCode != 200 {
			return false, nil, fmt.Errorf("unexpected status code: %d", statusCode)
		}

		// Parse the response body to get the status
		var statusResponse api.VoteStatusResponse
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse); err != nil {
			return false, nil, fmt.Errorf("failed to decode status response: %w", err)
		}

		// Verify the status is valid
		if statusResponse.Status == "" {
			return false, nil, fmt.Errorf("status is empty")
		}

		// Check if the vote has the expected status
		switch statusResponse.Status {
		case storage.VoteIDStatusName(storage.VoteIDStatusError):
			allExpectedStatus = allExpectedStatus && (expectedStatus == storage.VoteIDStatusName(storage.VoteIDStatusError))
			if expectedStatus != storage.VoteIDStatusName(storage.VoteIDStatusError) {
				failed = append(failed, voteID)
			}
		case expectedStatus:
			allExpectedStatus = allExpectedStatus && true
		default:
			allExpectedStatus = false
		}
	}

	return allExpectedStatus, failed, nil
}

// HasAddressAlreadyVoted method return if the provided address has already
// voted in the process identified by the given ProcessID.
func (c *Client) HasAddressAlreadyVoted(pid types.ProcessID, address common.Address) (bool, error) {
	// get participant from the sequencer
	voteByAddressProcessEndpoint := api.EndpointWithParam(api.VoteByAddressEndpoint, api.ProcessURLParam, pid.String())
	voteByAddressEndpoint := api.EndpointWithParam(voteByAddressProcessEndpoint, api.AddressURLParam, address.Hex())
	voteByAddressBody, statusCode, err := c.httpClient.Request("GET", nil, nil, voteByAddressEndpoint)
	if err != nil {
		return false, fmt.Errorf("failed to request participant: %w", err)
	}
	if statusCode != 200 {
		return false, fmt.Errorf("unexpected status code: %d: %s", statusCode, string(voteByAddressBody))
	}
	var voteByAddressResponse *elgamal.Ballot
	err = json.NewDecoder(bytes.NewReader(voteByAddressBody)).Decode(&voteByAddressResponse)
	if err != nil {
		return false, fmt.Errorf("failed to decode already voted response: %w", err)
	}
	return voteByAddressResponse != nil, nil
}
