package voteverifier

import (
	"fmt"
	"math/big"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

type VoteVerifierInputs struct {
	ProcessID       *big.Int
	BallotMode      circuits.BallotMode[*big.Int]
	EncryptionKey   circuits.EncryptionKey[*big.Int]
	Address         *big.Int
	VoteID          types.HexBytes
	EncryptedBallot *elgamal.Ballot
	CensusOrigin    types.CensusOrigin
	CSPProof        csp.CSPProof
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
	return nil
}

func (vi *VoteVerifierInputs) Serialize() []*big.Int {
	inputs := make([]*big.Int, 0, 8+len(vi.EncryptedBallot.BigInts()))
	inputs = append(inputs, vi.ProcessID)
	inputs = append(inputs, vi.CensusOrigin.BigInt().MathBigInt())
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
	censusOrigin types.CensusOrigin,
) (*big.Int, error) {
	hashInputs := make([]*big.Int, 0, 8+len(encryptedBallot.BigInts()))
	hashInputs = append(hashInputs, processID)
	hashInputs = append(hashInputs, censusOrigin.BigInt().MathBigInt())
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
