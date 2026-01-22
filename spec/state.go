package spec

import (
	"fmt"
	"math"
	"math/big"

	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/types/params"
)

const (
	// StateTreeMaxLevels is the maximum number of levels in the state merkle tree.
	StateTreeMaxLevels = 64
)

// state config keys
const (
	StateKeyProcessID     uint64 = 0x00
	StateKeyCensusOrigin  uint64 = 0x01
	StateKeyBallotMode    uint64 = 0x02
	StateKeyEncryptionKey uint64 = 0x03
	StateKeyResultsAdd    uint64 = 0x04
	StateKeyResultsSub    uint64 = 0x05
)

// State Namespaces

const (
	ConfigMin uint64 = 0                                        // 0x0000_0000_0000_0000
	ConfigMax uint64 = 1<<4 - 1                                 // 0x0000_0000_0000_000F
	BallotMin uint64 = ConfigMax + 1                            // 0x0000_0000_0000_0010
	BallotMax uint64 = VoteIDMin - 1                            // 0x7FFF_FFFF_FFFF_FFFF
	VoteIDMin uint64 = (math.MaxUint64 - 1<<VoteIDHashBits) + 1 // 0x8000_0000_0000_0000
	VoteIDMax uint64 = math.MaxUint64                           // 0xFFFF_FFFF_FFFF_FFFF

	VoteIDHashBits uint = 63 // bits

	CensusAddressBitLen uint = 16 // bits

	CensusIndexMax = BallotMax>>CensusAddressBitLen - ConfigMax // 0x0000_7FFF_FFFF_FFF0
)

// PoseidonHash hashes the provided inputs with iden3 Poseidon.
func PoseidonHash(inputs ...*big.Int) (*big.Int, error) {
	return poseidon.Hash(inputs)
}

// VoteID calculates the poseidon hash of:
// the process ID, voter's address and a secret value k.
// This is truncated to the least significant 63 bits,
// and then shifted to the upper half of the 64 bit space
// by setting the first bit to 1.
// The vote ID is used to identify a vote in the system. The
// function transforms the inputs to safe values of ballot proof curve scalar
// field, then hashes them using iden3 poseidon.
func VoteID(processID, address, k *big.Int) (uint64, error) {
	if processID == nil || address == nil || k == nil {
		return 0, fmt.Errorf("processID, address, and k are required")
	}
	hash, err := PoseidonHash(
		crypto.BigToFF(params.BallotProofCurve.ScalarField(), processID),
		crypto.BigToFF(params.BallotProofCurve.ScalarField(), address),
		k)
	if err != nil {
		return 0, fmt.Errorf("failed to generate vote ID: %w", err)
	}
	hashTruncated := truncateToLowerBits(hash, VoteIDHashBits)
	voteIDMin := new(big.Int).SetUint64(VoteIDMin)
	return new(big.Int).Add(voteIDMin, hashTruncated).Uint64(), nil
}

// BallotIndex returns a BallotIndex on the lower half of the 64 bit space,
// between BallotMin and BallotMax.
//
//	BallotIndex = BallotMin + (censusIndex * 2^CensusAddressBitLen) + (address mod 2^CensusAddressBitLen)
func BallotIndex(address *big.Int, censusIndex uint64) (uint64, error) {
	if censusIndex > CensusIndexMax {
		return 0, fmt.Errorf("censusIndex too big")
	}
	censusIndexShifted := censusIndex * (1 << CensusAddressBitLen)
	addressTruncated := truncateToLowerBits(address, CensusAddressBitLen)
	return BallotMin + censusIndexShifted + addressTruncated.Uint64(), nil
}

// truncateToLowerBits returns a big.Int truncated to the least-significant `bits`.
func truncateToLowerBits(input *big.Int, bits uint) *big.Int {
	mask := new(big.Int).Lsh(big.NewInt(1), bits) // 1 << bits
	mask.Sub(mask, big.NewInt(1))                 // (1 << bits) - 1
	return new(big.Int).And(input, mask)          // input & ((1 << bits) - 1)
}
