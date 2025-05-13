package finalizer

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/metadb"
)

// TestFinalize tests the finalize method of the Finalizer struct
func TestFinalize(t *testing.T) {
	c := qt.New(t)

	// Setup test environment
	stg, stateDB, pid, _, _, _, cleanup := setupTestEnvironment(t, 10000, 5000)
	defer cleanup()

	// Create a finalizer
	f := New(stg, stateDB)
	f.Start(t.Context(), 0)

	// Test finalize
	f.OndemandCh <- pid
	err := f.WaitUntilFinalized(pid)
	c.Assert(err, qt.IsNil, qt.Commentf("finalize failed: %v", err))

	// Check that the process has been updated with the result
	process, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(process.Result, qt.Not(qt.IsNil))

	// Verify the results are as expected
	c.Assert(len(process.Result), qt.Equals, types.FieldsPerBallot)
	expected := big.NewInt(5000)
	c.Assert(process.Result[0].MathBigInt().Cmp(expected), qt.Equals, 0,
		qt.Commentf("Expected first result to be 500, got %s", process.Result[0].String()))
}

// TestFinalizeByDate tests the finalizeByDate functionality of the Finalizer struct
func TestFinalizeByDate(t *testing.T) {
	c := qt.New(t)

	// Setup test environment
	stg, stateDB, pid, _, _, _, cleanup := setupTestEnvironment(t, 10000, 5000)
	defer cleanup()

	// Update the process with specific start time and duration
	process, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)

	// Set the process to start in the past and end in the future
	startTime := time.Now().Add(-30 * time.Minute) // 30 minutes ago
	duration := time.Hour                          // 1 hour long (so it ends 30 minutes from now)
	process.StartTime = startTime
	process.Duration = duration

	err = stg.SetProcess(process)
	c.Assert(err, qt.IsNil)

	// Create a finalizer with monitoring disabled
	f := New(stg, stateDB)
	f.Start(t.Context(), 0)

	// Call finalizeByDate with current time
	// This should cause the process to be finalized because endTime > currentTime
	currentTime := time.Now()
	f.finalizeByDate(currentTime)

	// Wait for finalization to complete
	err = f.WaitUntilFinalized(pid)
	c.Assert(err, qt.IsNil, qt.Commentf("finalize by date failed: %v", err))

	// Check that the process has been updated with the result
	process, err = stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(process.Result, qt.Not(qt.IsNil))

	// Verify the results are as expected
	c.Assert(len(process.Result), qt.Equals, types.FieldsPerBallot)
	expected := big.NewInt(5000)
	c.Assert(process.Result[0].MathBigInt().Cmp(expected), qt.Equals, 0,
		qt.Commentf("Expected first result to be 5000, got %s", process.Result[0].String()))
}

// setupTestEnvironment creates a test environment with necessary objects
func setupTestEnvironment(t *testing.T, addValue, subValue int64) (
	*storage.Storage,
	db.Database,
	*types.ProcessID,
	ecc.Point,
	ecc.Point,
	*big.Int,
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
	pid := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		ChainID: 1,
	}

	// Create encryption keys
	curve := curves.New(bjj.CurveType)
	pubKey, privKey, err := elgamal.GenerateKey(curve)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Store the keys in storage
	err = stg.SetEncryptionKeys(pid, pubKey, privKey)
	if err != nil {
		t.Fatalf("failed to store encryption keys: %v", err)
	}

	// Create a process
	process := &types.Process{
		ID:          pid.Marshal(),
		Status:      0,
		StartTime:   time.Now(),
		Duration:    time.Hour,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   make([]byte, 32),
		BallotMode: &types.BallotMode{
			MaxCount:        8,
			MaxValue:        new(types.BigInt).SetUint64(100),
			MinValue:        new(types.BigInt).SetUint64(0),
			MaxTotalCost:    new(types.BigInt).SetUint64(0),
			MinTotalCost:    new(types.BigInt).SetUint64(0),
			ForceUniqueness: false,
			CostFromWeight:  false,
		},
		Census: &types.Census{
			CensusRoot:   make([]byte, 32),
			MaxVotes:     &types.BigInt{},
			CensusURI:    "http://example.com/census",
			CensusOrigin: 0,
		},
	}

	// Set BigInt values
	process.Census.MaxVotes.SetUint64(1000)

	// Store the process
	err = stg.SetProcess(process)
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

	// Setup state with test data
	setupTestState(t, stateDB, pid, pubKey, addValue, subValue)

	// Return cleanup function
	cleanup := func() {
		stg.Close()
	}

	return stg, stateDB, pid, curve, pubKey, privKey, cleanup
}

// setupTestState initializes the state with encrypted test data
func setupTestState(t *testing.T, stateDB db.Database, pid *types.ProcessID, pubKey ecc.Point, addValue, subValue int64) {
	// Create a new state
	st, err := state.New(stateDB, pid.BigInt())
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	// Initialize the state with proper circuit types
	censusRoot := big.NewInt(123)

	// Create a proper BallotMode for circuits using the utility function
	// BoolToBigInt is needed to convert boolean values to *big.Int
	ballotMode := circuits.BallotMode[*big.Int]{
		MaxCount:        big.NewInt(8),
		MaxValue:        big.NewInt(100),
		MinValue:        big.NewInt(0),
		MaxTotalCost:    big.NewInt(0),
		MinTotalCost:    big.NewInt(0),
		ForceUniqueness: circuits.BoolToBigInt(false),
		CostFromWeight:  circuits.BoolToBigInt(false),
		CostExp:         big.NewInt(0), // Missing field in original
	}

	// Create a proper EncryptionKey for circuits
	encryptionKey := circuits.EncryptionKeyFromECCPoint(pubKey)

	err = st.Initialize(censusRoot, ballotMode, encryptionKey)
	if err != nil {
		t.Fatalf("failed to initialize state: %v", err)
	}

	// Create encrypted accumulators with known values
	curve := pubKey.New()

	// Add accumulator with a known value
	addAccumulator := elgamal.NewBallot(curve)
	addValues := [types.FieldsPerBallot]*big.Int{}
	// Set values for testing
	for i := range types.FieldsPerBallot {
		addValues[i] = big.NewInt(addValue)
	}
	// Encrypt the values
	k1 := new(big.Int).SetBytes(util.RandomBytes(16))
	encryptedAdd, err := addAccumulator.Encrypt(addValues, pubKey, k1)
	if err != nil {
		t.Fatalf("failed to encrypt add accumulator: %v", err)
	}

	// Sub accumulator with a known value
	subAccumulator := elgamal.NewBallot(curve)
	subValues := [types.FieldsPerBallot]*big.Int{}
	// Set values for testing
	for i := range types.FieldsPerBallot {
		subValues[i] = big.NewInt(subValue)
	}
	// Encrypt the values
	k2 := new(big.Int).SetBytes(util.RandomBytes(16))
	encryptedSub, err := subAccumulator.Encrypt(subValues, pubKey, k2)
	if err != nil {
		t.Fatalf("failed to encrypt sub accumulator: %v", err)
	}

	// Store the encrypted accumulators in the state
	st.SetResultsAdd(encryptedAdd)
	st.SetResultsSub(encryptedSub)
}
