package statetransitiontest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	qt "github.com/frankban/quicktest"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/rs/zerolog"
	censustest "github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/prover"
	davinci_solidity "github.com/vocdoni/davinci-node/solidity"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

const falseString = "false"

func TestMain(m *testing.M) {
	// enable log to see nbConstraints
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	m.Run()
}

func testCircuitCompile(t *testing.T, c frontend.Circuit) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}
	// enable log to see nbConstraints
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if _, err := frontend.Compile(circuits.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, c); err != nil {
		panic(err)
	}
}

func testCircuitProve(t *testing.T, circuit, witness frontend.Circuit) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}
	assert := test.NewAssert(t)
	assert.ProverSucceeded(
		circuit,
		witness,
		test.WithCurves(circuits.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))
}

func TestStateTransitionCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	// inputs generation
	now := time.Now()
	_, placeholder, assignments := StateTransitionInputsForTest(t, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1, 3)
	c.Logf("inputs generation took %s", time.Since(now).String())
	// proving
	now = time.Now()

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(placeholder, assignments,
		test.WithCurves(circuits.StateTransitionCurve), test.WithBackends(backend.GROTH16))
	c.Logf("proving took %s", time.Since(now).String())
}

func TestStateTransitionFullProvingCircuit(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseString {
		t.Skip("skipping circuit tests...")
	}
	c := qt.New(t)
	// inputs generation
	totalStart := time.Now()
	now := time.Now()

	// Use centralized testing ProcessID for consistent caching
	testResults, placeholder, assignments := StateTransitionInputsForTest(t, testutil.FixedProcessID(), types.CensusOriginMerkleTreeOffchainStaticV1, 3)
	c.Logf("inputs generation took %s", time.Since(now).String())

	// compile circuit
	now = time.Now()
	ccs, err := frontend.Compile(circuits.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, placeholder)
	c.Assert(err, qt.IsNil, qt.Commentf("compile circuit"))
	c.Logf("compiled circuit with %d constraints, took %s", ccs.GetNbConstraints(), time.Since(now).String())

	// setup proving and verifying keys
	now = time.Now()
	pk, vk, err := prover.Setup(ccs)
	c.Assert(err, qt.IsNil, qt.Commentf("setup"))
	c.Logf("setup took %s", time.Since(now).String())

	// create witness
	now = time.Now()
	w, err := frontend.NewWitness(assignments, circuits.StateTransitionCurve.ScalarField())
	c.Assert(err, qt.IsNil, qt.Commentf("create witness"))
	c.Logf("witness creation took %s", time.Since(now).String())

	// prove
	var proof groth16.Proof
	now = time.Now()
	opts := solidity.WithProverTargetSolidityVerifier(backend.GROTH16)
	proof, err = prover.ProveWithWitness(circuits.StateTransitionCurve, ccs, pk, w, opts)
	c.Assert(err, qt.IsNil, qt.Commentf("prove with witness"))
	c.Logf("proving took %s", time.Since(now).String())

	// verify the last proof with gnark
	now = time.Now()
	publicWitness, err := w.Public()
	c.Assert(err, qt.IsNil, qt.Commentf("get public witness"))
	err = groth16.Verify(proof, vk, publicWitness, solidity.WithVerifierTargetSolidityVerifier(backend.GROTH16))
	c.Assert(err, qt.IsNil, qt.Commentf("verify proof"))
	c.Logf("✓ gnark verification took %s", time.Since(now).String())

	// Export artifacts to temporary directory
	dir, err := os.MkdirTemp("", "davinci_solidity_*")
	c.Assert(err, qt.IsNil, qt.Commentf("create temp dir"))
	defer func() {
		if !t.Failed() {
			// Clean up if test passes
			if err := os.RemoveAll(dir); err != nil {
				log.Printf("warning: failed to remove temp dir %s: %v", dir, err)
			}
		}
	}()

	c.Logf("exporting artifacts to %s", dir)

	// Export verification key to Solidity
	vkFile, err := os.OpenFile(filepath.Join(dir, "vk.sol"), os.O_CREATE|os.O_WRONLY, 0o644)
	c.Assert(err, qt.IsNil, qt.Commentf("create vk.sol file"))
	err = vk.ExportSolidity(vkFile)
	c.Assert(err, qt.IsNil, qt.Commentf("export vk to solidity"))
	if err := vkFile.Close(); err != nil {
		log.Printf("warning: failed to close vk.sol file: %v", err)
	}

	// Convert proof to Solidity format
	solProof := davinci_solidity.Groth16CommitmentProof{}
	err = solProof.FromGnarkProof(proof)
	c.Assert(err, qt.IsNil, qt.Commentf("convert to solidity proof"))

	// Save proof as JSON
	proofJSON, err := json.MarshalIndent(solProof, "", "  ")
	c.Assert(err, qt.IsNil, qt.Commentf("marshal proof to JSON"))
	err = os.WriteFile(filepath.Join(dir, "proof.json"), proofJSON, 0o644)
	c.Assert(err, qt.IsNil, qt.Commentf("write proof.json"))

	// Save proof as ABI encoded
	abiProof, err := solProof.ABIEncode()
	c.Assert(err, qt.IsNil, qt.Commentf("ABI encode proof"))
	err = os.WriteFile(filepath.Join(dir, "proof.abi"), abiProof, 0o644)
	c.Assert(err, qt.IsNil, qt.Commentf("write proof.abi"))

	// Get public inputs from test results
	publicInputs := testResults.PublicInputs

	// Convert to array of 8 big.Ints for Solidity (matching StateTransitionBatchProofInputs)
	var inputArray [8]*big.Int
	inputArray[0] = publicInputs.RootHashBefore
	inputArray[1] = publicInputs.RootHashAfter
	inputArray[2] = publicInputs.VotersCount
	inputArray[3] = publicInputs.OverwrittenVotesCount
	inputArray[4] = publicInputs.CensusRoot
	inputArray[5] = publicInputs.BlobCommitmentLimbs[0]
	inputArray[6] = publicInputs.BlobCommitmentLimbs[1]
	inputArray[7] = publicInputs.BlobCommitmentLimbs[2]

	// Create a simplified struct for JSON export
	type PublicInputsJSON struct {
		RootHashBefore        *big.Int    `json:"rootHashBefore"`
		RootHashAfter         *big.Int    `json:"rootHashAfter"`
		VotersCount           *big.Int    `json:"votersCount"`
		OverwrittenVotesCount *big.Int    `json:"overwrittenVotesCount"`
		CensusRoot            *big.Int    `json:"censusRoot"`
		BlobCommitmentLimbs   [3]*big.Int `json:"blobCommitmentLimbs"`
	}

	inputsForJSON := PublicInputsJSON{
		RootHashBefore:        publicInputs.RootHashBefore,
		RootHashAfter:         publicInputs.RootHashAfter,
		VotersCount:           publicInputs.VotersCount,
		OverwrittenVotesCount: publicInputs.OverwrittenVotesCount,
		CensusRoot:            publicInputs.CensusRoot,
		BlobCommitmentLimbs:   publicInputs.BlobCommitmentLimbs,
	}

	// Save inputs as JSON
	inputsJSON, err := json.MarshalIndent(inputsForJSON, "", "  ")
	c.Assert(err, qt.IsNil, qt.Commentf("marshal inputs to JSON"))
	err = os.WriteFile(filepath.Join(dir, "inputs.json"), inputsJSON, 0o644)
	c.Assert(err, qt.IsNil, qt.Commentf("write inputs.json"))

	// Save inputs as ABI encoded (all 8 values - what Solidity expects)
	abiInputs, err := abiEncodeInputs(inputArray)
	c.Assert(err, qt.IsNil, qt.Commentf("ABI encode inputs"))
	err = os.WriteFile(filepath.Join(dir, "inputs.abi"), abiInputs, 0o644)
	c.Assert(err, qt.IsNil, qt.Commentf("write inputs.abi"))

	c.Logf("✓ exported proof and inputs to %s", dir)
	c.Logf("total proving process took %s", time.Since(totalStart).String())

	// Solidity Verification (Docker-based)

	// Check if Docker is available
	if !isDockerAvailable() {
		c.Logf("Docker not available, skipping Solidity verification")
		c.Logf("  To test Solidity verification, install Docker and run again")
		return
	}

	c.Logf("=== Starting Solidity Verification ===")
	solidityStart := time.Now()

	// Step 1: Compile Solidity contract with Docker
	c.Logf("1. Compiling verification contract...")
	buildDir := filepath.Join(dir, "build")
	err = os.MkdirAll(buildDir, 0o755)
	c.Assert(err, qt.IsNil, qt.Commentf("create build directory"))

	var compileCmd *exec.Cmd
	if _, err := exec.LookPath("solc"); err == nil {
		c.Logf("Found local solc, using it...")
		compileCmd = exec.Command("solc",
			"--abi", "--bin",
			filepath.Join(dir, "vk.sol"),
			"-o", buildDir)
	} else {
		compileCmd = exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/src", dir),
			"ethereum/solc:stable",
			"--abi", "--bin",
			"/src/vk.sol",
			"-o", "/src/build")
	}

	compileOutput, err := compileCmd.CombinedOutput()
	if err != nil {
		c.Logf("Compile output: %s", string(compileOutput))
	}
	c.Assert(err, qt.IsNil, qt.Commentf("compile Solidity contract"))
	c.Logf("  ✓ Contract compiled")

	// Step 2: Deploy and verify on SimulatedBackend
	c.Logf("2. Deploying contract to simulated blockchain...")
	success := verifySolidityProof(c, buildDir, &solProof, inputArray)
	if success {
		c.Logf("  ✓ Solidity verification SUCCEEDED!")
		c.Logf("=== Solidity verification took %s ===", time.Since(solidityStart).String())
		c.Logf("")
		c.Logf("✅ COMPLETE: Proof verified both with gnark AND Solidity!")
	} else {
		c.Fatalf("❌ Solidity verification FAILED - this should not happen!")
	}
}

// isDockerAvailable checks if Docker is available on the system
func isDockerAvailable() bool {
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}

// abiEncodeInputs encodes a [8]*big.Int array to ABI format (8 × 32 bytes)
func abiEncodeInputs(inputs [8]*big.Int) ([]byte, error) {
	result := make([]byte, 0, 256) // 8 × 32 bytes
	for i := range 8 {
		// Pad to 32 bytes, big-endian
		b := inputs[i].Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		result = append(result, padded...)
	}
	return result, nil
}

// verifySolidityProof deploys the contract and verifies the proof on SimulatedBackend
// This function uses direct ABI parsing without needing abigen-generated bindings
func verifySolidityProof(c *qt.C, buildDir string, proof *davinci_solidity.Groth16CommitmentProof, inputs [8]*big.Int) bool {
	// Setup SimulatedBackend
	privKey, err := crypto.GenerateKey()
	if err != nil {
		c.Logf("Failed to generate key: %v", err)
		return false
	}

	chainID := big.NewInt(1337)
	deployer, err := bind.NewKeyedTransactorWithChainID(privKey, chainID)
	if err != nil {
		c.Logf("Failed to create transactor: %v", err)
		return false
	}

	alloc := ethtypes.GenesisAlloc{
		deployer.From:               {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(10))},
		common.HexToAddress("0x05"): {Balance: big.NewInt(1)}, // MODEXP
		common.HexToAddress("0x06"): {Balance: big.NewInt(1)}, // BN256ADD
		common.HexToAddress("0x07"): {Balance: big.NewInt(1)}, // BN256MUL
		common.HexToAddress("0x08"): {Balance: big.NewInt(1)}, // BN256PAIRING
	}

	sim := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(10_000_000))
	defer func() {
		if err := sim.Close(); err != nil {
			log.Printf("warning: failed to close simulated backend: %v", err)
		}
	}()

	// Read the compiled ABI and bytecode from Docker compilation output
	abiBytes, err := os.ReadFile(filepath.Join(buildDir, "Verifier.abi"))
	if err != nil {
		c.Logf("Failed to read ABI: %v", err)
		return false
	}

	bytecode, err := os.ReadFile(filepath.Join(buildDir, "Verifier.bin"))
	if err != nil {
		c.Logf("Failed to read bytecode: %v", err)
		return false
	}

	// Parse ABI
	parsed, err := abi.JSON(strings.NewReader(string(abiBytes)))
	if err != nil {
		c.Logf("Failed to parse ABI: %v", err)
		return false
	}

	// Deploy contract
	address, tx, _, err := bind.DeployContract(deployer, parsed, common.FromHex(string(bytecode)), sim.Client())
	if err != nil {
		c.Logf("Failed to deploy contract: %v", err)
		return false
	}
	sim.Commit()

	c.Logf("  Contract deployed at: %s (tx: %s)", address.Hex(), tx.Hash().Hex())

	// Prepare proof array for Solidity call
	var proofArray [8]*big.Int
	proofArray[0] = proof.Proof.Ar[0]
	proofArray[1] = proof.Proof.Ar[1]
	proofArray[2] = proof.Proof.Bs[0][0]
	proofArray[3] = proof.Proof.Bs[0][1]
	proofArray[4] = proof.Proof.Bs[1][0]
	proofArray[5] = proof.Proof.Bs[1][1]
	proofArray[6] = proof.Proof.Krs[0]
	proofArray[7] = proof.Proof.Krs[1]

	// Pack the call data for verifyProof
	callData, err := parsed.Pack("verifyProof", proofArray, proof.Commitments, proof.CommitmentPok, inputs)
	if err != nil {
		c.Logf("Failed to pack call data: %v", err)
		return false
	}

	// Call the contract using CallContract
	msg := ethereum.CallMsg{
		From: deployer.From,
		To:   &address,
		Data: callData,
	}

	result, err := sim.Client().CallContract(context.Background(), msg, nil)
	if err != nil {
		c.Logf("Verification call failed: %v", err)
		return false
	}

	// verifyProof is a view function that reverts on failure and returns nothing on success
	// If we got here without error, verification succeeded
	c.Logf("  Call succeeded, result length: %d bytes", len(result))
	return true
}

type CircuitCalculateAggregatorWitness struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitCalculateAggregatorWitness) Define(api frontend.API) error {
	mask := circuit.VoteMask(api)
	_, err := circuit.CalculateAggregatorWitness(api, mask)
	if err != nil {
		circuits.FrontendError(api, "failed to create bw6761 witness: ", err)
	}
	return nil
}

func TestCircuitCalculateAggregatorWitnessCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitCalculateAggregatorWitness{*CircuitPlaceholder()})
}

func TestCircuitCalculateAggregatorWitnessProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitCalculateAggregatorWitness{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitAggregatorProof struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitAggregatorProof) Define(api frontend.API) error {
	mask := circuit.VoteMask(api)
	circuit.VerifyAggregatorProof(api, mask)
	return nil
}

func TestCircuitAggregatorProofCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitAggregatorProof{*CircuitPlaceholder()})
}

func TestCircuitAggregatorProofProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitAggregatorProof{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitBallots struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitBallots) Define(api frontend.API) error {
	mask := circuit.VoteMask(api)
	circuit.VerifyBallots(api, mask)
	return nil
}

func TestCircuitBallotsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitBallots{*CircuitPlaceholder()})
}

func TestCircuitBallotsProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitBallots{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitMerkleProofs struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitMerkleProofs) Define(api frontend.API) error {
	circuit.VerifyMerkleProofs(api, statetransition.HashFn)
	return nil
}

func TestCircuitMerkleProofsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitMerkleProofs{*CircuitPlaceholder()})
}

func TestCircuitMerkleProofsProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitMerkleProofs{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitMerkleTransitions struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitMerkleTransitions) Define(api frontend.API) error {
	mask := circuit.VoteMask(api)
	circuit.VerifyMerkleTransitions(api, statetransition.HashFn, mask)
	return nil
}

func TestCircuitMerkleTransitionsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitMerkleTransitions{*CircuitPlaceholder()})
}

func TestCircuitMerkleTransitionsProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitMerkleTransitions{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
	if os.Getenv("DEBUG") != "" {
		debugLog(t, witness)
	}
}

type CircuitLeafHashes struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitLeafHashes) Define(api frontend.API) error {
	circuit.VerifyLeafHashes(api, statetransition.HashFn)
	return nil
}

func TestCircuitLeafHashesCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitLeafHashes{*CircuitPlaceholder()})
}

func TestCircuitLeafHashesProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitLeafHashes{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
	if os.Getenv("DEBUG") != "" {
		debugLog(t, witness)
	}
}

type CircuitReencryptBallots struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitReencryptBallots) Define(api frontend.API) error {
	mask := circuit.VoteMask(api)
	circuit.VerifyReencryptedVotes(api, mask)
	return nil
}

func TestCircuitReencryptBallotsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitReencryptBallots{
		*CircuitPlaceholder(),
	})
}

func TestCircuitReencryptBallotsProve(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitReencryptBallots{
		*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitCensusProofs struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitCensusProofs) Define(api frontend.API) error {
	mask := circuit.VoteMask(api)
	circuit.VerifyMerkleCensusProofs(api, mask)
	circuit.VerifyCSPCensusProofs(api, mask)
	return nil
}

func TestCircuitCensusProofsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitCensusProofs{
		*CircuitPlaceholder(),
	})
}

func TestCircuitCensusProofsProve(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	t.Run("MerkleTree", func(t *testing.T) {
		witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)

		testCircuitProve(t, &CircuitCensusProofs{
			*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
		}, witness)
	})

	t.Run("CSPEdDSABN254", func(t *testing.T) {
		witness := newMockWitness(t, types.CensusOriginCSPEdDSABN254V1)

		testCircuitProve(t, &CircuitCensusProofs{
			*CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
		}, witness)
	})
}

func newMockTransitionWithVotes(t *testing.T, s *state.State, votes ...state.Vote) *statetransition.StateTransitionCircuit {
	reencryptionK, err := elgamal.RandK()
	if err != nil {
		t.Fatal(err)
	}
	originalEncKey := s.EncryptionKey()
	encryptionKey := state.Curve.New().SetPoint(originalEncKey.PubKey[0], originalEncKey.PubKey[1])
	if err := s.StartBatch(); err != nil {
		t.Fatal(err)
	}
	lastK := new(big.Int).Set(reencryptionK)
	for _, v := range votes {
		v.ReencryptedBallot, lastK, err = v.Ballot.Reencrypt(encryptionKey, lastK)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddVote(&v); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.EndBatch(); err != nil {
		t.Fatal(err)
	}

	censusOrigin := types.CensusOrigin(s.CensusOrigin().Uint64())
	processID, err := types.ProcessIDFromBigInt(s.ProcessID())
	if err != nil {
		t.Fatal(err)
	}

	censusRoot, censusProofs, err := censustest.CensusProofsForCircuitTest(votes, censusOrigin, processID)
	if err != nil {
		t.Fatal(err)
	}

	witness, _, err := statetransition.GenerateWitness(
		s,
		new(types.BigInt).SetBigInt(censusRoot),
		censusProofs,
		new(types.BigInt).SetBigInt(reencryptionK))
	if err != nil {
		t.Fatal(err)
	}

	aggregatorHash, err := aggregatorWitnessHashForTest(s)
	if err != nil {
		t.Fatal(err)
	}

	proof, vk, err := DummyAggProof(len(votes), aggregatorHash)
	if err != nil {
		t.Fatal(err)
	}
	witness.AggregatorProof = *proof
	witness.AggregatorVK = *vk
	return witness
}

// newMockWitness returns a witness with an overwritten vote
func newMockWitness(t *testing.T, origin types.CensusOrigin) *statetransition.StateTransitionCircuit {
	// First initialize a state with a transition of 2 new votes,
	s := newMockState(t, origin)
	{
		reencryptionK, err := elgamal.RandK()
		if err != nil {
			t.Fatal(err)
		}
		originalEncKey := s.EncryptionKey()
		encryptionKey := state.Curve.New().SetPoint(originalEncKey.PubKey[0], originalEncKey.PubKey[1])
		if err := s.StartBatch(); err != nil {
			t.Fatal(err)
		}
		lastK := new(big.Int).Set(reencryptionK)
		votes := []state.Vote{
			newMockVote(s, 0, 10),
			newMockVote(s, 1, 10),
		}
		for _, v := range votes {
			v.ReencryptedBallot, lastK, err = v.Ballot.Reencrypt(encryptionKey, lastK)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.AddVote(&v); err != nil {
				t.Fatal(err)
			}
		}

		if err := s.EndBatch(); err != nil {
			t.Fatal(err)
		}
	}

	// so now we can return a transition where vote 1 is overwritten
	// and add 3 more votes
	return newMockTransitionWithVotes(t, s,
		newMockVote(s, 1, 20),
		newMockVote(s, 2, 20),
		newMockVote(s, 3, 20),
		newMockVote(s, 4, 20),
	)
}

func newMockState(t *testing.T, origin types.CensusOrigin) *state.State {
	s, err := state.New(metadb.NewTest(t), testutil.RandomProcessID())
	if err != nil {
		t.Fatal(err)
	}

	_, encryptionKey := testutil.RandomEncryptionKey()
	if err := s.Initialize(
		origin.BigInt().MathBigInt(),
		testutil.BallotMode(),
		encryptionKey,
	); err != nil {
		t.Fatal(err)
	}

	return s
}

const mockAddressesOffset = 200

func newMockAddress(index int64) *big.Int {
	return big.NewInt(index + int64(mockAddressesOffset)) // mock
}

// newMockVote creates a new vote
func newMockVote(s *state.State, index, amount int64) state.Vote {
	publicKey := state.Curve.New().SetPoint(s.EncryptionKey().PubKey[0], s.EncryptionKey().PubKey[1])

	fields := [types.FieldsPerBallot]*big.Int{}
	for i := range fields {
		fields[i] = big.NewInt(int64(amount + int64(i)))
	}

	ballot, err := elgamal.NewBallot(publicKey).Encrypt(fields, publicKey, nil)
	if err != nil {
		panic(fmt.Errorf("error encrypting: %v", err))
	}

	return state.Vote{
		Address: newMockAddress(index),
		VoteID:  util.RandomBytes(20),
		Weight:  big.NewInt(10),
		Ballot:  ballot,
	}
}

// aggregatorWitnessHashForTest uses the following values for each vote
//
//	process.ID
//	process.BallotMode
//	process.EncryptionKey
//	vote.Address
//	vote.Ballot
//
// to calculate a subhash of each process+vote, then hashes all subhashes
// and returns the final hash
func aggregatorWitnessHashForTest(o *state.State) (*big.Int, error) {
	hashes := []*big.Int{}
	for _, v := range o.PaddedVotes() {
		inputs := []*big.Int{}
		inputs = append(inputs, o.ProcessSerializeBigInts()...)
		inputs = append(inputs, v.SerializeBigInts()...)
		h, err := mimc7.Hash(inputs, nil)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	// calculate final hash
	finalHashInputs := []*big.Int{}
	for i := range types.VotesPerBatch {
		if i < o.VotersCount() {
			finalHashInputs = append(finalHashInputs, hashes[i])
		} else {
			finalHashInputs = append(finalHashInputs, big.NewInt(1))
		}
	}
	finalHash, err := mimc7.Hash(finalHashInputs, nil)
	if err != nil {
		return nil, err
	}

	return finalHash, nil
}

func debugLog(t *testing.T, witness *statetransition.StateTransitionCircuit) {
	t.Log("public: RootHashBefore", util.PrettyHex(witness.RootHashBefore))
	t.Log("public: RootHashAfter", util.PrettyHex(witness.RootHashAfter))
	t.Log("public: VotersCount", util.PrettyHex(witness.VotersCount))
	t.Log("public: OverwrittenVotesCount", util.PrettyHex(witness.OverwrittenVotesCount))
	for name, mts := range map[string][types.VotesPerBatch]merkleproof.MerkleTransition{
		"Ballot": witness.VotesProofs.Ballot,
	} {
		for _, mt := range mts {
			t.Log(name, "transitioned", "(root", util.PrettyHex(mt.OldRoot), "->", util.PrettyHex(mt.NewRoot), ")",
				"value", util.PrettyHex(mt.OldLeafHash), "->", util.PrettyHex(mt.NewLeafHash),
			)
		}
	}

	for name, mt := range map[string]merkleproof.MerkleTransition{
		"ResultsAdd": witness.ResultsProofs.ResultsAdd,
		"ResultsSub": witness.ResultsProofs.ResultsSub,
	} {
		t.Log(name, "transitioned", "(root", util.PrettyHex(mt.OldRoot), "->", util.PrettyHex(mt.NewRoot), ")",
			"value", util.PrettyHex(mt.OldLeafHash), "->", util.PrettyHex(mt.NewLeafHash),
		)
	}
}
