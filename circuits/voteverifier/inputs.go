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
	CensusOrigin    types.CensusOrigin
	BallotMode      circuits.BallotMode[*big.Int]
	EncryptionKey   circuits.EncryptionKey[*big.Int]
	Address         *big.Int
	VoteID          types.HexBytes
	UserWeight      *big.Int
	EncryptedBallot *elgamal.Ballot
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
	vi.UserWeight = b.VoterWeight
	vi.EncryptedBallot = b.EncryptedBallot
	return nil
}

func (vi *VoteVerifierInputs) Serialize() []*big.Int {
	ballotMode := vi.BallotMode.Serialize()
	encryptionKey := vi.EncryptionKey.Serialize()
	encryptedBallot := vi.EncryptedBallot.BigInts()
	inputs := make([]*big.Int, 0, 5+len(ballotMode)+len(encryptionKey)+len(encryptedBallot))
	inputs = append(inputs, vi.ProcessID)
	inputs = append(inputs, vi.CensusOrigin.BigInt().MathBigInt())
	inputs = append(inputs, ballotMode...)
	inputs = append(inputs, encryptionKey...)
	inputs = append(inputs, vi.Address)
	inputs = append(inputs, vi.VoteID.BigInt().MathBigInt())
	inputs = append(inputs, vi.UserWeight)
	inputs = append(inputs, encryptedBallot...)
	return inputs
}

func (vi *VoteVerifierInputs) InputsHash() (*big.Int, error) {
	inputHash, err := mimc7.Hash(vi.Serialize(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to hash inputs: %w", err)
	}
	return inputHash, nil
}
