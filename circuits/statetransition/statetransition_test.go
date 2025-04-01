package statetransition_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	"github.com/rs/zerolog"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/statetransition"
	statetransitiontest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/statetransition"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/util"

	"go.vocdoni.io/dvote/db/metadb"
)

const (
	falseStr = "false"
)

func testCircuitCompile(t *testing.T, c frontend.Circuit) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}
	// enable log to see nbConstraints
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	if _, err := frontend.Compile(circuits.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, c); err != nil {
		panic(err)
	}
}

func testCircuitProve(t *testing.T, circuit, witness frontend.Circuit) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	assert := test.NewAssert(t)
	assert.ProverSucceeded(
		circuit,
		witness,
		test.WithCurves(circuits.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))
}

func testCircuitExportSolidity(t *testing.T, c, w frontend.Circuit) {
	if os.Getenv("RELEASE_SOLIDITY") == "" || os.Getenv("RELEASE_SOLIDITY") == falseStr {
		t.Skip("skipping circuit tests...")
	}
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	assert := test.NewAssert(t)
	// generate witness
	fmt.Println("NewWitness") // debug
	witness, err := frontend.NewWitness(w, circuits.StateTransitionCurve.ScalarField())
	assert.NoError(err)
	// get public witness
	pubWitness, err := witness.Public()
	assert.NoError(err)
	// compile the circuit
	fmt.Println("Compile") // debug
	ccs, err := frontend.Compile(circuits.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, c)
	assert.NoError(err)
	// generate proving and verifying keys
	fmt.Println("Setup") // debug
	pk, vk, err := groth16.Setup(ccs)
	assert.NoError(err)
	// generate proof
	fmt.Println("Prove") // debug
	proof, err := groth16.Prove(ccs, pk, witness)
	assert.NoError(err)
	// write proof into a buffer
	var buf bytes.Buffer
	_, err = proof.WriteRawTo(&buf)
	assert.NoError(err)
	proofBytes := buf.Bytes()
	// compose the proof for solidity
	type SolidityProof struct {
		Ar  [2]*big.Int    `json:"Ar"`
		Bs  [2][2]*big.Int `json:"Bs"`
		Krs [2]*big.Int    `json:"Krs"`
	}
	p := SolidityProof{}
	// proof.Ar, proof.Bs, proof.Krs
	const fpSize = 4 * 8
	p.Ar[0] = new(big.Int).SetBytes(proofBytes[fpSize*0 : fpSize*1])
	p.Ar[1] = new(big.Int).SetBytes(proofBytes[fpSize*1 : fpSize*2])
	p.Bs[0][0] = new(big.Int).SetBytes(proofBytes[fpSize*2 : fpSize*3])
	p.Bs[0][1] = new(big.Int).SetBytes(proofBytes[fpSize*3 : fpSize*4])
	p.Bs[1][0] = new(big.Int).SetBytes(proofBytes[fpSize*4 : fpSize*5])
	p.Bs[1][1] = new(big.Int).SetBytes(proofBytes[fpSize*5 : fpSize*6])
	p.Krs[0] = new(big.Int).SetBytes(proofBytes[fpSize*6 : fpSize*7])
	p.Krs[1] = new(big.Int).SetBytes(proofBytes[fpSize*7 : fpSize*8])
	// write proof into a file
	prooffd, err := os.Create("statetransition_proof.json")
	assert.NoError(err)
	defer func() {
		if err := prooffd.Close(); err != nil {
			t.Errorf("error closing proof file: %v", err)
		}
	}()
	bProof, err := json.Marshal(p)
	assert.NoError(err)
	_, err = prooffd.Write(bProof)
	assert.NoError(err)
	// generate solidity verifier
	solfd, err := os.Create("statetransition_verifier.sol")
	assert.NoError(err)
	defer func() {
		if err := solfd.Close(); err != nil {
			t.Errorf("error closing solidity file: %v", err)
		}
	}()
	// write verifier
	err = vk.ExportSolidity(solfd)
	assert.NoError(err)
	// generate also the json of the public witness
	schema, err := frontend.NewSchema(w)
	assert.NoError(err)
	jsonWitness, err := pubWitness.ToJSON(schema)
	assert.NoError(err)
	pubWitnessJSONfd, err := os.Create("statetransition_public_witness.json")
	assert.NoError(err)
	defer func() {
		if err := pubWitnessJSONfd.Close(); err != nil {
			t.Errorf("error closing public witness JSON file: %v", err)
		}
	}()
	_, err = pubWitnessJSONfd.Write(jsonWitness)
	assert.NoError(err)
}

func TestCircuitExportSolidity(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitExportSolidity(t,
		statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
		witness)
}

func TestCircuitCompile(t *testing.T) {
	testCircuitCompile(t, statetransitiontest.CircuitPlaceholder())
}

func TestCircuitProve(t *testing.T) {
	s := newMockState(t)
	{
		witness := newMockTransitionWithVotes(t, s,
			newMockVote(1, 10), // add vote 1
			newMockVote(2, 20), // add vote 2
		)
		testCircuitProve(t, statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK), witness)

		debugLog(t, witness)
	}
	{
		witness := newMockTransitionWithVotes(t, s,
			newMockVote(1, 100), // overwrite vote 1
			newMockVote(3, 30),  // add vote 3
			newMockVote(4, 40),  // add vote 4
		)
		testCircuitProve(t, statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK), witness)

		debugLog(t, witness)
	}
}

type CircuitCalculateAggregatorWitness struct {
	statetransition.Circuit
}

func (circuit CircuitCalculateAggregatorWitness) Define(api frontend.API) error {
	_, err := circuit.CalculateAggregatorWitness(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create bw6761 witness: ", err)
	}
	return nil
}

func TestCircuitCalculateAggregatorWitnessCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitCalculateAggregatorWitness{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitCalculateAggregatorWitnessProve(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitProve(t, &CircuitCalculateAggregatorWitness{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitAggregatorProof struct {
	statetransition.Circuit
}

func (circuit CircuitAggregatorProof) Define(api frontend.API) error {
	circuit.VerifyAggregatorProof(api)
	return nil
}

func TestCircuitAggregatorProofCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitAggregatorProof{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitAggregatorProofProve(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitProve(t, &CircuitAggregatorProof{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitBallots struct {
	statetransition.Circuit
}

func (circuit CircuitBallots) Define(api frontend.API) error {
	circuit.VerifyBallots(api)
	return nil
}

func TestCircuitBallotsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitBallots{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitBallotsProve(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitProve(t, &CircuitBallots{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitMerkleProofs struct {
	statetransition.Circuit
}

func (circuit CircuitMerkleProofs) Define(api frontend.API) error {
	circuit.VerifyMerkleProofs(api, statetransition.HashFn)
	return nil
}

func TestCircuitMerkleProofsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitMerkleProofs{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitMerkleProofsProve(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitProve(t, &CircuitMerkleProofs{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitMerkleTransitions struct {
	statetransition.Circuit
}

func (circuit CircuitMerkleTransitions) Define(api frontend.API) error {
	circuit.VerifyMerkleTransitions(api, statetransition.HashFn)
	return nil
}

func TestCircuitMerkleTransitionsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitMerkleTransitions{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitMerkleTransitionsProve(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitProve(t, &CircuitMerkleTransitions{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)

	debugLog(t, witness)
}

type CircuitLeafHashes struct {
	statetransition.Circuit
}

func (circuit CircuitLeafHashes) Define(api frontend.API) error {
	circuit.VerifyLeafHashes(api, statetransition.HashFn)
	return nil
}

func TestCircuitLeafHashesCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitLeafHashes{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitLeafHashesProve(t *testing.T) {
	witness := newMockWitness(t)
	testCircuitProve(t, &CircuitLeafHashes{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)

	debugLog(t, witness)
}

func newMockTransitionWithVotes(t *testing.T, s *state.State, votes ...*state.Vote) *statetransition.Circuit {
	if err := s.StartBatch(); err != nil {
		t.Fatal(err)
	}

	for _, v := range votes {
		if err := s.AddVote(v); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.EndBatch(); err != nil {
		t.Fatal(err)
	}

	witness, err := statetransitiontest.GenerateWitness(s)
	if err != nil {
		t.Fatal(err)
	}

	inputsHash, err := s.AggregatorWitnessHash()
	if err != nil {
		t.Fatal(err)
	}

	proof, vk, err := statetransitiontest.DummyAggProof(inputsHash, s.BallotCount())
	if err != nil {
		t.Fatal(err)
	}
	witness.AggregatorProof = *proof
	witness.AggregatorVK = *vk
	return witness
}

func newMockWitness(t *testing.T) *statetransition.Circuit {
	return newMockTransitionWithVotes(t, newMockState(t),
		newMockVote(1, 10),
		newMockVote(2, 20),
	)
}

func newMockState(t *testing.T) *state.State {
	s, err := state.New(metadb.NewTest(t),
		new(big.Int).SetBytes(util.RandomBytes(16)))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Initialize(
		new(big.Int).SetBytes(util.RandomBytes(16)),
		circuits.MockBallotMode(),
		circuits.MockEncryptionKey(),
	); err != nil {
		t.Fatal(err)
	}

	return s
}

const (
	mockNullifiersOffset = 100
	mockAddressesOffset  = 200
	// maxKeyLen is ceil(maxLevels/8)
	maxKeyLen = (circuits.CensusTreeMaxLevels + 7) / 8
)

// newMockVote creates a new vote
func newMockVote(index, amount int64) *state.Vote {
	nullifier := big.NewInt(int64(index) + int64(mockNullifiersOffset)) // mock

	// generate a public mocked key
	publicKey, _, err := elgamal.GenerateKey(state.Curve)
	if err != nil {
		panic(fmt.Errorf("error generating public key: %v", err))
	}

	fields := [circuits.FieldsPerBallot]*big.Int{}
	for i := range fields {
		fields[i] = big.NewInt(int64(amount + int64(i)))
	}

	ballot, err := elgamal.NewBallot(publicKey).Encrypt(fields, publicKey, nil)
	if err != nil {
		panic(fmt.Errorf("error encrypting: %v", err))
	}

	address := big.NewInt(int64(index) + int64(mockAddressesOffset)) // mock
	commitment := big.NewInt(amount + 256)

	return &state.Vote{
		Nullifier:  nullifier,
		Ballot:     ballot,
		Address:    address,
		Commitment: commitment,
	}
}

func debugLog(t *testing.T, witness *statetransition.Circuit) {
	// js, _ := json.MarshalIndent(witness, "", "  ")
	// fmt.Printf("\n\n%s\n\n", js)
	t.Log("public: RootHashBefore", util.PrettyHex(witness.RootHashBefore))
	t.Log("public: RootHashAfter", util.PrettyHex(witness.RootHashAfter))
	t.Log("public: NumVotes", util.PrettyHex(witness.NumNewVotes))
	t.Log("public: NumOverwrites", util.PrettyHex(witness.NumOverwrites))
	for name, mts := range map[string][circuits.VotesPerBatch]statetransition.MerkleTransition{
		"Ballot":     witness.VotesProofs.Ballot,
		"Commitment": witness.VotesProofs.Commitment,
	} {
		for _, mt := range mts {
			t.Log(name, "transitioned", "(root", util.PrettyHex(mt.OldRoot), "->", util.PrettyHex(mt.NewRoot), ")",
				"value", util.PrettyHex(mt.OldLeafHash), "->", util.PrettyHex(mt.NewLeafHash),
			)
		}
	}

	for name, mt := range map[string]statetransition.MerkleTransition{
		"ResultsAdd": witness.ResultsProofs.ResultsAdd,
		"ResultsSub": witness.ResultsProofs.ResultsSub,
	} {
		t.Log(name, "transitioned", "(root", util.PrettyHex(mt.OldRoot), "->", util.PrettyHex(mt.NewRoot), ")",
			"value", util.PrettyHex(mt.OldLeafHash), "->", util.PrettyHex(mt.NewLeafHash),
		)
	}
}
