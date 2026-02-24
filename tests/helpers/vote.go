package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

func NewVote(pid types.ProcessID, bm spec.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int, fields []*types.BigInt) (api.Vote, error) {
	var err error
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	if k == nil {
		k, err = specutil.RandomK()
		if err != nil {
			return api.Vote{}, fmt.Errorf("failed to generate random k: %w", err)
		}
	}
	// set voter weight
	voterWeight := new(types.BigInt).SetInt(testutil.Weight)
	// compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:       address.Bytes(),
		ProcessID:     pid,
		EncryptionKey: []*types.BigInt{encKey.X, encKey.Y},
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    bm,
		Weight:        voterWeight,
		FieldValues:   fields,
	}
	// generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate ballot proof inputs: %w", err)
	}
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to marshal circom inputs: %w", err)
	}
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to compile and generate proof: %w", err)
	}
	// convert the proof to gnark format
	circomProof, _, err := circomgnark.UnmarshalCircom(rawProof, pubInputs)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to unmarshal circom proof: %w", err)
	}
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to sign ECDSA: %w", err)
	}
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		VoteID:           wasmResult.VoteID,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
	}, nil
}

func NewVoteWithRandomFields(pid types.ProcessID, bm spec.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int) (api.Vote, []*types.BigInt, error) {
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.NumFields),
		int(bm.MaxValue),
		int(bm.MinValue),
		bm.UniqueValues)
	// cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
	}
	// create the vote
	vote, err := NewVote(pid, bm, encKey, privKey, k, fields)
	if err != nil {
		return api.Vote{}, nil, err
	}
	// return the vote and the generated fields
	return vote, fields, nil
}

func NewVoteFromNonCensusVoter(pid types.ProcessID, bm spec.BallotMode, encKey *types.EncryptionKey) (api.Vote, error) {
	privKey, err := ethereum.NewSigner()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate signer: %w", err)
	}
	k, err := specutil.RandomK()
	if err != nil {
		return api.Vote{}, fmt.Errorf("failed to generate random k: %w", err)
	}
	vote, _, err := NewVoteWithRandomFields(pid, bm, encKey, privKey, k)
	return vote, err
}

func EnsureVotesStatus(cli *client.HTTPclient, pid types.ProcessID, voteIDs []types.VoteID, expectedStatus string) (bool, []types.VoteID, error) {
	// Check vote status and return whether all votes have the expected status
	allExpectedStatus := true
	failed := []types.VoteID{}

	// Check status for each vote
	for _, voteID := range voteIDs {
		// Construct the status endpoint URL
		statusEndpoint := api.EndpointWithParam(
			api.EndpointWithParam(api.VoteStatusEndpoint,
				api.ProcessURLParam, pid.String()),
			api.VoteIDURLParam, voteID.String())

		// Make the request to get the vote status
		body, statusCode, err := cli.Request("GET", nil, nil, statusEndpoint)
		if err != nil {
			return false, nil, fmt.Errorf("failed to request vote status: %w", err)
		}
		if statusCode != 200 {
			return false, nil, fmt.Errorf("unexpected status code: %d", statusCode)
		}

		// Parse the response body to get the status
		var statusResponse api.VoteStatusResponse
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse)
		if err != nil {
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

func HasAddressAlreadyVoted(cli *client.HTTPclient, pid types.ProcessID, address common.Address) (bool, error) {
	// get participant from the sequencer
	voteByAddressProcessEndpoint := api.EndpointWithParam(api.VoteByAddressEndpoint, api.ProcessURLParam, pid.String())
	voteByAddressEndpoint := api.EndpointWithParam(voteByAddressProcessEndpoint, api.AddressURLParam, address.Hex())
	voteByAddressBody, statusCode, err := cli.Request("GET", nil, nil, voteByAddressEndpoint)
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
