package tests

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/api/client"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballotprooftest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func createCensus(c *qt.C, cli *client.HTTPclient, size int) ([]byte, []*api.CensusParticipant, []*ethereum.Signer) {
	// Create a new census
	body, code, err := cli.Request(http.MethodPost, nil, nil, api.NewCensusEndpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK)

	var resp api.NewCensus
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
	c.Assert(err, qt.IsNil)

	// Generate random participants
	signers := []*ethereum.Signer{}
	censusParticipants := api.CensusParticipants{Participants: []*api.CensusParticipant{}}
	for range size {
		signer, err := ethereum.NewSigner()
		if err != nil {
			c.Fatalf("failed to generate signer: %v", err)
		}
		censusParticipants.Participants = append(censusParticipants.Participants, &api.CensusParticipant{
			Key:    signer.Address().Bytes(),
			Weight: new(types.BigInt).SetUint64(circuits.MockWeight),
		})
		signers = append(signers, signer)
	}

	// Add participants to census
	addEnpoint := api.EndpointWithParam(api.AddCensusParticipantsEndpoint, api.CensusURLParam, resp.Census.String())
	_, code, err = cli.Request(http.MethodPost, censusParticipants, nil, addEnpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK)

	// Get census root
	getRootEnpoint := api.EndpointWithParam(api.GetCensusRootEndpoint, api.CensusURLParam, resp.Census.String())
	body, code, err = cli.Request(http.MethodGet, nil, nil, getRootEnpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK)

	var rootResp api.CensusRoot
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&rootResp)
	c.Assert(err, qt.IsNil)

	return rootResp.Root, censusParticipants.Participants, signers
}

func generateCensusProof(c *qt.C, cli *client.HTTPclient, root []byte, key []byte) *types.CensusProof {
	// Get proof for the key
	getProofEnpoint := api.EndpointWithParam(api.GetCensusProofEndpoint, api.CensusURLParam, hex.EncodeToString(root))
	body, code, err := cli.Request(http.MethodGet, nil, []string{"key", hex.EncodeToString(key)}, getProofEnpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK)

	var proof types.CensusProof
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&proof)
	c.Assert(err, qt.IsNil)

	return &proof
}

func createOrganization(c *qt.C, contracts *web3.Contracts) common.Address {
	orgAddr := contracts.AccountAddress()
	txHash, err := contracts.CreateOrganization(orgAddr, &types.OrganizationInfo{
		Name:        fmt.Sprintf("Vocdoni test %x", orgAddr[:4]),
		MetadataURI: "https://vocdoni.io",
	})
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to create organization: %v", err))

	err = contracts.WaitTx(txHash, time.Second*30)
	c.Assert(err, qt.IsNil, qt.Commentf("Failed to wait for organization creation transaction: %v", err))
	return orgAddr
}

func createProcessInSequencer(c *qt.C, contracts *web3.Contracts, cli *client.HTTPclient,
	censusRoot []byte, ballotMode *types.BallotMode,
) (*types.ProcessID, *types.EncryptionKey, *types.HexBytes) {
	// Geth the next process ID from the contracts
	processID, err := contracts.NextProcessID(contracts.AccountAddress())
	c.Assert(err, qt.IsNil)

	// Sign the process creation request
	signature, err := contracts.SignMessage(fmt.Appendf(nil, types.NewProcessMessageToSign, processID.String()))
	c.Assert(err, qt.IsNil)

	process := &types.ProcessSetup{
		ProcessID:  processID.Marshal(),
		CensusRoot: censusRoot,
		BallotMode: ballotMode,
		Signature:  signature,
	}

	body, code, err := cli.Request(http.MethodPost, process, nil, api.ProcessesEndpoint)
	c.Assert(err, qt.IsNil)
	c.Assert(code, qt.Equals, http.StatusOK, qt.Commentf("response body %s", string(body)))

	var resp types.ProcessSetupResponse
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&resp)
	c.Assert(err, qt.IsNil)
	c.Assert(resp.ProcessID, qt.Not(qt.IsNil))
	c.Assert(resp.EncryptionPubKey[0], qt.Not(qt.IsNil))
	c.Assert(resp.EncryptionPubKey[1], qt.Not(qt.IsNil))

	encryptionKeys := &types.EncryptionKey{
		X: resp.EncryptionPubKey[0],
		Y: resp.EncryptionPubKey[1],
	}
	return processID, encryptionKeys, &resp.StateRoot
}

func createProcessInContracts(c *qt.C, contracts *web3.Contracts,
	censusRoot []byte, ballotMode *types.BallotMode, encryptionKey *types.EncryptionKey, stateRoot *types.HexBytes,
) *types.ProcessID {
	pid, txHash, err := contracts.CreateProcess(&types.Process{
		Status:         0,
		OrganizationId: contracts.AccountAddress(),
		EncryptionKey:  encryptionKey,
		StateRoot:      stateRoot.BigInt(),
		StartTime:      time.Now().Add(1 * time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		BallotMode:     ballotMode,
		Census: &types.Census{
			CensusRoot:   censusRoot,
			MaxVotes:     new(types.BigInt).SetUint64(1000),
			CensusURI:    "https://example.com/census",
			CensusOrigin: 0,
		},
	})
	c.Assert(err, qt.IsNil)

	err = contracts.WaitTx(*txHash, time.Second*15)
	c.Assert(err, qt.IsNil)

	return pid
}

func createVote(c *qt.C, pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey, privKey *ethereum.Signer, k *big.Int) (api.Vote, *big.Int) {
	var err error
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	if k == nil {
		k, err = elgamal.RandK()
		c.Assert(err, qt.IsNil)
	}
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.MaxCount),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.ForceUniqueness)
	// cast fields to types.BigInt
	fields := []*types.BigInt{}
	for _, f := range randFields {
		fields = append(fields, (*types.BigInt)(f))
	}
	c.Logf("creating vote for address %s with fields %v", address.Hex(), fields)
	// compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:   address.Bytes(),
		ProcessID: pid.Marshal(),
		EncryptionKey: []*types.BigInt{
			(*types.BigInt)(encKey.X),
			(*types.BigInt)(encKey.Y),
		},
		K:           (*types.BigInt)(k),
		BallotMode:  bm,
		Weight:      (*types.BigInt)(new(big.Int).SetUint64(circuits.MockWeight)),
		FieldValues: fields,
	}
	// generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	c.Assert(err, qt.IsNil)
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	c.Assert(err, qt.IsNil)
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	c.Assert(err, qt.IsNil)
	// convert the proof to gnark format
	circomProof, _, err := circuits.Circom2GnarkProof(rawProof, pubInputs)
	c.Assert(err, qt.IsNil)
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	c.Assert(err, qt.IsNil)
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		VoteID:           wasmResult.VoteID,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
	}, k
}

func createVoteFromInvalidVoter(c *qt.C, pid *types.ProcessID, bm *types.BallotMode, encKey *types.EncryptionKey) api.Vote {
	privKey, err := ethereum.NewSigner()
	if err != nil {
		c.Fatalf("failed to generate signer: %v", err)
	}
	// emulate user inputs
	address := ethcrypto.PubkeyToAddress(privKey.PublicKey)
	k, err := elgamal.RandK()
	c.Assert(err, qt.IsNil)
	// generate random ballot fields
	randFields := ballotprooftest.GenBallotFieldsForTest(
		int(bm.MaxCount),
		int(bm.MaxValue.MathBigInt().Int64()),
		int(bm.MinValue.MathBigInt().Int64()),
		bm.ForceUniqueness)
	// compose wasm inputs
	wasmInputs := &ballotproof.BallotProofInputs{
		Address:       address.Bytes(),
		ProcessID:     pid.Marshal(),
		EncryptionKey: []*types.BigInt{encKey.X, encKey.Y},
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    bm,
		Weight:        new(types.BigInt).SetUint64(circuits.MockWeight),
		FieldValues:   randFields[:],
	}
	// generate the inputs for the ballot proof circuit
	wasmResult, err := ballotproof.GenerateBallotProofInputs(wasmInputs)
	c.Assert(err, qt.IsNil)
	// encode the inputs to json
	encodedCircomInputs, err := json.Marshal(wasmResult.CircomInputs)
	c.Assert(err, qt.IsNil)
	// generate the proof using the circom circuit
	rawProof, pubInputs, err := ballotprooftest.CompileAndGenerateProofForTest(encodedCircomInputs)
	c.Assert(err, qt.IsNil)
	// convert the proof to gnark format
	circomProof, _, err := circuits.Circom2GnarkProof(rawProof, pubInputs)
	c.Assert(err, qt.IsNil)
	// sign the hash of the circuit inputs
	signature, err := ballotprooftest.SignECDSAForTest(privKey, wasmResult.VoteID)
	c.Assert(err, qt.IsNil)
	// return the vote ready to be sent to the sequencer
	return api.Vote{
		ProcessID:        wasmResult.ProcessID,
		Address:          wasmInputs.Address,
		Ballot:           wasmResult.Ballot,
		BallotProof:      circomProof,
		BallotInputsHash: wasmResult.BallotInputsHash,
		Signature:        signature.Bytes(),
		VoteID:           wasmResult.VoteID,
	}
}

func checkVoteStatus(t *testing.T, cli *client.HTTPclient, pid *types.ProcessID, voteIDs []types.HexBytes, expectedStatus string) (bool, []types.HexBytes) {
	c := qt.New(t)
	// Check vote status and return whether all votes have the expected status
	txt := strings.Builder{}
	txt.WriteString(fmt.Sprintf("Vote status (expecting %s): ", expectedStatus))
	allExpectedStatus := true

	failed := []types.HexBytes{}
	// Check status for each vote
	for i, voteID := range voteIDs {
		// Construct the status endpoint URL
		statusEndpoint := api.EndpointWithParam(
			api.EndpointWithParam(api.VoteStatusEndpoint,
				api.ProcessURLParam, pid.String()),
			api.VoteStatusVoteIDParam, voteID.String())

		// Make the request to get the vote status
		body, statusCode, err := cli.Request("GET", nil, nil, statusEndpoint)
		c.Assert(err, qt.IsNil)
		c.Assert(statusCode, qt.Equals, 200)

		// Parse the response body to get the status
		var statusResponse api.VoteStatusResponse
		err = json.NewDecoder(bytes.NewReader(body)).Decode(&statusResponse)
		c.Assert(err, qt.IsNil)

		// Verify the status is valid
		c.Assert(statusResponse.Status, qt.Not(qt.Equals), "")

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
		// Write to the string builder for logging
		txt.WriteString(fmt.Sprintf("#%d:%s ", i, statusResponse.Status))
	}

	// Log the vote status
	t.Log(txt.String())
	return allExpectedStatus, failed
}

func publishedVotes(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) int {
	c := qt.New(t)
	process, err := contracts.Process(pid.Marshal())
	c.Assert(err, qt.IsNil)
	if process == nil || process.VoteCount == nil {
		return 0
	}
	return int(process.VoteCount.MathBigInt().Int64())
}

func publishedOverwriteVotes(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) int {
	c := qt.New(t)
	process, err := contracts.Process(pid.Marshal())
	c.Assert(err, qt.IsNil)
	if process == nil || process.VoteOverwrittenCount == nil {
		return 0
	}
	return int(process.VoteOverwrittenCount.MathBigInt().Int64())
}

func finishProcessOnContract(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) {
	c := qt.New(t)
	txHash, err := contracts.SetProcessStatus(pid.Marshal(), types.ProcessStatusEnded)
	c.Assert(err, qt.IsNil)
	c.Assert(txHash, qt.IsNotNil)
	err = contracts.WaitTx(*txHash, time.Second*30)
	c.Assert(err, qt.IsNil)
	t.Logf("process %s finished successfully", pid.String())
}

func publishedResults(t *testing.T, contracts *web3.Contracts, pid *types.ProcessID) []*types.BigInt {
	c := qt.New(t)
	process, err := contracts.Process(pid.Marshal())
	c.Assert(err, qt.IsNil)
	if process == nil || process.Status != types.ProcessStatusResults || len(process.Result) == 0 {
		return nil
	}
	return process.Result
}
