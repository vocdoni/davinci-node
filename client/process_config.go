package client

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/types"
)

// CensusConfig includes the configuration of a census, including its origin
// (in string), the census root, uri and contract address. It can be used to
// define an pre-existing census or to create a new one.
type CensusConfig struct {
	CensusOrigin    string         `json:"censusOrigin"`
	CensusRoot      types.HexBytes `json:"censusRoot"`
	CensusURI       string         `json:"censusURI"`
	ContractAddress types.HexBytes `json:"contractAddress"`
}

// Origin method returns the types.CensusOrigin associated to the string
// defined CensusOrigin of the current CensusConfig.
func (c CensusConfig) Origin() types.CensusOrigin {
	return types.CensusOriginFromString(c.CensusOrigin)
}

// Census method returns the types.Census struct based on the current
// CensusConfig struct values.
func (c CensusConfig) Census() *types.Census {
	return &types.Census{
		CensusOrigin:    types.CensusOriginFromString(c.CensusOrigin),
		CensusRoot:      c.CensusRoot,
		CensusURI:       c.CensusURI,
		ContractAddress: common.BytesToAddress(c.ContractAddress),
	}
}

// Valid method returns if the current CensusConfig is valid or not using the
// Valid method of the resulting types.Census.
func (c CensusConfig) Valid() bool {
	return c.Census().Valid()
}

// CSPConfig struct contains the required information to initialize valid CSP
// signatures and census proofs, including the CSP seed and the census origin
// (as string), that should be a valid one for CSP censuses.
type CSPConfig struct {
	CensusOrigin string         `json:"origin"`
	Seed         types.HexBytes `json:"seed"`
}

// Origin method returns the types.CensusOrigin for the current CSPConfig
// census origin string.
func (c CSPConfig) Origin() types.CensusOrigin {
	return types.CensusOriginFromString(c.CensusOrigin)
}

// Valid method returns if the current CSPConfig is valid or not based on its
// census origin and if the seed is empty or not.
func (c CSPConfig) Valid() bool {
	return c.Origin().IsCSP() && len(c.Seed) > 0
}

// VoterInfo struct contains the required information for a valid voter, to be
// included in a census (address and weight) but also to submit a vote (private
// key and weight).
type VoterInfo struct {
	Address types.HexBytes `json:"address"`
	Weight  *types.BigInt  `json:"weight"`
	PrivKey types.HexBytes `json:"privKey"`
}

// Valid method returns if the current VoterInfo is valid for the action
// provided. For CreateProcessAction it validates that the weight is not nil
// or zero and it has a non-empty address or private key. For the
// SubmitVoteAction it validates that the weight is not nil or zero and it has
// an non-empty private key.
func (v VoterInfo) Valid(action string) bool {
	validWeight := v.Weight != nil && !v.Weight.LessThanOrEqual(types.NewInt(0))
	validIdentity := common.IsHexAddress(v.Address.String()) || len(v.PrivKey) > 0

	switch action {
	case CreateProcessAction:
		return validIdentity && validWeight
	case SubmitVoteAction:
		return len(v.PrivKey) > 0 && validWeight
	}
	return true
}

// VotersConfig struct contains the required information about voters for
// both actions: create a process and its census, and submit their votes.
// It includes the number of voters and the default weight if the voters
// information should be generated, or an slice of VoterInfo if they are
// already defined.
type VotersConfig struct {
	VotersInfo    []VoterInfo   `json:"votersInfo"`
	NumVoters     int           `json:"numVoters"`
	DefaultWeight *types.BigInt `json:"defaultWeight"`
}

// Valid method returins if the current VotersConfig is valid for the action
// provided. For CreateProcessAction it checks that all the voters information
// is valid or if the required information to generate it is valid (num of
// voters and default weight). For the SubmitVoteAction it checks that all the
// voters information is valid.
func (v VotersConfig) Valid(action string) bool {
	allVotersValid := true
	for _, voter := range v.VotersInfo {
		allVotersValid = allVotersValid && voter.Valid(action)
	}
	switch action {
	case CreateProcessAction:
		return allVotersValid || (v.NumVoters > 0 && v.DefaultWeight != nil)
	case SubmitVoteAction:
		return allVotersValid
	}
	return true
}

// RandomVotersInfo generates a slice of VoterInfo structs based on the number
// of voters and the default weight of the current VotersConfig.
func (v VotersConfig) RandomVotersInfo() ([]VoterInfo, error) {
	votersInfo := make([]VoterInfo, 0, v.NumVoters)
	for range v.NumVoters {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, fmt.Errorf("failed to generate signer: %w", err)
		}
		votersInfo = append(votersInfo, VoterInfo{
			Address: signer.Address().Bytes(),
			Weight:  v.DefaultWeight,
			PrivKey: signer.HexPrivateKey(),
		})
	}
	return votersInfo, nil
}

// Signers method returns the signers of the voters info of the current
// VotersConfig.
func (v VotersConfig) Signers() ([]*ethereum.Signer, error) {
	if len(v.VotersInfo) == 0 {
		return nil, fmt.Errorf("no voters info provided")
	}
	signers := make([]*ethereum.Signer, 0, len(v.VotersInfo))
	for _, voter := range v.VotersInfo {
		signner, err := ethereum.NewSignerFromHex(voter.PrivKey.Hex())
		if err != nil {
			return nil, fmt.Errorf("failed to create signer: %w", err)
		}
		signers = append(signers, signner)
	}
	return signers, nil
}

// ProcessConfig struct contains all required information to interact with a
// process (create it, submit votes to it or stop it and get its results).
// It includes its:
//   - ProcessID
//   - The chainID of the network where it lives
//   - CensusConfig
//   - CSPConfig
//   - VotersConfig
//   - BallotMode
//   - Metadata
//   - The maximun number of voters that it accepts
//   - The start time of the process
//   - The duration of the process
//
// Not all this information are required for any of the available actions.
type ProcessConfig struct {
	ProcessID    types.ProcessID `json:"processID"`
	ChainID      uint64          `json:"chainID"`
	CensusConfig CensusConfig    `json:"censusConfig"`
	CSPConfig    CSPConfig       `json:"cspConfig"`
	VotersConfig VotersConfig    `json:"votersConfig"`
	BallotMode   spec.BallotMode `json:"ballotMode"`
	Metadata     types.Metadata  `json:"metadata"`
	MaxVoters    int             `json:"maxVoters"`
	StartTime    string          `json:"startTime"`
	Duration     string          `json:"duration"`
}

// Valid method returns if the current ProcessConfig is valid or not for the
// action provided.
//   - For the CreateProcessAction it requires a valid CensusConfig or a valid
//     VotersConfig, a valid BallotMode and non-zero MaxVoters.
//   - For SubmitVoteAction it requires a valid ProcessID and VotersInfo.
//   - For StopProcessAction it only requires a valid ProcessID.
func (p ProcessConfig) Valid(action string) bool {
	switch action {
	case CreateProcessAction:
		return (p.CensusConfig.Valid() || p.VotersConfig.Valid(action)) &&
			p.BallotMode.Validate() == nil && p.MaxVoters > 0
	case SubmitVoteAction:
		return p.ProcessID.IsValid() && p.VotersConfig.Valid(action)
	case StopProcessAction:
		return p.ProcessID.IsValid()
	}
	return true
}

// Load method reads the JSOn file in the path provided and parses it as a
// ProcessConfig struct.
func (p *ProcessConfig) Load(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("error opening process config file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Warnf("error closing process config file: %v", err)
		}
	}()

	if err := json.NewDecoder(file).Decode(p); err != nil {
		return fmt.Errorf("error decoding process config file: %w", err)
	}
	return nil
}
