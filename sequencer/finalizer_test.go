package sequencer

import (
	"math/big"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/spec/params"
	specutil "github.com/vocdoni/davinci-node/spec/util"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
)

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
	t.Skip("TODO: fix and re-enable")
	c := qt.New(t)

	// Setup test environment
	stg, stateDB, procesSID, _, _, cleanup := setupTestEnvironment(t, 5000)
	defer cleanup()

	// Create a finalizer
	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil)
	f.Start(t.Context(), 0)

	// Test finalize
	f.OndemandCh <- procesSID
	_, err := f.WaitUntilResults(t.Context(), procesSID)
	c.Assert(err, qt.IsNil, qt.Commentf("finalize failed: %v", err))

	// Check that the process has been updated with the result
	process, err := stg.Process(procesSID)
	c.Assert(err, qt.IsNil)
	c.Assert(process.Result, qt.Not(qt.IsNil))

	// Verify the results are as expected
	c.Assert(len(process.Result), qt.Equals, params.FieldsPerBallot)
	expected := big.NewInt(5000)
	c.Assert(process.Result[0].MathBigInt().Cmp(expected), qt.Equals, 0,
		qt.Commentf("Expected first result to be 500, got %s", process.Result[0].String()))
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
	// Create temporary directory
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	// Create database
	mainDB, err := metadb.New(db.TypePebble, dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Create storage
	stg := storage.New(mainDB)

	// Get state database
	stateDB := stg.StateDB()

	// Create a process ID
	processID := testutil.DeterministicProcessID(42)

	// Create encryption keys
	curve := curves.New(bjj.CurveType)
	pubKey, _, err := elgamal.GenerateKey(curve)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Store the keys in storage
	t.Log("TODO: fix SetEncryptionKeys", processID, pubKey)
	// err = stg.SetEncryptionKeys(processID, pubKey, privKey)
	// if err != nil {
	// 	t.Fatalf("failed to store encryption keys: %v", err)
	// }

	// Store the process
	err = stg.NewProcess(testutil.RandomProcess(processID))
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

	process, err := stg.Process(processID)
	if err != nil {
		t.Fatalf("failed to get process: %v", err)
	}

	// Setup state with test data
	process.StateRoot = setupTestState(t, stateDB, processID, pubKey, process.StateRoot.MathBigInt(), resultValue)
	err = stg.UpdateProcess(processID, func(p *types.Process) error {
		p.StateRoot = process.StateRoot
		return nil
	})
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

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
	// Load the initial state
	st, err := state.LoadOnRoot(stateDB, processID, stateRoot)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
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
	st.SetResults(encryptedResults)

	stateRoot, err = st.RootAsBigInt()
	if err != nil {
		t.Fatalf("failed to get state root: %v", err)
	}
	return (*types.BigInt)(stateRoot)
}
