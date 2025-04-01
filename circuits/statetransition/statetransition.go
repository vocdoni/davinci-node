package statetransition

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/math/cmp"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/gnark-crypto-primitives/emulated/bn254/twistededwards/mimc7"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
)

var (
	HashFn           = utils.MiMCHasher
	AggregatorHashFn = MiMC7Hasher
)

// MiMC7Hasher function calculates the mimc7 hash of the provided inputs. It
// returns the hash of the inputs.
func MiMC7Hasher(api frontend.API, inputs ...emulated.Element[sw_bn254.ScalarField]) emulated.Element[sw_bn254.ScalarField] {
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create emulated MiMC hash function", err)
	}
	if err := hFn.Write(inputs...); err != nil {
		circuits.FrontendError(api, "failed to write inputs to emulated MiMC hash function", err)
	}
	return hFn.Sum()
}

type Circuit struct {
	// ---------------------------------------------------------------------------------------------
	// PUBLIC INPUTS

	RootHashBefore frontend.Variable `gnark:",public"`
	RootHashAfter  frontend.Variable `gnark:",public"`
	NumNewVotes    frontend.Variable `gnark:",public"`
	NumOverwrites  frontend.Variable `gnark:",public"`

	// ---------------------------------------------------------------------------------------------
	// SECRET INPUTS
	Process circuits.Process[frontend.Variable]
	Votes   [circuits.VotesPerBatch]Vote
	Results Results

	ProcessProofs ProcessProofs
	VotesProofs   VotesProofs
	ResultsProofs ResultsProofs

	AggregatorProof groth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine]
	AggregatorVK    groth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl] `gnark:"-"`
}

type Results struct {
	OldResultsAdd circuits.Ballot
	OldResultsSub circuits.Ballot
	NewResultsAdd circuits.Ballot
	NewResultsSub circuits.Ballot
}

type ProcessProofs struct {
	ID            MerkleProof
	CensusRoot    MerkleProof
	BallotMode    MerkleProof
	EncryptionKey MerkleProof
}

type VotesProofs struct {
	Ballot     [circuits.VotesPerBatch]MerkleTransition // Key is Nullifier, LeafHash is smt.Hash1(Ballot.Serialize())
	Commitment [circuits.VotesPerBatch]MerkleTransition // Key is Address, LeafHash is smt.Hash1(Commitment)
}

type ResultsProofs struct {
	ResultsAdd MerkleTransition
	ResultsSub MerkleTransition
}

type Vote struct {
	circuits.Vote[frontend.Variable]
	OverwrittenBallot circuits.Ballot
}

// Define declares the circuit's constraints
func (circuit Circuit) Define(api frontend.API) error {
	circuit.VerifyAggregatorProof(api)
	circuit.VerifyMerkleProofs(api, HashFn)
	circuit.VerifyMerkleTransitions(api, HashFn)
	circuit.VerifyLeafHashes(api, HashFn)
	circuit.VerifyBallots(api)
	return nil
}

func paddedElement(limb frontend.Variable) emulated.Element[sw_bw6761.ScalarField] {
	return emulated.Element[sw_bw6761.ScalarField]{
		Limbs: []frontend.Variable{limb, 0, 0, 0, 0, 0},
	}
}

func inputHashToElements(api frontend.API, isValid, inputsHash frontend.Variable) []emulated.Element[sw_bw6761.ScalarField] {
	voterHash, err := utils.UnpackVarToScalar[sw_bn254.ScalarField](api, inputsHash)
	if err != nil {
		return nil
	}
	finalElements := []emulated.Element[sw_bw6761.ScalarField]{}
	for l, limb := range voterHash.Limbs {
		dummyLimb := 0
		if l == 0 {
			dummyLimb = 1
		}
		finalLimb := api.Select(isValid, limb, dummyLimb)
		finalElements = append(finalElements, paddedElement(finalLimb))
	}
	return finalElements
}

func (c Circuit) proofInputsHash(api frontend.API, idx int) frontend.Variable {
	// init native mimc7 hash function
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create mimc7 hash function: ", err)
		return 0
	}
	// calculate the hash of the public inputs of the proof of the i-th vote
	if err := hFn.Write(circuits.VoteVerifierInputs(c.Process, c.Votes[idx].Vote)...); err != nil {
		circuits.FrontendError(api, "failed to write mimc7 hash function: ", err)
		return 0
	}
	// transform the hash to an emulated element of the bn254 curve
	return hFn.Sum()
}

func (c Circuit) CalculateAggregatorWitness(api frontend.API) (groth16.Witness[sw_bw6761.ScalarField], error) {
	// The Aggregator witness is the hash of the public inputs of the proof
	// of each vote that it aggregates. The public inputs of the proof of each
	// vote are composed by a bit that indicates if the vote is valid or not
	// as frontend.Variable, and the hash of the public-private inputs of the
	// proof, which is an emulated.Element[sw_bn254.ScalarField].
	// So, to calculate the witness we need to calculate each hash of the
	// public inputs of the proof of each vote (it can be done using native
	// mimc7 because this circuit should be work in the bn254 curve).

	// calculate the number of valid votes (it should be the number of new
	// votes plus the number of overwrites)
	validVotes := api.Add(c.NumNewVotes, c.NumOverwrites)
	// the witness should be a bw6761 element, and it should include the
	// number of valid votes as public input
	witness := groth16.Witness[sw_bw6761.ScalarField]{
		Public: []emulated.Element[sw_bw6761.ScalarField]{paddedElement(validVotes)},
	}
	// iterate over votes inputs to calculate the votes hashes and append them
	// to the witness
	for i := range circuits.VotesPerBatch {
		isValid := cmp.IsLess(api, i, validVotes)
		inputsHash := c.proofInputsHash(api, i)
		// transform the hash to an emulated element of the bn254 curve
		witness.Public = append(witness.Public, inputHashToElements(api, isValid, inputsHash)...)
	}
	return witness, nil
}

func (circuit Circuit) VerifyAggregatorProof(api frontend.API) {
	witness, err := circuit.CalculateAggregatorWitness(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create bw6761 witness: ", err)
	}
	// initialize the verifier
	verifier, err := groth16.NewVerifier[sw_bw6761.ScalarField, sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](api)
	if err != nil {
		circuits.FrontendError(api, "failed to create bw6761 verifier: ", err)
		return
	}
	// verify the proof with the hash as input and the fixed verification key
	if err := verifier.AssertProof(circuit.AggregatorVK, circuit.AggregatorProof, witness, groth16.WithCompleteArithmetic()); err != nil {
		circuits.FrontendError(api, "failed to verify aggregated proof: ", err)
		return
	}
}

func (circuit Circuit) VerifyMerkleProofs(api frontend.API, hFn utils.Hasher) {
	api.Println("verify ProcessID, CensusRoot, BallotMode and EncryptionKey belong to RootHashBefore")
	circuit.ProcessProofs.ID.Verify(api, hFn, circuit.RootHashBefore)
	circuit.ProcessProofs.CensusRoot.Verify(api, hFn, circuit.RootHashBefore)
	circuit.ProcessProofs.BallotMode.Verify(api, hFn, circuit.RootHashBefore)
	circuit.ProcessProofs.EncryptionKey.Verify(api, hFn, circuit.RootHashBefore)
}

func (circuit Circuit) VerifyMerkleTransitions(api frontend.API, hFn utils.Hasher) {
	// verify chain of tree transitions, order here is fundamental.
	api.Println("tree transition starts with RootHashBefore:", util.PrettyHex(circuit.RootHashBefore))
	root := circuit.RootHashBefore
	for i := range circuit.VotesProofs.Ballot {
		root = circuit.VotesProofs.Ballot[i].Verify(api, hFn, root)
	}
	for i := range circuit.VotesProofs.Commitment {
		root = circuit.VotesProofs.Commitment[i].Verify(api, hFn, root)
	}
	root = circuit.ResultsProofs.ResultsAdd.Verify(api, hFn, root)
	root = circuit.ResultsProofs.ResultsSub.Verify(api, hFn, root)
	api.Println("and final root is", util.PrettyHex(root), "should be equal to RootHashAfter", util.PrettyHex(circuit.RootHashAfter))
	api.AssertIsEqual(root, circuit.RootHashAfter)
}

func (circuit Circuit) VerifyLeafHashes(api frontend.API, hFn utils.Hasher) {
	// Process
	circuit.ProcessProofs.ID.VerifyLeafHash(api, hFn, circuit.Process.ID)
	circuit.ProcessProofs.CensusRoot.VerifyLeafHash(api, hFn, circuit.Process.CensusRoot)
	circuit.ProcessProofs.BallotMode.VerifyLeafHash(api, hFn, circuit.Process.BallotMode.Serialize()...)
	circuit.ProcessProofs.EncryptionKey.VerifyLeafHash(api, hFn, circuit.Process.EncryptionKey.Serialize()...)
	// Votes
	for i, v := range circuit.Votes {
		// Nullifier
		api.AssertIsEqual(v.Nullifier, circuit.VotesProofs.Ballot[i].NewKey)
		// Ballot
		circuit.VotesProofs.Ballot[i].VerifyNewLeafHash(api, hFn,
			v.Ballot.SerializeVars()...)
		// Address
		api.AssertIsEqual(v.Address, circuit.VotesProofs.Commitment[i].NewKey)
		// Commitment
		circuit.VotesProofs.Commitment[i].VerifyNewLeafHash(api, hFn,
			v.Commitment)
		// OverwrittenBallot
		circuit.VotesProofs.Ballot[i].VerifyOverwrittenBallot(api, hFn,
			v.OverwrittenBallot.SerializeVars()...)
	}
	// Results
	circuit.ResultsProofs.ResultsAdd.VerifyOldLeafHash(api, hFn,
		circuit.Results.OldResultsAdd.SerializeVars()...)
	circuit.ResultsProofs.ResultsSub.VerifyOldLeafHash(api, hFn,
		circuit.Results.OldResultsSub.SerializeVars()...)
	circuit.ResultsProofs.ResultsAdd.VerifyNewLeafHash(api, hFn,
		circuit.Results.NewResultsAdd.SerializeVars()...)
	circuit.ResultsProofs.ResultsSub.VerifyNewLeafHash(api, hFn,
		circuit.Results.NewResultsSub.SerializeVars()...)
}

// VerifyBallots counts the ballots using homomorphic encrpytion
func (circuit Circuit) VerifyBallots(api frontend.API) {
	ballotSum, overwrittenSum, zero := circuits.NewBallot(), circuits.NewBallot(), circuits.NewBallot()
	var ballotCount, overwrittenCount frontend.Variable = 0, 0

	for i, b := range circuit.VotesProofs.Ballot {
		ballotSum.Add(api, ballotSum,
			circuits.NewBallot().Select(api, b.IsInsertOrUpdate(api), &circuit.Votes[i].Ballot, zero))

		overwrittenSum.Add(api, overwrittenSum,
			circuits.NewBallot().Select(api, b.IsUpdate(api), &circuit.Votes[i].OverwrittenBallot, zero))

		ballotCount = api.Add(ballotCount, api.Select(b.IsInsertOrUpdate(api), 1, 0))
		overwrittenCount = api.Add(overwrittenCount, api.Select(b.IsUpdate(api), 1, 0))
	}

	circuit.Results.NewResultsAdd.AssertIsEqual(api,
		circuits.NewBallot().Add(api,
			&circuit.Results.OldResultsAdd,
			ballotSum))
	circuit.Results.NewResultsSub.AssertIsEqual(api,
		circuits.NewBallot().Add(api,
			&circuit.Results.OldResultsSub,
			overwrittenSum))
	api.AssertIsEqual(circuit.NumNewVotes, ballotCount)
	api.AssertIsEqual(circuit.NumOverwrites, overwrittenCount)
}

func (circuit Circuit) ListVotes() []circuits.Vote[frontend.Variable] {
	list := []circuits.Vote[frontend.Variable]{}
	for _, v := range circuit.Votes {
		list = append(list, v.Vote)
	}
	return list
}

func (circuit Circuit) ListVotesAsEmulated(api frontend.API) []circuits.EmulatedVote[sw_bn254.ScalarField] {
	list := []circuits.EmulatedVote[sw_bn254.ScalarField]{}
	for _, v := range circuit.Votes {
		list = append(list, v.ToEmulated(api))
	}
	return list
}
