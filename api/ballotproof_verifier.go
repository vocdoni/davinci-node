package api

import (
	"fmt"
	"sync"

	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

type ballotProofVerifier struct {
	rawVerifyingKeyFn  func() ([]byte, error)
	verifyAndConvertFn func(vkey []byte, proof *circomgnark.CircomProof, pubSignals []string) (*circomgnark.GnarkRecursionProof, error)

	vkMu sync.Mutex
	vk   []byte
}

var defaultBallotProofVerifier = &ballotProofVerifier{
	rawVerifyingKeyFn:  ballotproof.Artifacts.RawVerifyingKey,
	verifyAndConvertFn: circomgnark.VerifyAndConvertToRecursion,
}

func (v *ballotProofVerifier) VerifyBallotProof(
	address types.HexBytes,
	voteID types.VoteID,
	ballotInputsHash *types.BigInt,
	proof *circomgnark.CircomProof,
) (*circomgnark.GnarkRecursionProof, error) {
	if ballotInputsHash == nil {
		return nil, fmt.Errorf("ballot inputs hash is required")
	}
	if proof == nil {
		return nil, fmt.Errorf("ballot proof is required")
	}
	rawBallotProofVK, err := v.rawVerifyingKey()
	if err != nil {
		return nil, fmt.Errorf("load ballot proof verification key: %w", err)
	}

	verifiedProof, err := v.verifyAndConvertFn(rawBallotProofVK, proof, []string{
		address.BigInt().ToFF(params.BallotProofCurve.ScalarField()).String(),
		voteID.BigInt().String(),
		ballotInputsHash.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("verify and convert ballot proof: %w", err)
	}
	return verifiedProof, nil
}

func (v *ballotProofVerifier) rawVerifyingKey() ([]byte, error) {
	v.vkMu.Lock()
	defer v.vkMu.Unlock()

	if v.vk != nil {
		return v.vk, nil
	}

	vk, err := v.rawVerifyingKeyFn()
	if err != nil {
		return nil, err
	}
	v.vk = vk
	return v.vk, nil
}
