package statetransition_test

import (
	"fmt"
	"log"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/rs/zerolog"
	censustest "github.com/vocdoni/davinci-node/census/test"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	statetransitiontest "github.com/vocdoni/davinci-node/circuits/test/statetransition"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	dlog "github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"

	"github.com/vocdoni/davinci-node/db/metadb"
)

func TestMain(m *testing.M) {
	dlog.Init(dlog.LogLevelDebug, "stdout", nil)
	// enable log to see nbConstraints
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())
	os.Exit(m.Run())
}

const falseStr = "false"

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
	assert := test.NewAssert(t)
	assert.ProverSucceeded(
		circuit,
		witness,
		test.WithCurves(circuits.StateTransitionCurve),
		test.WithBackends(backend.GROTH16))
}

func TestCircuitCompile(t *testing.T) {
	testCircuitCompile(t, statetransitiontest.CircuitPlaceholder())
}

func TestCircuitProve(t *testing.T) {
	s := newMockState(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	{
		witness := newMockTransitionWithVotes(t, s,
			newMockVote(s, 1, 10), // add vote 1
			newMockVote(s, 2, 20), // add vote 2
		)
		testCircuitProve(t, statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK), witness)

		debugLog(t, witness)
	}
	{
		witness := newMockTransitionWithVotes(t, s,
			newMockVote(s, 1, 100), // overwrite vote 1
			newMockVote(s, 3, 30),  // add vote 3
			newMockVote(s, 4, 40),  // add vote 4
		)
		testCircuitProve(t, statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK), witness)

		debugLog(t, witness)
	}
}

type CircuitCalculateAggregatorWitness struct {
	statetransition.StateTransitionCircuit
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
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitCalculateAggregatorWitness{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitAggregatorProof struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitAggregatorProof) Define(api frontend.API) error {
	circuit.VerifyAggregatorProof(api)
	return nil
}

func TestCircuitAggregatorProofCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitAggregatorProof{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitAggregatorProofProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitAggregatorProof{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitBallots struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitBallots) Define(api frontend.API) error {
	circuit.VerifyBallots(api)
	return nil
}

func TestCircuitBallotsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitBallots{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitBallotsProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitBallots{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
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
	testCircuitCompile(t, &CircuitMerkleProofs{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitMerkleProofsProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitMerkleProofs{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitMerkleTransitions struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitMerkleTransitions) Define(api frontend.API) error {
	circuit.VerifyMerkleTransitions(api, statetransition.HashFn)
	return nil
}

func TestCircuitMerkleTransitionsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitMerkleTransitions{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitMerkleTransitionsProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitMerkleTransitions{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)

	debugLog(t, witness)
}

type CircuitLeafHashes struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitLeafHashes) Define(api frontend.API) error {
	circuit.VerifyLeafHashes(api, statetransition.HashFn)
	return nil
}

func TestCircuitLeafHashesCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitLeafHashes{*statetransitiontest.CircuitPlaceholder()})
}

func TestCircuitLeafHashesProve(t *testing.T) {
	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitLeafHashes{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)

	debugLog(t, witness)
}

type CircuitReencryptBallots struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitReencryptBallots) Define(api frontend.API) error {
	circuit.VerifyReencryptedVotes(api)
	return nil
}

func TestCircuitReencryptBallotsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitReencryptBallots{
		*statetransitiontest.CircuitPlaceholder(),
	})
}

func TestCircuitReencryptBallotsProve(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)
	testCircuitProve(t, &CircuitReencryptBallots{
		*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
	}, witness)
}

type CircuitCensusProofs struct {
	statetransition.StateTransitionCircuit
}

func (circuit CircuitCensusProofs) Define(api frontend.API) error {
	circuit.VerifyMerkleCensusProofs(api)
	circuit.VerifyCSPCensusProofs(api)
	return nil
}

func TestCircuitCensusProofsCompile(t *testing.T) {
	testCircuitCompile(t, &CircuitCensusProofs{
		*statetransitiontest.CircuitPlaceholder(),
	})
}

func TestCircuitCensusProofsProve(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	t.Run("MerkleTree", func(t *testing.T) {
		witness := newMockWitness(t, types.CensusOriginMerkleTreeOffchainStaticV1)

		testCircuitProve(t, &CircuitCensusProofs{
			*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
		}, witness)
	})

	t.Run("CSPEdDSABN254", func(t *testing.T) {
		witness := newMockWitness(t, types.CensusOriginCSPEdDSABN254V1)

		testCircuitProve(t, &CircuitCensusProofs{
			*statetransitiontest.CircuitPlaceholderWithProof(&witness.AggregatorProof, &witness.AggregatorVK),
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
	pid := new(types.ProcessID).SetBytes(s.ProcessID().Bytes())

	censusRoot, censusProofs, err := censustest.CensusProofsForCircuitTest(votes, censusOrigin, pid)
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

	proof, vk, err := statetransitiontest.DummyAggProof(len(votes), aggregatorHash)
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
	_ = newMockTransitionWithVotes(t, s,
		newMockVote(s, 0, 10),
		newMockVote(s, 1, 10),
	)
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
	pid := &types.ProcessID{
		Address: common.BytesToAddress(util.RandomBytes(20)),
		Version: types.ProcessIDVersion(1, common.BytesToAddress(util.RandomBytes(20))),
		Nonce:   1,
	}
	s, err := state.New(metadb.NewTest(t), pid.BigInt())
	if err != nil {
		t.Fatal(err)
	}

	_, encryptionKey := circuits.MockEncryptionKey()
	if err := s.Initialize(
		origin.BigInt().MathBigInt(),
		circuits.MockBallotMode(),
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
		if i < o.BallotCount() {
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
	t.Log("public: NumVotes", util.PrettyHex(witness.NumNewVotes))
	t.Log("public: NumOverwritten", util.PrettyHex(witness.NumOverwritten))
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
