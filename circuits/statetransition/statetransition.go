package statetransition

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/math/cmp"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/mimc7"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
)

// HashFn is the hash function used in the circuit. It should the equivalent
// hash function used in the state package (state.HashFn).
var HashFn = poseidon.MultiHash

type StateTransitionCircuit struct {
	// Public inputs
	RootHashBefore frontend.Variable `gnark:",public"`
	RootHashAfter  frontend.Variable `gnark:",public"`
	NumNewVotes    frontend.Variable `gnark:",public"`
	NumOverwritten frontend.Variable `gnark:",public"`
	// Private data inputs
	Process    circuits.Process[frontend.Variable]
	Votes      [types.VotesPerBatch]Vote
	Results    Results
	ReencryptK frontend.Variable
	// Private merkle proofs inputs
	ProcessProofs ProcessProofs
	VotesProofs   VotesProofs
	ResultsProofs ResultsProofs
	// Private recursive proof inputs
	AggregatorProof groth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine]
	AggregatorVK    groth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl] `gnark:"-"`
}

// Results struct contains the ballot structs for addition and subtraction
// after and before the aggregation.
type Results struct {
	OldResultsAdd circuits.Ballot
	OldResultsSub circuits.Ballot
	NewResultsAdd circuits.Ballot
	NewResultsSub circuits.Ballot
}

// ProcessProofs struct contains the Merkle proofs for the process for the ID
// CensusRoot, BallotMode and EncryptionKey.
type ProcessProofs struct {
	ID            merkleproof.MerkleProof
	CensusRoot    merkleproof.MerkleProof
	BallotMode    merkleproof.MerkleProof
	EncryptionKey merkleproof.MerkleProof
}

// VotesProofs struct contains the Merkle transition proofs for the ballots and
// commitments.
type VotesProofs struct {
	// Key is Address, LeafHash is smt.Hash1(encoded(Ballot.Serialize()))
	Ballot  [types.VotesPerBatch]merkleproof.MerkleTransition
	VoteIDs [types.VotesPerBatch]merkleproof.MerkleTransition
}

// ResultsProofs struct contains the Merkle transition proofs for the addition
// and subtraction of the results.
type ResultsProofs struct {
	ResultsAdd merkleproof.MerkleTransition
	ResultsSub merkleproof.MerkleTransition
}

// Vote struct contains the circuits.Vote struct and the overwritten ballot.
type Vote struct {
	circuits.Vote[frontend.Variable]
	ReencryptedBallot circuits.Ballot
	OverwrittenBallot circuits.Ballot
}

// Define declares the circuit's constraints
func (circuit StateTransitionCircuit) Define(api frontend.API) error {
	circuit.VerifyAggregatorProof(api)
	circuit.VerifyReencryptedVotes(api)
	circuit.VerifyMerkleProofs(api, HashFn)
	circuit.VerifyMerkleTransitions(api, HashFn)
	circuit.VerifyLeafHashes(api, HashFn)
	circuit.VerifyBallots(api)
	return nil
}

// paddedElement helper function returns an bw6761 curve emulated element with
// the limb provided as the first limb and the rest as 0. This is used to
// transform the mimc7 hash output to an emulated element of the bw6761 curve.
func paddedElement(limb frontend.Variable) emulated.Element[sw_bw6761.ScalarField] {
	return emulated.Element[sw_bw6761.ScalarField]{
		Limbs: []frontend.Variable{limb, 0, 0, 0, 0, 0},
	}
}

// inputHashToElements transforms the mimc7 hash output to an array of emulated
// elements of the bw6761 curve. It transform the hash output to an emulated
// element of the bn254 curve, and then split each limb of the element to single
// emulated element of the bw6761 curve. Each bn254 limb will be placed as the
// first limb of the resulting bw6761 elements, and the rest of the limbs
// will be set to 0.
func inputsHashToElements(api frontend.API, inputsHash frontend.Variable) []emulated.Element[sw_bw6761.ScalarField] {
	voterHash, err := utils.UnpackVarToScalar[sw_bn254.ScalarField](api, inputsHash)
	if err != nil {
		return nil
	}
	finalElements := []emulated.Element[sw_bw6761.ScalarField]{}
	for _, limb := range voterHash.Limbs {
		finalElements = append(finalElements, paddedElement(limb))
	}
	return finalElements
}

// proofInputsHash calculates the mimc7 hash of the public inputs of the proof
// of the i-th vote. It uses the native mimc7 hash function to calculate the
// hash, and then transform the hash to an emulated element of the bw6761 curve.
// The hash is calculated using the public inputs of the proof of the i-th vote.
func (c StateTransitionCircuit) proofInputsHash(api frontend.API, idx int) frontend.Variable {
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

// CalculateAggregatorWitness calculates the witness for the Aggregator proof.
// The Aggregator witness is the hash of the public inputs of the proof of each
// vote that it aggregates. The public inputs of the proof of each vote are
// composed by the hash of the public-private inputs of the proof, which is an
// emulated.Element[sw_bn254.ScalarField]. To calculate the witness we need to
// calculate each hash of the public inputs of the proof of each vote (it can
// be done using native mimc7 because this circuit should be work in the bn254
// curve). But the witness should be an emulated element of the bw6761 curve,
// that contains the hash as a emulated element of the bn254 curve. So we need
// to transform the hash, first to an emulated element of the bn254 curve,
// and then to an emulated element of the bw6761 curve.
func (c StateTransitionCircuit) CalculateAggregatorWitness(api frontend.API) (groth16.Witness[sw_bw6761.ScalarField], error) {
	// the witness should be a bw6761 element, and it should include the
	// number of valid votes as public input
	witness := groth16.Witness[sw_bw6761.ScalarField]{
		Public: []emulated.Element[sw_bw6761.ScalarField]{paddedElement(c.NumNewVotes)},
	}
	// iterate over votes inputs to select between valid hashes and dummy ones
	hashes := []frontend.Variable{}
	for i := range types.VotesPerBatch {
		isValid := cmp.IsLess(api, i, c.NumNewVotes)
		inputsHash := c.proofInputsHash(api, i)
		hashes = append(hashes, api.Select(isValid, inputsHash, 1))
	}
	// hash the inputs hashes to get the final witness
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		return groth16.Witness[sw_bw6761.ScalarField]{}, err
	}
	if err := hFn.Write(hashes...); err != nil {
		return groth16.Witness[sw_bw6761.ScalarField]{}, err
	}
	// include the inputs hash in the witness as elements of the bw6761
	witness.Public = append(witness.Public, inputsHashToElements(api, hFn.Sum())...)
	return witness, nil
}

// VerifyAggregatorProof verifies the Aggregator proof using the witness
// calculated by the CalculateAggregatorWitness function. It uses the
// groth16 verifier to verify the proof. The proof is verified using the
// AggregatorVK, which is the verification key of the Aggregator proof.
func (circuit StateTransitionCircuit) VerifyAggregatorProof(api frontend.API) {
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

// VerifyReencryptedVotes reencrypts the votes using the reencryptK and checks
// if the result is equal to the reencrypted ballot provided as input. To
// reencrypt the votes, it adds the encrypted zero ballot to the original
// ballot. The encrypted zero uses the reencryptK as the randomness.
func (circuit StateTransitionCircuit) VerifyReencryptedVotes(api frontend.API) {
	lastK := frontend.Variable(circuit.ReencryptK)
	api.Println("encryption key", circuit.Process.EncryptionKey.PubKey[0], circuit.Process.EncryptionKey.PubKey[1])
	for i, v := range circuit.Votes {
		api.Println("Verifying reencrypted vote", i)
		isValid := cmp.IsLess(api, i, circuit.NumNewVotes)
		var err error
		var reencryptedBallot *circuits.Ballot
		reencryptedBallot, lastK, err = v.Ballot.Reencrypt(api, circuit.Process.EncryptionKey, lastK)
		if err != nil {
			circuits.FrontendError(api, "failed to reencrypt ballot: ", err)
			return
		}
		isEqual := v.ReencryptedBallot.IsEqual(api, reencryptedBallot)
		api.AssertIsEqual(api.Select(isValid, isEqual, 1), 1)
	}
}

// VerifyMerkleProofs verifies that the ProcessID, CensusRoot, BallotMode
// and EncryptionKey belong to the RootHashBefore. It uses the MerkleProof
// structure to verify the proofs. The proofs are verified using the Verify
// function of the MerkleProof structure.
func (circuit StateTransitionCircuit) VerifyMerkleProofs(api frontend.API, hFn utils.Hasher) {
	circuit.ProcessProofs.ID.Verify(api, hFn, circuit.RootHashBefore)
	circuit.ProcessProofs.CensusRoot.Verify(api, hFn, circuit.RootHashBefore)
	circuit.ProcessProofs.BallotMode.Verify(api, hFn, circuit.RootHashBefore)
	circuit.ProcessProofs.EncryptionKey.Verify(api, hFn, circuit.RootHashBefore)
}

// VerifyMerkleTransitions verifies that the chain of tree transitions is valid.
// It first verifies the chain of tree transitions of the ballots, then the
// chain of tree transitions of the commitments, and finally the chain of tree
// transitions of the results. The order of the transitions is fundamental to
// achieve the final root hash.
func (circuit StateTransitionCircuit) VerifyMerkleTransitions(api frontend.API, hFn utils.Hasher) {
	// verify chain of tree transitions, order here is fundamental.
	root := circuit.RootHashBefore
	for i := range circuit.VotesProofs.Ballot {
		root = circuit.VotesProofs.Ballot[i].Verify(api, hFn, root)
		root = circuit.VotesProofs.VoteIDs[i].Verify(api, hFn, root)
	}
	root = circuit.ResultsProofs.ResultsAdd.Verify(api, hFn, root)
	root = circuit.ResultsProofs.ResultsSub.Verify(api, hFn, root)
	api.AssertIsEqual(root, circuit.RootHashAfter)
}

// VerifyLeafHashes verifies that the leaf hashes of the process, votes and
// results are correct. It verifies that the leaf hashes of the process, votes
// and results are equal to the leaf hashes of the proofs. It uses the
// VerifyLeafHash function of the MerkleProof structure to verify the leaf
// hashes.
func (circuit StateTransitionCircuit) VerifyLeafHashes(api frontend.API, hFn utils.Hasher) {
	// Process
	if err := circuit.ProcessProofs.ID.VerifyLeafHash(api, hFn, circuit.Process.ID); err != nil {
		circuits.FrontendError(api, "failed to verify process id process proof leaf hash: ", err)
		return
	}
	if err := circuit.ProcessProofs.CensusRoot.VerifyLeafHash(api, hFn, circuit.Process.CensusRoot); err != nil {
		circuits.FrontendError(api, "failed to verify census root process proof leaf hash: ", err)
		return
	}
	if err := circuit.ProcessProofs.BallotMode.VerifyLeafHash(api, hFn, circuit.Process.BallotMode.Serialize()...); err != nil {
		circuits.FrontendError(api, "failed to verify ballot mode process proof leaf hash: ", err)
		return
	}
	if err := circuit.ProcessProofs.EncryptionKey.VerifyLeafHash(api, hFn, circuit.Process.EncryptionKey.Serialize()...); err != nil {
		circuits.FrontendError(api, "failed to verify encryption key process proof leaf hash: ", err)
		return
	}
	// Votes
	for i, v := range circuit.Votes {
		// Address
		addressKey, err := merkleproof.TruncateMerkleTreeKey(api, v.Address, types.StateKeyMaxLen)
		if err != nil {
			circuits.FrontendError(api, "failed to truncate address key: ", err)
			return
		}
		api.AssertIsEqual(addressKey, circuit.VotesProofs.Ballot[i].NewKey)
		// Ballot
		if err := circuit.VotesProofs.Ballot[i].VerifyNewLeafHash(api, hFn, v.ReencryptedBallot.SerializeVars()...); err != nil {
			circuits.FrontendError(api, "failed to verify ballot vote proof leaf hash: ", err)
			return
		}
		// OverwrittenBallot
		if err := circuit.VotesProofs.Ballot[i].VerifyOverwrittenBallot(api, hFn, v.OverwrittenBallot.SerializeVars()...); err != nil {
			circuits.FrontendError(api, "failed to verify ballot vote proof leaf hash: ", err)
			return
		}
	}
	// Results
	if err := circuit.ResultsProofs.ResultsAdd.VerifyOldLeafHash(api, hFn, circuit.Results.OldResultsAdd.SerializeVars()...); err != nil {
		circuits.FrontendError(api, "failed to verify add results proof old leaf hash: ", err)
		return
	}
	if err := circuit.ResultsProofs.ResultsSub.VerifyOldLeafHash(api, hFn, circuit.Results.OldResultsSub.SerializeVars()...); err != nil {
		circuits.FrontendError(api, "failed to verify sub results proof old leaf hash: ", err)
		return
	}
	if err := circuit.ResultsProofs.ResultsAdd.VerifyNewLeafHash(api, hFn, circuit.Results.NewResultsAdd.SerializeVars()...); err != nil {
		circuits.FrontendError(api, "failed to verify add results proof new leaf hash: ", err)
		return
	}
	if err := circuit.ResultsProofs.ResultsSub.VerifyNewLeafHash(api, hFn, circuit.Results.NewResultsSub.SerializeVars()...); err != nil {
		circuits.FrontendError(api, "failed to verify sub results proof new leaf hash: ", err)
		return
	}
}

// VerifyBallots counts the ballots using homomorphic encryption and checks
// that the number of ballots is equal to the number of new votes and
// overwritten votes. It uses the Ballot structure to count the ballots.
func (circuit StateTransitionCircuit) VerifyBallots(api frontend.API) {
	ballotSum, overwrittenSum, zero := circuits.NewBallot(), circuits.NewBallot(), circuits.NewBallot()
	var ballotCount, overwrittenCount frontend.Variable = 0, 0

	for i, b := range circuit.VotesProofs.Ballot {
		ballotSum.Add(api, ballotSum, circuits.NewBallot().Select(api, b.IsInsertOrUpdate(api), &circuit.Votes[i].ReencryptedBallot, zero))
		overwrittenSum.Add(api, overwrittenSum, circuits.NewBallot().Select(api, b.IsUpdate(api), &circuit.Votes[i].OverwrittenBallot, zero))
		ballotCount = api.Add(ballotCount, b.IsInsertOrUpdate(api))
		overwrittenCount = api.Add(overwrittenCount, b.IsUpdate(api))
	}

	circuit.Results.NewResultsAdd.AssertIsEqual(api,
		circuits.NewBallot().Add(api, &circuit.Results.OldResultsAdd, ballotSum))
	circuit.Results.NewResultsSub.AssertIsEqual(api,
		circuits.NewBallot().Add(api, &circuit.Results.OldResultsSub, overwrittenSum))
	api.AssertIsEqual(circuit.NumNewVotes, ballotCount)
	api.AssertIsEqual(circuit.NumOverwritten, overwrittenCount)
}
