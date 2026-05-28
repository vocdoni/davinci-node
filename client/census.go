package client

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/client/census3"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/types"
)

func (c *Client) CreateCensus(processConfig *ProcessConfig) (*types.Census, error) {
	// Validate voters config
	if !processConfig.VotersConfig.Valid(CreateProcessAction) {
		return nil, fmt.Errorf("invalid config for create process action")
	}

	// If no voters info is provided, generate random ones
	if len(processConfig.VotersConfig.VotersInfo) == 0 {
		var err error
		processConfig.VotersConfig.VotersInfo, err = processConfig.VotersConfig.RandomVotersInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to generate random voters info: %w", err)
		}
	}

	// Check if CSP config is valid
	if processConfig.CSPConfig.Valid() {
		eddsaCSP, err := csp.New(processConfig.CSPConfig.Origin(), processConfig.CSPConfig.Seed)
		if err != nil {
			return nil, fmt.Errorf("failed to create CSP: %w", err)
		}
		root := eddsaCSP.CensusRoot()
		if root == nil {
			return nil, fmt.Errorf("csp census root is nil")
		}
		return &types.Census{
			CensusOrigin: processConfig.CSPConfig.Origin(),
			CensusRoot:   root.Root,
			CensusURI:    "http://client.csp",
		}, nil
	}

	// Convert voters info to participants
	participants := []census3.CensusParticipant{}
	for _, voter := range processConfig.VotersConfig.VotersInfo {
		participants = append(participants, census3.CensusParticipant{
			Key:    voter.Address,
			Weight: voter.Weight,
		})
	}

	// Create a new census in the census3 service
	census, err := c.census3Client.NewCensus(processConfig.CensusConfig.Origin(), participants)
	if err != nil {
		return nil, fmt.Errorf("failed to create census on census3 service: %w", err)
	}
	return census, nil
}

func (c *Client) CreateCensusProof(processConfig *ProcessConfig, address common.Address, weight *types.BigInt) (types.CensusProof, error) {
	if !processConfig.ProcessID.IsValid() {
		return types.CensusProof{}, fmt.Errorf("invalid process id")
	}

	finalWeight := new(types.BigInt).Set(weight)
	if finalWeight == nil {
		if processConfig.VotersConfig.DefaultWeight == nil {
			return types.CensusProof{}, fmt.Errorf("weight and default weight are nil")
		}
		finalWeight = processConfig.VotersConfig.DefaultWeight
	}

	// Check if address is valid
	if (common.Address{}) == address {
		return types.CensusProof{}, fmt.Errorf("invalid address")
	}

	if processConfig.CSPConfig.Valid() {
		eddsaCSP, err := csp.New(processConfig.CSPConfig.Origin(), processConfig.CSPConfig.Seed)
		if err != nil {
			return types.CensusProof{}, fmt.Errorf("failed to create CSP: %w", err)
		}
		cspProof, err := eddsaCSP.GenerateProof(processConfig.ProcessID, address, finalWeight)
		if err != nil {
			return types.CensusProof{}, fmt.Errorf("failed to generate CSP proof: %w", err)
		}
		return *cspProof, nil
	}
	return types.CensusProof{
		Weight: finalWeight,
	}, nil
}
