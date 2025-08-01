package statetransitiontest

import (
	"fmt"
	"math/big"
	"os"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	aggregatortest "github.com/vocdoni/davinci-node/circuits/test/aggregator"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// StateTransitionTestResults struct includes relevant data after StateTransitionCircuit
// inputs generation
type StateTransitionTestResults struct {
	Process circuits.Process[*big.Int]
	Votes   []state.Vote
}

// StateTransitionInputsForTest returns the StateTransitionTestResults, the placeholder
// and the assignments of a StateTransitionCircuit for the processId provided
// generating nValidVoters. If something fails it returns an error.
func StateTransitionInputsForTest(processId *types.ProcessID, nValidVoters int) (
	*StateTransitionTestResults, *statetransition.StateTransitionCircuit, *statetransition.StateTransitionCircuit, error,
) {
	// generate aggregator circuit and inputs
	agInputs, agPlaceholder, agWitness, err := aggregatortest.AggregatorInputsForTest(processId, nValidVoters)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregator inputs: %w", err)
	}
	// compile aggregator circuit
	agCCS, err := frontend.Compile(circuits.AggregatorCurve.ScalarField(), r1cs.NewBuilder, agPlaceholder)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregator compile: %w", err)
	}
	agPk, agVk, err := groth16.Setup(agCCS)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregator setup: %w", err)
	}
	// parse the witness to the circuit
	fullWitness, err := frontend.NewWitness(agWitness, circuits.AggregatorCurve.ScalarField())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregator witness: %w", err)
	}
	// generate the proof
	proof, err := groth16.Prove(agCCS, agPk, fullWitness, stdgroth16.GetNativeProverOptions(
		circuits.StateTransitionCurve.ScalarField(),
		circuits.AggregatorCurve.ScalarField()))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("err proving aggregator circuit: %w", err)
	}
	// convert the proof to the circuit proof type
	proofInBW6761, err := stdgroth16.ValueOfProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](proof)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("convert aggregator proof: %w", err)
	}
	// convert the public inputs to the circuit public inputs type
	publicWitness, err := fullWitness.Public()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("convert aggregator public inputs: %w", err)
	}
	err = groth16.Verify(proof, agVk, publicWitness, stdgroth16.GetNativeVerifierOptions(
		circuits.StateTransitionCurve.ScalarField(),
		circuits.AggregatorCurve.ScalarField()))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregator verify: %w", err)
	}
	// reencrypt the votes
	reencryptionK, err := elgamal.RandK()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate random k: %w", err)
	}
	// get the encryption key from the aggregator inputs
	encryptionKey := state.Curve.New().SetPoint(agInputs.Process.EncryptionKey.PubKey[0], agInputs.Process.EncryptionKey.PubKey[1])
	// init final assignments stuff
	s := newState(agInputs.Process.ID, agInputs.Process.CensusRoot,
		circuits.MockBallotMode(), agInputs.Process.EncryptionKey)

	if err := s.StartBatch(); err != nil {
		return nil, nil, nil, fmt.Errorf("start batch: %w", err)
	}
	// iterate over the votes, reencrypting each time the zero ballot with the
	// correct k value and adding it to the state
	lastK := new(big.Int).Set(reencryptionK)
	for _, v := range agInputs.Votes {
		v.ReencryptedBallot, lastK, err = v.Ballot.Reencrypt(encryptionKey, lastK)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to reencrypt ballot: %w", err)
		}
		if err := s.AddVote(&v); err != nil {
			return nil, nil, nil, fmt.Errorf("add vote: %w", err)
		}
	}
	if err := s.EndBatch(); err != nil {
		return nil, nil, nil, fmt.Errorf("end batch: %w", err)
	}
	witness, err := statetransition.GenerateWitness(s, new(types.BigInt).SetBigInt(reencryptionK))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate witness: %w", err)
	}
	witness.AggregatorProof = proofInBW6761

	// create final placeholder
	circuitPlaceholder := CircuitPlaceholder()
	// fix the vote verifier verification key
	fixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](agVk)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregator vk: %w", err)
	}
	circuitPlaceholder.AggregatorVK = fixedVk
	return &StateTransitionTestResults{
		Process: agInputs.Process,
		Votes:   agInputs.Votes,
	}, circuitPlaceholder, witness, nil
}

func newState(processId, censusRoot *big.Int, ballotMode circuits.BallotMode[*big.Int],
	encryptionKey circuits.EncryptionKey[*big.Int],
) *state.State {
	dir, err := os.MkdirTemp(os.TempDir(), "statetransition")
	if err != nil {
		panic(err)
	}
	db, err := metadb.New("pebble", dir)
	if err != nil {
		panic(err)
	}
	s, err := state.New(db, processId)
	if err != nil {
		panic(err)
	}

	if err := s.Initialize(
		types.CensusOriginMerkleTree.BigInt().MathBigInt(),
		censusRoot,
		ballotMode,
		encryptionKey,
	); err != nil {
		panic(err)
	}

	return s
}
