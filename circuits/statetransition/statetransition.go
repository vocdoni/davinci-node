package statetransition

import (
	"fmt"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/math/cmp"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/mimc7"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
	imt "github.com/vocdoni/lean-imt-go/circuit"
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

	// KZG commitment to the blob
	BlobEvaluationPointZ  frontend.Variable                     `gnark:",public"`
	BlobEvaluationResultY emulated.Element[emulated.BLS12381Fr] `gnark:",public"`

	// Private data inputs
	Process       circuits.Process[frontend.Variable]
	Votes         [types.VotesPerBatch]Vote
	Results       Results
	ReencryptionK frontend.Variable

	// Private merkle proofs inputs
	ProcessProofs ProcessProofs
	VotesProofs   VotesProofs
	ResultsProofs ResultsProofs

	// Census related stuff
	CensusRoot   frontend.Variable `gnark:",public"`
	CensusProofs CensusProofs

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
	CensusOrigin  merkleproof.MerkleProof
	BallotMode    merkleproof.MerkleProof
	EncryptionKey merkleproof.MerkleProof
}

// CensusProofs struct contains the Merkle proofs and CSP proofs for the
// voters of the ballots in the batch. They can be proofs of merkle tree or
// CSP proofs depending on the census origin.
type CensusProofs struct {
	MerkleProofs [types.VotesPerBatch]imt.MerkleProof
	CSPProofs    [types.VotesPerBatch]csp.CSPProof
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
	std.RegisterHints()
	// recursive proof
	circuit.VerifyAggregatorProof(api)
	// current state
	circuit.VerifyMerkleProofs(api, HashFn)
	// state transition
	circuit.VerifyMerkleTransitions(api, HashFn)
	// leaf hashes
	circuit.VerifyLeafHashes(api, HashFn)
	// censuses
	circuit.VerifyMerkleCensusProofs(api)
	circuit.VerifyCSPCensusProofs(api)
	// votes reencryption and ballots
	circuit.VerifyReencryptedVotes(api)
	circuit.VerifyBallots(api)
	circuit.VerifyBlobs(api)
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

// VerifyReencryptedVotes reencrypts the votes using the reencryptionK and
// checks if the result is equal to the reencrypted ballot provided as input.
// To reencrypt the votes, it adds the encrypted zero ballot to the original
// ballot. The encrypted zero uses the reencryptionK as the randomness.
func (circuit StateTransitionCircuit) VerifyReencryptedVotes(api frontend.API) {
	lastK := frontend.Variable(circuit.ReencryptionK)
	for i, v := range circuit.Votes {
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
	circuit.ProcessProofs.CensusOrigin.Verify(api, hFn, circuit.RootHashBefore)
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
	if err := circuit.ProcessProofs.CensusOrigin.VerifyLeafHash(api, hFn, circuit.Process.CensusOrigin); err != nil {
		circuits.FrontendError(api, "failed to verify census origin process proof leaf hash: ", err)
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

func (circuit StateTransitionCircuit) VerifyBlobs(api frontend.API) {
	// Build blob and verify evaluation
	//
	// The blob is built as follows:
	// - First, we add the new results (addition and subtraction) - always present
	// - Then, we add the votes sequentially (no padding)
	// - Finally, we add a sentinel (voteID = 0x0) to mark end of votes
	// Each ballot coordinate is represented as a field element (32 bytes).
	// Each field element is represented as a big-endian byte array.
	// The blob is a fixed-size array (FieldElementsPerBlob * BytesPerFieldElement).
	var blob [4096]frontend.Variable
	blobIndex := 0
	// Append a ballot with a mask: every pushed var is multiplied by mask.
	appendBallotMasked := func(b circuits.Ballot, mask frontend.Variable) {
		for _, v := range b.SerializeVars() {
			blob[blobIndex] = api.Mul(mask, v)
			blobIndex++
		}
	}
	// Always include results (no sentinel applies to them)
	appendBallotMasked(circuit.Results.NewResultsAdd, 1)
	appendBallotMasked(circuit.Results.NewResultsSub, 1)
	// Votes section with sentinel handling.
	// keep==1 means "we haven't seen sentinel yet". Once we see voteID==0,
	// keep becomes 0 and stays 0, zeroing out everything afterwards.
	keep := frontend.Variable(1)
	for i := range types.VotesPerBatch {
		voteID := circuit.Votes[i].VoteID
		isZero := api.IsZero(voteID)  // 1 if voteID==0 else 0
		notZero := api.Sub(1, isZero) // 1 if voteID!=0 else 0
		// Only write this vote if keep==1 AND voteID!=0
		writeMask := api.Mul(keep, notZero)
		// VoteID and address (masked)
		blob[blobIndex] = api.Mul(writeMask, voteID)
		blobIndex++
		blob[blobIndex] = api.Mul(writeMask, circuit.Votes[i].Address)
		blobIndex++
		// Reencrypted ballot (masked)
		appendBallotMasked(circuit.Votes[i].ReencryptedBallot, writeMask)
		// Update keep for next iterations: once we saw 0, keep→0 forever
		keep = api.Mul(keep, notZero)
	}
	// Fill the rest of the blob with zeros
	for i := blobIndex; i < len(blob); i++ {
		blob[i] = 0
	}
	// Verify blob baricentric evaluation (z and y are public inputs)
	if err := blobs.VerifyBlobEvaluationNative(api, circuit.BlobEvaluationPointZ, &circuit.BlobEvaluationResultY, blob); err != nil {
		circuits.FrontendError(api, "failed to verify blob evaluation: ", err)
		return
	}
}

// VerifyBallots counts the ballots using homomorphic encryption and checks
// that the number of ballots is equal to the number of new votes and
// overwritten votes. It uses the Ballot structure to count the ballots.
// It also builds the blob and verifies the KZG commitment to the blob.
func (circuit StateTransitionCircuit) VerifyBallots(api frontend.API) {
	ballotSum, overwrittenSum, zero := circuits.NewBallot(), circuits.NewBallot(), circuits.NewBallot()
	var ballotCount, overwrittenCount frontend.Variable = 0, 0
	// Sum ballots and count new and overwritten
	for i, b := range circuit.VotesProofs.Ballot {
		ballotSum.Add(api, ballotSum, circuits.NewBallot().Select(api, b.IsInsertOrUpdate(api), &circuit.Votes[i].ReencryptedBallot, zero))
		overwrittenSum.Add(api, overwrittenSum, circuits.NewBallot().Select(api, b.IsUpdate(api), &circuit.Votes[i].OverwrittenBallot, zero))
		ballotCount = api.Add(ballotCount, b.IsInsertOrUpdate(api))
		overwrittenCount = api.Add(overwrittenCount, b.IsUpdate(api))
	}
	// Assert new results are equal to old results plus ballot sums
	circuit.Results.NewResultsAdd.AssertIsEqual(api,
		circuits.NewBallot().Add(api, &circuit.Results.OldResultsAdd, ballotSum))
	circuit.Results.NewResultsSub.AssertIsEqual(api,
		circuits.NewBallot().Add(api, &circuit.Results.OldResultsSub, overwrittenSum))
	api.AssertIsEqual(circuit.NumNewVotes, ballotCount)
	api.AssertIsEqual(circuit.NumOverwritten, overwrittenCount)
}

func (c StateTransitionCircuit) VerifyMerkleCensusProofs(api frontend.API) {
	for i := range types.VotesPerBatch {
		vote := c.Votes[i]
		// only verify the proof if i < NumNewVotes (to discard dummy proofs)
		isRealProof := cmp.IsLess(api, i, c.NumNewVotes)
		// check if the proof is valid only if the census origin is MerkleTree
		// and the current vote inputs are from a valid vote.
		isMerkleTreeCensus := api.IsZero(api.Sub(c.Process.CensusOrigin, uint8(types.CensusOriginMerkleTree)))
		shouldBeValid := api.And(isRealProof, isMerkleTreeCensus)

		// check that calculated leaf is equal to the one in the proof
		leaf := imt.PackLeaf(api, vote.Address, vote.UserWeight)
		isLeafEqual := api.IsZero(api.Cmp(leaf, c.CensusProofs.MerkleProofs[i].Leaf))
		// assert leaf equality only if the proof should be valid
		api.AssertIsEqual(api.Select(shouldBeValid, isLeafEqual, 1), 1)

		// verify the census proof using the lean imt circuit
		isValid, err := c.CensusProofs.MerkleProofs[i].Verify(api, c.CensusRoot)
		if err != nil {
			circuits.FrontendError(api, "failed to verify merkle census proof: ", err)
			return
		}
		// assert the validity of the proof only if it should be valid
		api.AssertIsEqual(api.Select(shouldBeValid, isValid, 1), 1)
	}
}

func (c StateTransitionCircuit) censusKey(api frontend.API, address frontend.Variable) (frontend.Variable, error) {
	// convert user address to bytes to swap the endianness
	bAddress, err := utils.VarToU8(api, address)
	if err != nil {
		return 0, fmt.Errorf("failed to convert address emulated element to bytes: %w", err)
	}
	// swap the endianness of the address to le to be used in the census proof
	key, err := utils.U8ToVar(api, bAddress[:types.CensusKeyMaxLen])
	if err != nil {
		return 0, fmt.Errorf("failed to convert address bytes to var: %w", err)
	}
	return key, nil
}

func (c StateTransitionCircuit) VerifyCSPCensusProofs(api frontend.API) {
	censusOrigin := types.CensusOriginCSPEdDSABN254
	curveID := censusOrigin.CurveID()
	for i := range types.VotesPerBatch {
		vote := c.Votes[i]
		cspProof := c.CensusProofs.CSPProofs[i]
		// only verify the proof if i < NumNewVotes (to discard dummy proofs)
		isRealProof := cmp.IsLess(api, i, c.NumNewVotes)
		// verify the CSP proof
		isValidProof := cspProof.IsValid(api, curveID, c.CensusRoot, c.Process.ID, vote.Address)
		// check if the census origin is CSP
		isCSPCensus := api.IsZero(api.Sub(c.Process.CensusOrigin, uint8(censusOrigin)))
		// the proof should be valid only if it's a real proof and the census origin is CSP
		shouldBeValid := api.And(isRealProof, isCSPCensus)
		// assert the validity of the proof only if it should be valid, using
		// its value to compare with 1 only when it applies, otherwise compare
		// with 1 directly (to ignore dummy proofs and non-CSP census origins)
		api.AssertIsEqual(api.Select(shouldBeValid, isValidProof, 1), 1)
	}
}
