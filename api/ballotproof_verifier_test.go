package api

import (
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

func TestBallotProofVerifierVerifyBallotProof(t *testing.T) {
	c := qt.New(t)

	expectedVK := []byte("vk")
	expectedProof := &circomgnark.GnarkRecursionProof{}
	circomProof := &circomgnark.CircomProof{}

	var gotVK []byte
	var gotProof *circomgnark.CircomProof
	var gotSignals []string

	verifier := ballotProofVerifier{
		rawVerifyingKeyFn: func() ([]byte, error) {
			return expectedVK, nil
		},
		verifyAndConvertFn: func(vk []byte, proof *circomgnark.CircomProof, pubSignals []string) (*circomgnark.GnarkRecursionProof, error) {
			gotVK = append([]byte(nil), vk...)
			gotProof = proof
			gotSignals = append([]string(nil), pubSignals...)
			return expectedProof, nil
		},
	}

	address := types.HexBytes{0x12, 0x34}
	voteID := types.VoteID(123)
	ballotInputsHash := types.NewInt(456)

	proof, err := verifier.VerifyBallotProof(address, voteID, ballotInputsHash, circomProof)
	c.Assert(err, qt.IsNil)
	c.Assert(proof, qt.Equals, expectedProof)
	c.Assert(gotVK, qt.DeepEquals, expectedVK)
	c.Assert(gotProof, qt.Equals, circomProof)
	c.Assert(gotSignals, qt.DeepEquals, []string{
		address.BigInt().String(),
		voteID.BigInt().String(),
		ballotInputsHash.String(),
	})
}

func TestBallotProofVerifierCachesRawVerifyingKey(t *testing.T) {
	c := qt.New(t)

	circomProof := &circomgnark.CircomProof{}
	ballotInputsHash := types.NewInt(456)

	loads := 0
	verifier := ballotProofVerifier{
		rawVerifyingKeyFn: func() ([]byte, error) {
			loads++
			return []byte("vk"), nil
		},
		verifyAndConvertFn: func(vk []byte, proof *circomgnark.CircomProof, pubSignals []string) (*circomgnark.GnarkRecursionProof, error) {
			return &circomgnark.GnarkRecursionProof{}, nil
		},
	}

	_, err := verifier.VerifyBallotProof(types.HexBytes{0x01}, types.VoteID(1), ballotInputsHash, circomProof)
	c.Assert(err, qt.IsNil)
	_, err = verifier.VerifyBallotProof(types.HexBytes{0x02}, types.VoteID(2), ballotInputsHash, circomProof)
	c.Assert(err, qt.IsNil)

	c.Assert(loads, qt.Equals, 1)
}

func TestBallotProofVerifierRawVerifyingKeyReturnsCachedSlice(t *testing.T) {
	c := qt.New(t)

	verifier := ballotProofVerifier{
		rawVerifyingKeyFn: func() ([]byte, error) {
			return []byte("vk"), nil
		},
	}

	first, err := verifier.rawVerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(first, qt.DeepEquals, []byte("vk"))

	second, err := verifier.rawVerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(second, qt.DeepEquals, []byte("vk"))
	c.Assert(second, qt.DeepEquals, first)
}
