package storage

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/inmemory"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
	leanimt "github.com/vocdoni/lean-imt-go"
	leancensus "github.com/vocdoni/lean-imt-go/census"
)

func TestProcessIsAcceptingVotesRequiresCensus(t *testing.T) {
	c := qt.New(t)
	st := newTestStorage(t)
	defer st.Close()

	processID := testutil.DeterministicProcessID(10)
	process := newReadyProcess(processID, testutil.RandomCensus(types.CensusOriginMerkleTreeOffchainStaticV1))

	err := st.NewProcess(process)
	c.Assert(err, qt.IsNil)

	ok, err := st.ProcessIsAcceptingVotes(processID)
	c.Assert(ok, qt.IsFalse)
	c.Assert(err, qt.ErrorMatches, "process .* census not available.*")
}

func TestProcessIsAcceptingVotesAcceptsTrimmedCensusRoot(t *testing.T) {
	c := qt.New(t)
	st := newTestStorage(t)
	defer st.Close()

	censusRoot := importTestCensus(t, c, st)
	census := &types.Census{
		CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		CensusRoot:   censusRoot.LeftPad(types.CensusRootLength),
		CensusURI:    "http://example.com/census",
	}

	processID := testutil.DeterministicProcessID(11)
	process := newReadyProcess(processID, census)

	err := st.NewProcess(process)
	c.Assert(err, qt.IsNil)

	ok, err := st.ProcessIsAcceptingVotes(processID)
	c.Assert(err, qt.IsNil)
	c.Assert(ok, qt.IsTrue)
}

func newReadyProcess(pid types.ProcessID, census *types.Census) *types.Process {
	return &types.Process{
		ID:             &pid,
		Status:         types.ProcessStatusReady,
		OrganizationId: common.Address{},
		StartTime:      time.Now().Add(-time.Minute),
		Duration:       time.Hour,
		MetadataURI:    "https://example.com/metadata",
		StateRoot:      testutil.StateRoot(),
		BallotMode:     testutil.BallotModeInternal(),
		Census:         census,
	}
}

func importTestCensus(t *testing.T, c *qt.C, st *Storage) types.HexBytes {
	t.Helper()

	memDB, err := inmemory.New(db.Options{})
	c.Assert(err, qt.IsNil)

	censusTree, err := leancensus.NewCensusIMT(memDB, leanimt.PoseidonHasher)
	c.Assert(err, qt.IsNil)
	c.Assert(censusTree.Add(testutil.DeterministicAddress(1), big.NewInt(1)), qt.IsNil)

	dump, err := censusTree.DumpAll()
	c.Assert(err, qt.IsNil)
	c.Assert(dump.Root, qt.Not(qt.IsNil))

	payload, err := json.Marshal(dump)
	c.Assert(err, qt.IsNil)

	_, err = st.CensusDB().ImportAll(payload)
	c.Assert(err, qt.IsNil)

	return types.HexBytes(dump.Root.Bytes())
}
