package sequencer

import (
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec"
	"github.com/vocdoni/davinci-node/spec/params"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

func TestMaxPossibleResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		process *types.Process
		want    uint64
	}{
		{
			name: "returns zero when no votes can contribute",
			process: &types.Process{
				BallotMode:  spec.BallotMode{MaxValue: 16},
				VotersCount: types.NewInt(0),
			},
			want: 0,
		},
		{
			name: "uses maxValue times votersCount",
			process: &types.Process{
				BallotMode:  spec.BallotMode{MaxValue: 16},
				VotersCount: types.NewInt(3),
			},
			want: 48,
		},
		{
			name: "caps at fallback maximum",
			process: &types.Process{
				BallotMode:  spec.BallotMode{MaxValue: 1_000_000_000_000},
				VotersCount: types.NewInt(2),
			},
			want: maxPossibleResultCap,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := maxPossibleResult(tc.process); got != tc.want {
				t.Fatalf("maxPossibleResult() = %d, want %d", got, tc.want)
			}
		})
	}
}

func loadResultsVerifierArtifactsForTest(t *testing.T) *internalCircuits {
	t.Helper()
	ca := new(internalCircuits)
	var err error
	ca.resultsVerifier, err = results.Artifacts.LoadOrDownload(t.Context())
	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to load results verifier artifacts: %v", err))
	return ca
}

// TestFinalize tests the finalize method of the Finalizer struct
func TestFinalize(t *testing.T) {
	c := qt.New(t)

	expectedResults := int64(5)

	// Setup test environment
	stg, stateDB, processID, _, _, cleanup := setupTestEnvironment(t, expectedResults)
	defer cleanup()

	// Create a finalizer
	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	f.Start(ctx, 0)

	// Force to finalize the process
	f.OndemandCh <- processID

	// Check that the process has been updated with the result
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Fatal("finalizer context done")
		case <-ticker.C:
			if stg.HasVerifiedResults(processID) {
				for {
					results, err := stg.NextVerifiedResults()
					if err != nil {
						c.Fatal(err)
					}

					if results.ProcessID != processID {
						continue
					}

					c.Assert(len(results.Inputs.Results), qt.Equals, params.FieldsPerBallot)
					c.Assert(results.Inputs.Results[0].Cmp(big.NewInt(expectedResults)), qt.Equals, 0)
					return
				}
			}
		}
	}
}

func TestFinalizeMissingEncryptionKeysReturnsSequencerSentinel(t *testing.T) {
	c := qt.New(t)

	stg, stateDB, processID, _, _, cleanup := setupTestEnvironment(t, 5)
	defer cleanup()
	err := stg.UpdateProcess(processID, func(p *types.Process) error {
		p.EncryptionKey = nil
		return nil
	})
	c.Assert(err, qt.IsNil)

	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil, nil)

	err = f.finalize(processID)
	c.Assert(err, qt.IsNotNil)
	c.Assert(errors.Is(err, ErrProcessEncryptionKeysMissing), qt.IsTrue)
}

func TestShouldMarkMissingRootInvalid(t *testing.T) {
	t.Parallel()

	processID := testutil.DeterministicProcessID(42)

	testCases := []struct {
		name            string
		supportsBlobTxs func(types.ProcessID) (bool, error)
		wantMarkInvalid bool
	}{
		{
			name:            "blob-capable runtimes keep missing roots retryable",
			supportsBlobTxs: func(types.ProcessID) (bool, error) { return true, nil },
			wantMarkInvalid: false,
		},
		{
			name:            "non-blob runtimes treat missing roots as terminal",
			supportsBlobTxs: func(types.ProcessID) (bool, error) { return false, nil },
			wantMarkInvalid: true,
		},
		{
			name:            "missing capability getter preserves terminal fallback",
			supportsBlobTxs: nil,
			wantMarkInvalid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := qt.New(t)
			calls := 0
			getter := tc.supportsBlobTxs
			if getter != nil {
				getter = func(processID types.ProcessID) (bool, error) {
					calls++
					return tc.supportsBlobTxs(processID)
				}
			}

			f := &finalizer{supportsBlobTxs: getter}

			gotMarkInvalid, err := f.shouldMarkMissingRootInvalid(processID)
			c.Assert(err, qt.IsNil)
			c.Assert(gotMarkInvalid, qt.Equals, tc.wantMarkInvalid)

			wantCalls := 0
			if getter != nil {
				wantCalls = 1
			}
			c.Assert(calls, qt.Equals, wantCalls)
		})
	}
}

// setupTestEnvironment creates a test environment with necessary objects
func setupTestEnvironment(t *testing.T, resultValue int64) (
	*storage.Storage,
	db.Database,
	types.ProcessID,
	ecc.Point,
	ecc.Point,
	func(),
) {
	c := qt.New(t)
	// Create temporary directory
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	// Create database
	mainDB, err := metadb.New(db.TypePebble, dbPath)
	c.Assert(err, qt.IsNil)

	// Create storage
	stg := storage.New(mainDB)

	// Get state database
	stateDB := stg.StateDB()

	// Create a process ID
	processID := testutil.DeterministicProcessID(42)

	// Create encryption keys
	curve := curves.New(bjj.CurveType)
	pubKey, privKey, err := elgamal.GenerateKey(curve)
	c.Assert(err, qt.IsNil)

	// Store the process
	x, y := pubKey.Point()
	process := testutil.RandomProcessWithEncryptionKey(processID, types.EncryptionKey{
		X: (*types.BigInt)(x),
		Y: (*types.BigInt)(y),
	})
	err = stg.NewProcess(process)
	c.Assert(err, qt.IsNil)

	// Store the keys in storage
	err = stg.SetEncryptionKeys(pubKey, privKey)
	c.Assert(err, qt.IsNil)

	// Setup state with test data
	err = stg.UpdateProcess(processID, func(p *types.Process) error {
		p.StateRoot = setupTestState(t, stateDB, processID, pubKey, process.StateRoot.MathBigInt(), resultValue)
		p.VotersCount = types.NewInt(1)
		return nil
	})
	c.Assert(err, qt.IsNil)

	// Return cleanup function
	cleanup := func() {
		stg.Close()
	}

	return stg, stateDB, processID, curve, pubKey, cleanup
}

// setupTestState initializes the state with encrypted test data
func setupTestState(
	t *testing.T,
	stateDB db.Database,
	processID types.ProcessID,
	pubKey ecc.Point,
	stateRoot *big.Int,
	resultValue int64,
) *types.BigInt {
	// Load the initial state for mutation.
	st, err := state.New(stateDB, processID)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if err := st.SetRootAsBigInt(stateRoot); err != nil {
		t.Fatalf("failed to set state root: %v", err)
	}

	// Create an encrypted results accumulator with a known value
	curve := pubKey.New()
	resultsAccumulator := elgamal.NewBallot(curve)
	resultsValues := [params.FieldsPerBallot]*big.Int{}
	for i := range params.FieldsPerBallot {
		resultsValues[i] = big.NewInt(resultValue)
	}
	k1, err := specutil.RandomK()
	if err != nil {
		t.Fatalf("failed to generate k1: %v", err)
	}
	encryptedResults, err := resultsAccumulator.Encrypt(resultsValues, pubKey, k1)
	if err != nil {
		t.Fatalf("failed to encrypt results accumulator: %v", err)
	}

	// Store the encrypted results in the state
	if err := st.SetResults(encryptedResults); err != nil {
		t.Fatalf("failed to set encrypted results: %v", err)
	}

	stateRoot, err = st.RootAsBigInt()
	if err != nil {
		t.Fatalf("failed to get state root: %v", err)
	}
	return (*types.BigInt)(stateRoot)
}
