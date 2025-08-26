package voteverifier

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/storage/census"
	"github.com/vocdoni/davinci-node/types"
)

type VoteVerifierInputs struct {
	ProcessID       *big.Int
	BallotMode      circuits.BallotMode[*big.Int]
	EncryptionKey   circuits.EncryptionKey[*big.Int]
	Address         *big.Int
	VoteID          types.HexBytes
	EncryptedBallot *elgamal.Ballot
	CensusRoot      *big.Int
	CensusOrigin    types.CensusOrigin
	CSPProof        csp.CSPProof
	CensusSiblings  [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]
}

func (vi *VoteVerifierInputs) FromProcessBallot(process *types.Process, b *storage.Ballot) error {
	if vi == nil {
		return fmt.Errorf("vote verifier inputs cannot be nil")
	}
	if b == nil {
		return fmt.Errorf("ballot cannot be nil")
	}

	vi.ProcessID = crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), b.ProcessID.BigInt().MathBigInt())
	vi.CensusOrigin = process.Census.CensusOrigin
	vi.BallotMode = circuits.BallotModeToCircuit(process.BallotMode)
	vi.EncryptionKey = circuits.EncryptionKeyToCircuit(*process.EncryptionKey)
	vi.Address = b.Address
	vi.VoteID = b.VoteID
	vi.EncryptedBallot = b.EncryptedBallot
	censusRoot, err := process.BigCensusRoot()
	if err != nil {
		return fmt.Errorf("failed to get census root: %w", err)
	}
	vi.CensusRoot = censusRoot.MathBigInt()

	switch vi.CensusOrigin {
	case types.CensusOriginMerkleTree:
		// For Merkle Tree origin, we need to convert the siblings to
		// emulated elements and set the CSPProof to a dummy value
		censusSiblings, err := census.BigIntSiblings(b.CensusProof.Siblings)
		if err != nil {
			return fmt.Errorf("failed to unpack census proof siblings: %w", err)
		}
		for i, s := range circuits.BigIntArrayToN(censusSiblings, types.CensusTreeMaxLevels) {
			vi.CensusSiblings[i] = emulated.ValueOf[sw_bn254.ScalarField](s)
		}
		vi.CSPProof = DummyCSPProof()
	case types.CensusOriginCSPEdDSABLS12377:
		// For CSP origin, we need to convert the census proof to a CSPProof
		// and set the siblings to dummy values
		vi.CensusSiblings = DummySiblings()
		cspProof, err := csp.CensusProofToCSPProof(b.CensusProof)
		if err != nil {
			return fmt.Errorf("failed to convert census proof to CSP proof: %w", err)
		}
		vi.CSPProof = *cspProof
	default:
		return fmt.Errorf("unsupported census origin: %s", vi.CensusOrigin)
	}

	return nil
}

func (vi *VoteVerifierInputs) Serialize() []*big.Int {
	inputs := make([]*big.Int, 0, 8+len(vi.EncryptedBallot.BigInts()))
	inputs = append(inputs, vi.ProcessID)
	inputs = append(inputs, vi.CensusOrigin.BigInt().MathBigInt())
	inputs = append(inputs, vi.CensusRoot)
	inputs = append(inputs, vi.BallotMode.Serialize()...)
	inputs = append(inputs, vi.EncryptionKey.Serialize()...)
	inputs = append(inputs, vi.Address)
	inputs = append(inputs, vi.VoteID.BigInt().MathBigInt())
	inputs = append(inputs, vi.EncryptedBallot.BigInts()...)
	return inputs
}

func (vi *VoteVerifierInputs) InputsHash() (*big.Int, error) {
	inputHash, err := mimc7.Hash(vi.Serialize(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to hash inputs: %w", err)
	}
	return inputHash, nil
}

func VoteVerifierInputHash(
	processID *big.Int,
	ballotMode circuits.BallotMode[*big.Int],
	encryptionKey circuits.EncryptionKey[*big.Int],
	address *big.Int,
	voteID types.HexBytes,
	encryptedBallot *elgamal.Ballot,
	censusRoot *big.Int,
	censusOrigin types.CensusOrigin,
) (*big.Int, error) {
	hashInputs := make([]*big.Int, 0, 8+len(encryptedBallot.BigInts()))
	hashInputs = append(hashInputs, processID)
	hashInputs = append(hashInputs, censusOrigin.BigInt().MathBigInt())
	hashInputs = append(hashInputs, censusRoot)
	hashInputs = append(hashInputs, ballotMode.Serialize()...)
	hashInputs = append(hashInputs, encryptionKey.Serialize()...)
	hashInputs = append(hashInputs, address)
	hashInputs = append(hashInputs, voteID.BigInt().MathBigInt())
	hashInputs = append(hashInputs, encryptedBallot.BigInts()...)

	inputHash, err := mimc7.Hash(hashInputs, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to hash inputs: %w", err)
	}
	return inputHash, nil
}
