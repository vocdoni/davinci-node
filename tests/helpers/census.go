package helpers

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	censustest "github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

func IsCSPCensus() bool {
	cspCensusEnvVar := os.Getenv(CSPCensusEnvVarName)
	return strings.ToLower(cspCensusEnvVar) == "true" || cspCensusEnvVar == "1"
}

func CurrentCensusOrigin() types.CensusOrigin {
	if IsCSPCensus() {
		return types.CensusOriginCSPEdDSABabyJubJubV1
	} else {
		return types.CensusOriginMerkleTreeOffchainDynamicV1
	}
}

func WrongCensusOrigin() types.CensusOrigin {
	if IsCSPCensus() {
		return types.CensusOriginMerkleTreeOffchainStaticV1
	} else {
		return types.CensusOriginCSPEdDSABabyJubJubV1
	}
}

func NewCensusWithRandomVoters(ctx context.Context, origin types.CensusOrigin, nVoters int) ([]byte, string, []*ethereum.Signer, error) {
	// Generate random participants
	signers := []*ethereum.Signer{}
	votes := []state.Vote{}
	for range nVoters {
		signer, err := ethereum.NewSigner()
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to generate signer: %w", err)
		}
		signers = append(signers, signer)
		votes = append(votes, state.Vote{
			Address: signer.Address().Big(),
			Weight:  big.NewInt(testutil.Weight),
		})
	}

	if origin.IsCSP() {
		eddsaCSP, err := csp.New(origin, []byte(LocalCSPSeed))
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to create CSP: %w", err)
		}
		root := eddsaCSP.CensusRoot()
		if root == nil {
			return nil, "", nil, fmt.Errorf("census root is nil")
		}
		return root.Root, "http://myowncsp.test", signers, nil
	} else {
		censusRoot, censusURI, err := censustest.NewCensus3MerkleTreeForTest(ctx, origin, votes, DefaultCensus3URL)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to serve census merkle tree: %w", err)
		}
		return censusRoot.Bytes(), censusURI, signers, nil
	}
}

func NewCensusWithVoters(ctx context.Context, origin types.CensusOrigin, signers ...*ethereum.Signer) ([]byte, string, []*ethereum.Signer, error) {
	// Generate random participants
	votes := []state.Vote{}
	for _, signer := range signers {
		votes = append(votes, state.Vote{
			Address: signer.Address().Big(),
			Weight:  big.NewInt(testutil.Weight),
		})
	}

	if origin.IsCSP() {
		eddsaCSP, err := csp.New(origin, []byte(LocalCSPSeed))
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to create CSP: %w", err)
		}
		root := eddsaCSP.CensusRoot()
		if root == nil {
			return nil, "", nil, fmt.Errorf("census root is nil")
		}
		return root.Root, "http://myowncsp.test", signers, nil
	} else {
		censusRoot, censusURI, err := censustest.NewCensus3MerkleTreeForTest(ctx, origin, votes, DefaultCensus3URL)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to serve census merkle tree: %w", err)
		}
		return censusRoot.Bytes(), censusURI, signers, nil
	}
}

func CreateCensusProof(origin types.CensusOrigin, pid types.ProcessID, address common.Address) (types.CensusProof, error) {
	if origin.IsCSP() {
		weight := new(types.BigInt).SetUint64(testutil.Weight)
		eddsaCSP, err := csp.New(types.CensusOriginCSPEdDSABabyJubJubV1, []byte(LocalCSPSeed))
		if err != nil {
			return types.CensusProof{}, fmt.Errorf("failed to create CSP: %w", err)
		}
		cspProof, err := eddsaCSP.GenerateProof(pid, address, weight)
		if err != nil {
			return types.CensusProof{}, fmt.Errorf("failed to generate CSP proof: %w", err)
		}
		return *cspProof, nil
	}
	return types.CensusProof{}, nil
}

func UpdateCensusOnChain(
	contracts *web3.Contracts,
	pid types.ProcessID,
	census types.Census,
) error {
	txHash, err := contracts.SetProcessCensus(pid, census)
	if err != nil {
		return fmt.Errorf("failed to update process census: %w", err)
	}
	return contracts.WaitTxByHash(*txHash, time.Second*15)
}
