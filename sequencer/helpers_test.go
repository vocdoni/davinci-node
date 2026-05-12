package sequencer

import (
	"math/big"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	leanimt "github.com/vocdoni/lean-imt-go"
	leancensus "github.com/vocdoni/lean-imt-go/census"
)

// buildCensusForTest creates an in-memory census tree with the given addresses
// (all with weight 1), imports it into stg, and returns the census root bytes.
func buildCensusForTest(t *testing.T, stg *storage.Storage, addrs ...uint64) types.HexBytes {
	t.Helper()
	tree, err := leancensus.NewCensusIMT(nil, leanimt.PoseidonHasher)
	if err != nil {
		t.Fatalf("NewCensusIMT: %v", err)
	}
	for _, n := range addrs {
		if err := tree.Add(testutil.DeterministicAddress(n), big.NewInt(1)); err != nil {
			t.Fatalf("tree.Add(%d): %v", n, err)
		}
	}
	root, ok := tree.Root()
	if !ok {
		t.Fatal("census tree has no root after adding entries")
	}
	censusRoot := types.HexBytes(root.Bytes())
	if _, err := stg.CensusDB().Import(censusRoot, tree.Dump()); err != nil {
		t.Fatalf("CensusDB.Import: %v", err)
	}
	return censusRoot
}

// ballot returns a minimal AggregatorBallot for the given deterministic address index.
func ballot(n uint64) *storage.AggregatorBallot {
	return &storage.AggregatorBallot{
		VoteID:  testutil.RandomVoteID(),
		Address: testutil.DeterministicAddress(n).Big(),
		Weight:  big.NewInt(1),
	}
}

// TestFilterBallotsByCensusAllPresent verifies that when every ballot address
// is in the census tree the full batch is returned unchanged.
func TestFilterBallotsByCensusAllPresent(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	censusRoot := buildCensusForTest(t, stg, 0, 1)
	makeTestProcessWithCensus(t, stg, processID, censusRoot)

	seq := &Sequencer{stg: stg, processIDs: NewProcessIDMap()}
	input := []*storage.AggregatorBallot{ballot(0), ballot(1)}

	got, err := seq.filterBallotsByCensus(processID, input)
	c.Assert(err, qt.IsNil)
	c.Assert(got, qt.HasLen, 2)
}

// TestFilterBallotsByCensusOneAbsent verifies that a ballot whose address is
// absent from the census tree is removed from the returned slice while the
// remaining ballot is kept and no error is returned.
func TestFilterBallotsByCensusOneAbsent(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	// Census contains addresses 0 and 1; address 999 is not in the census.
	censusRoot := buildCensusForTest(t, stg, 0, 1)
	makeTestProcessWithCensus(t, stg, processID, censusRoot)

	seq := &Sequencer{stg: stg, processIDs: NewProcessIDMap()}
	input := []*storage.AggregatorBallot{ballot(0), ballot(999), ballot(1)}

	got, err := seq.filterBallotsByCensus(processID, input)
	c.Assert(err, qt.IsNil)
	c.Assert(got, qt.HasLen, 2)
	// The two surviving ballots must be the ones with indices 0 and 1.
	c.Assert(got[0].Address.Cmp(testutil.DeterministicAddress(0).Big()), qt.Equals, 0)
	c.Assert(got[1].Address.Cmp(testutil.DeterministicAddress(1).Big()), qt.Equals, 0)
}

// TestFilterBallotsByCensusCensusUnavailable verifies that an error is
// returned when the census root stored on the process has no corresponding
// entry in the census database, so the caller can retry rather than silently
// dropping all ballots.
func TestFilterBallotsByCensusCensusUnavailable(t *testing.T) {
	c := qt.New(t)
	stg := newTestSequencerStorage(t)
	defer stg.Close()

	processID := testutil.RandomProcessID()
	// Create the process with a census root that was never imported into the DB.
	missingRoot := types.HexBytes(testutil.RandomCensusRoot().Bytes())
	makeTestProcessWithCensus(t, stg, processID, missingRoot)

	seq := &Sequencer{stg: stg, processIDs: NewProcessIDMap()}
	input := []*storage.AggregatorBallot{ballot(0)}

	_, err := seq.filterBallotsByCensus(processID, input)
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "failed to load census")
}
