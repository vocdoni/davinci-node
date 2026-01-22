package testutil

import (
	"fmt"
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

func NewStateForTest(t *testing.T,
	processID types.ProcessID,
	ballotMode *big.Int,
	censusOrigin types.CensusOrigin,
	encryptionKey circuits.EncryptionKey[*big.Int],
) *state.State {
	s, err := state.New(metadb.NewTest(t), processID)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("create state"))

	err = s.Initialize(
		censusOrigin.BigInt().MathBigInt(),
		ballotMode,
		encryptionKey,
	)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("initialize state"))

	return s
}

func NewRandomState(t *testing.T, origin types.CensusOrigin) *state.State {
	return NewStateForTest(t,
		testutil.RandomProcessID(),
		testutil.BallotModePacked(),
		origin,
		testutil.RandomEncryptionPubKey(),
	)
}

func NewVoteForTest(publicKey ecc.Point, index uint64, value int) *state.Vote {
	fields := [params.FieldsPerBallot]*big.Int{}
	for i := range fields {
		fields[i] = big.NewInt(int64(value + i))
	}
	ballot, err := elgamal.NewBallot(publicKey).Encrypt(fields, publicKey, nil)
	if err != nil {
		panic(fmt.Errorf("failed to encrypt ballot: %v", err))
	}
	k, err := specutil.RandomK()
	if err != nil {
		panic(fmt.Errorf("failed to generate k: %v", err))
	}
	reencryptedBallot, _, err := ballot.Reencrypt(publicKey, k)
	if err != nil {
		panic(fmt.Errorf("failed to reencrypt ballot: %v", err))
	}
	return &state.Vote{
		Address:           testutil.DeterministicAddress(index).Big(),
		VoteID:            testutil.RandomVoteID(),
		Ballot:            ballot,
		ReencryptedBallot: reencryptedBallot,
		Weight:            big.NewInt(testutil.Weight),
	}
}

func NewVotesForTest(publicKey ecc.Point, numVotes uint64, value int) []*state.Vote {
	votes := make([]*state.Vote, 0, numVotes)
	for i := range numVotes {
		votes = append(votes, NewVoteForTest(publicKey, i, value))
	}
	return votes
}

// EncryptionKeyAsECCPoint returns the encryption key of the state as an ecc.Point
func EncryptionKeyAsECCPoint(s *state.State) ecc.Point {
	ek := s.EncryptionKey()
	return state.Curve.New().SetPoint(ek.PubKey[0], ek.PubKey[1])
}
