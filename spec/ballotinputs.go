package spec

import (
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/spec/hash"
)

// BallotInputsHashRTE computes the ballot inputs hash using RTE-encoded points.
func BallotInputsHashRTE(
	processID *big.Int,
	ballotMode BallotMode,
	encryptionKeyX *big.Int,
	encryptionKeyY *big.Int,
	address *big.Int,
	voteID uint64,
	ballot []*big.Int,
	weight *big.Int,
) (*big.Int, error) {
	return ballotInputsHash(processID, ballotMode, encryptionKeyX, encryptionKeyY, address, voteID, ballot, weight)
}

func ballotInputsHash(
	processID *big.Int,
	ballotMode BallotMode,
	encryptionKeyX *big.Int,
	encryptionKeyY *big.Int,
	address *big.Int,
	voteID uint64,
	ballot []*big.Int,
	weight *big.Int,
) (*big.Int, error) {
	if processID == nil || encryptionKeyX == nil || encryptionKeyY == nil || address == nil || weight == nil {
		return nil, fmt.Errorf("ballot inputs hash: required input is nil")
	}
	if len(ballot) == 0 {
		return nil, fmt.Errorf("ballot inputs hash: ballot is empty")
	}
	ballotModePacked, err := ballotMode.Pack()
	if err != nil {
		return nil, fmt.Errorf("ballot inputs hash: pack ballot mode: %w", err)
	}

	inputs := make([]*big.Int, 0, 6+len(ballot)+1)
	inputs = append(inputs,
		processID,
		ballotModePacked,
		encryptionKeyX,
		encryptionKeyY,
		address,
		new(big.Int).SetUint64(voteID),
	)
	inputs = append(inputs, ballot...)
	inputs = append(inputs, weight)

	h, err := hash.PoseidonMultiHash(inputs)
	if err != nil {
		return nil, fmt.Errorf("ballot inputs hash: %w", err)
	}
	return h, nil
}
