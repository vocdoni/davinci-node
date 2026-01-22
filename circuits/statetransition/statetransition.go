package statetransition

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/census"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/crypto/blobs"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
	imt "github.com/vocdoni/lean-imt-go/circuit"
)

// HashFn is the hash function used in the circuit. It should the equivalent
// hash function used in the state package (state.HashFn).
var HashFn = poseidon.MultiHash

type StateTransitionCircuit struct {
	// Public inputs
	RootHashBefore        frontend.Variable `gnark:",public"`
	RootHashAfter         frontend.Variable `gnark:",public"`
	VotersCount           frontend.Variable `gnark:",public"`
	OverwrittenVotesCount frontend.Variable `gnark:",public"`

	// Census root
	CensusRoot frontend.Variable `gnark:",public"`
	// Private census inclusion proofs
	CensusProofs CensusProofs

	// KZG commitment to the blob (as 3 x 16-byte limbs)
	BlobCommitmentLimbs [3]frontend.Variable `gnark:",public"`

	// Private KZG proof and evaluation result (verified in-circuit)
	BlobProofLimbs        [3]frontend.Variable
	BlobEvaluationResultY emulated.Element[emulated.BLS12381Fr]

	// Private data inputs
	Process       circuits.Process[frontend.Variable]
	Votes         [params.VotesPerBatch]Vote
	Results       Results
	ReencryptionK frontend.Variable

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
	CensusOrigin  merkleproof.MerkleProof
	BallotMode    merkleproof.MerkleProof
	EncryptionKey merkleproof.MerkleProof
}

// CensusProofs struct contains the Merkle proofs and CSP proofs for the
// voters of the ballots in the batch. They can be proofs of merkle tree or
// CSP proofs depending on the census origin.
type CensusProofs struct {
	MerkleProofs [params.VotesPerBatch]imt.MerkleProof
	CSPProofs    [params.VotesPerBatch]csp.CSPProof
}

// VotesProofs struct contains the Merkle transition proofs for the ballots and
// commitments.
type VotesProofs struct {
	// Key is Address, LeafHash is smt.Hash1(encoded(Ballot.Serialize()))
	Ballot  [params.VotesPerBatch]merkleproof.MerkleTransition
	VoteIDs [params.VotesPerBatch]merkleproof.MerkleTransition
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
	// compute the mask for the valid votes
	mask := circuit.VoteMask(api)
	// recursive proof
	circuit.VerifyAggregatorProof(api, mask)
	// current state
	circuit.VerifyMerkleProofs(api, HashFn)
	// state transition
	circuit.VerifyMerkleTransitions(api, HashFn, mask)
	// leaf hashes
	circuit.VerifyLeafHashes(api, HashFn)
	// censuses
	circuit.VerifyMerkleCensusProofs(api, mask)
	circuit.VerifyCSPCensusProofs(api, mask)
	// votes reencryption and ballots
	circuit.VerifyReencryptedVotes(api, mask)
	circuit.VerifyBallots(api, mask)
	// verify the blob commitment
	circuit.VerifyBlobs(api)
	return nil
}

// VoteMask returns the latch-based mask for real votes.
// Computes a mask where the i-th element is 1 if the vote is
// valid and 0 otherwise. It uses a latch logic to avoid expensive comparisons
// inside the loops.
func (c StateTransitionCircuit) VoteMask(api frontend.API) []frontend.Variable {
	mask := make([]frontend.Variable, params.VotesPerBatch)
	// if VotersCount > 0, the first vote is valid
	isReal := api.Sub(1, api.IsZero(c.VotersCount))
	for i := range params.VotesPerBatch {
		mask[i] = isReal
		// if VotersCount == i+1, the next vote is invalid
		isEnd := api.IsZero(api.Sub(c.VotersCount, i+1))
		isReal = api.Mul(isReal, api.Sub(1, isEnd))
	}
	return mask
}

// paddedElement helper function returns an bw6761 curve emulated element with
// the limb provided as the first limb and the rest as 0. This is used to
// transform the Poseidon hash output to an emulated element of the bw6761 curve.
func paddedElement(limb frontend.Variable) emulated.Element[sw_bw6761.ScalarField] {
	return emulated.Element[sw_bw6761.ScalarField]{
		Limbs: []frontend.Variable{limb, 0, 0, 0, 0, 0},
	}
}

// inputHashToElements transforms the Poseidon hash output to an array of emulated
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

// proofInputsHash calculates the Poseidon hash of the public inputs of the proof
// of the i-th vote. It uses the native Poseidon hash function to calculate the
// hash, and then transform the hash to an emulated element of the bw6761 curve.
// The hash is calculated using the public inputs of the proof of the i-th vote.
func (c StateTransitionCircuit) proofInputsHash(api frontend.API, idx int) frontend.Variable {
	inputsHash, err := poseidon.MultiHash(api, circuits.BallotHash(api, c.Process, c.Votes[idx].Vote)...)
	if err != nil {
		circuits.FrontendError(api, "failed to hash proof inputs with Poseidon: ", err)
		return 0
	}
	return inputsHash
}

// CalculateAggregatorWitness calculates the witness for the Aggregator proof.
// The Aggregator witness is the hash of the public inputs of the proof of each
// vote that it aggregates. The public inputs of the proof of each vote are
// composed by the hash of the public-private inputs of the proof, which is an
// emulated.Element[sw_bn254.ScalarField]. To calculate the witness we need to
// calculate each hash of the public inputs of the proof of each vote (it can
// be done using native Poseidon because this circuit should work in the bn254
// curve). But the witness should be an emulated element of the bw6761 curve,
// that contains the hash as a emulated element of the bn254 curve. So we need
// to transform the hash, first to an emulated element of the bn254 curve,
// and then to an emulated element of the bw6761 curve.
func (c StateTransitionCircuit) CalculateAggregatorWitness(api frontend.API, mask []frontend.Variable) (groth16.Witness[sw_bw6761.ScalarField], error) {
	// the witness should be a bw6761 element, and it should include the
	// number of valid votes as public input
	witness := groth16.Witness[sw_bw6761.ScalarField]{
		Public: []emulated.Element[sw_bw6761.ScalarField]{paddedElement(c.VotersCount)},
	}
	// iterate over votes inputs to select between valid hashes and dummy ones
	hashes := []frontend.Variable{}
	for i := range params.VotesPerBatch {
		inputsHash := c.proofInputsHash(api, i)
		dummyProofInputsHash := 1
		hashes = append(hashes, api.Select(mask[i], inputsHash, dummyProofInputsHash))
	}
	// hash the inputs hashes to get the final witness
	res, err := poseidon.MultiHash(api, hashes...)
	if err != nil {
		return groth16.Witness[sw_bw6761.ScalarField]{}, err
	}
	// include the inputs hash in the witness as elements of the bw6761
	witness.Public = append(witness.Public, inputsHashToElements(api, res)...)
	return witness, nil
}

// VerifyAggregatorProof verifies the Aggregator proof using the witness
// calculated by the CalculateAggregatorWitness function. It uses the
// groth16 verifier to verify the proof. The proof is verified using the
// AggregatorVK, which is the verification key of the Aggregator proof.
func (circuit StateTransitionCircuit) VerifyAggregatorProof(api frontend.API, mask []frontend.Variable) {
	witness, err := circuit.CalculateAggregatorWitness(api, mask)
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
func (circuit StateTransitionCircuit) VerifyReencryptedVotes(api frontend.API, mask []frontend.Variable) {
	lastK := frontend.Variable(circuit.ReencryptionK)
	for i, v := range circuit.Votes {
		var err error
		var reencryptedBallot *circuits.Ballot
		reencryptedBallot, lastK, err = v.Ballot.Reencrypt(api, circuit.Process.EncryptionKey, lastK)
		if err != nil {
			circuits.FrontendError(api, "failed to reencrypt ballot: ", err)
			return
		}
		isEqual := v.ReencryptedBallot.IsEqual(api, reencryptedBallot)
		api.AssertIsEqual(api.Select(mask[i], isEqual, 1), 1)
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
func (circuit StateTransitionCircuit) VerifyMerkleTransitions(api frontend.API, hFn utils.Hasher, mask []frontend.Variable) {
	// verify chain of tree transitions, order here is fundamental.
	root := circuit.RootHashBefore
	for i := range circuit.VotesProofs.Ballot {
		// if the vote is dummy, the transition must be a NOOP (Fnc0=0, Fnc1=0)
		isDummy := api.Sub(1, mask[i])

		// assert that dummy votes have NOOP transitions
		circuit.VotesProofs.Ballot[i].AssertDummyIsNoop(api, isDummy)
		circuit.VotesProofs.VoteIDs[i].AssertDummyIsNoop(api, isDummy)

		// verify transitions
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
	if err := circuit.ProcessProofs.BallotMode.VerifyLeafHash(api, hFn, circuit.Process.BallotMode); err != nil {
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
		circuit.VotesProofs.Ballot[i].VerifyNewKey(api, CalculateBallotIndex(api, v.Address, types.IndexTODO))
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

// VerifyBlobs builds the blob from the state transition data and verifies
// its KZG commitment using the provided evaluation point and result.
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
	for i := range params.VotesPerBatch {
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
	// Verify blob evaluation with in-circuit z computation
	if err := blobs.VerifyFullBlobEvaluationBN254(
		api,
		circuit.Process.ID,
		circuit.RootHashBefore,
		circuit.BlobCommitmentLimbs,
		circuit.BlobProofLimbs,
		&circuit.BlobEvaluationResultY,
		blob); err != nil {
		circuits.FrontendError(api, "failed to verify blob evaluation: ", err)
		return
	}
}

// VerifyBallots sums the ballots using homomorphic encryption and checks
// that the count of all ballots is equal to VotersCount,
// as well as the count of overwritten ballots equals OverwrittenVotesCount.
// It uses the Ballot structure to sum the ballots.
func (circuit StateTransitionCircuit) VerifyBallots(api frontend.API, mask []frontend.Variable) {
	sumOfAllBallots, sumOfOverwrittenBallots, zero := circuits.NewBallot(), circuits.NewBallot(), circuits.NewBallot()
	var votersCount, overwrittenVotesCount frontend.Variable = 0, 0

	for i, b := range circuit.VotesProofs.Ballot {
		isInsertOrUpdate := b.IsInsertOrUpdate(api)
		isUpdate := b.IsUpdate(api)

		ballot := circuits.NewBallot().Select(api, isInsertOrUpdate, &circuit.Votes[i].ReencryptedBallot, zero)
		sumOfAllBallots.Add(api, sumOfAllBallots, ballot)

		overwrittenBallot := circuits.NewBallot().Select(api, isUpdate, &circuit.Votes[i].OverwrittenBallot, zero)
		sumOfOverwrittenBallots.Add(api, sumOfOverwrittenBallots, overwrittenBallot)

		votersCount = api.Add(votersCount, isInsertOrUpdate)
		overwrittenVotesCount = api.Add(overwrittenVotesCount, isUpdate)
	}

	// Assert new results are equal to old results plus ballot sums
	circuit.Results.NewResultsAdd.AssertIsEqual(api,
		circuits.NewBallot().Add(api, &circuit.Results.OldResultsAdd, sumOfAllBallots))
	circuit.Results.NewResultsSub.AssertIsEqual(api,
		circuits.NewBallot().Add(api, &circuit.Results.OldResultsSub, sumOfOverwrittenBallots))
	api.AssertIsEqual(circuit.VotersCount, votersCount)
	api.AssertIsEqual(circuit.OverwrittenVotesCount, overwrittenVotesCount)
}

// VerifyMerkleCensusProofs verifies the Merkle proofs of the votes in the
// batch. It verifies the Merkle proof of each vote using its Verify function
// and that the leaf is correct, but the result is only asserted if the census
// origin is MerkleTree and the vote is real.
func (c StateTransitionCircuit) VerifyMerkleCensusProofs(api frontend.API, mask []frontend.Variable) {
	isMerkleTreeCensus := census.IsMerkleTreeCensusOrigin(api, c.Process.CensusOrigin)
	for i := range params.VotesPerBatch {
		vote := c.Votes[i]
		// check if the proof is valid only if the census origin is MerkleTree
		// and the current vote inputs are from a valid vote.
		shouldBeValid := api.And(mask[i], isMerkleTreeCensus)
		// check that calculated leaf is equal to the one in the proof
		leaf := imt.PackLeaf(api, vote.Address, vote.VoteWeight)
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

// VerifyCSPCensusProofs verifies the CSP proofs of the votes in the batch.
// It verifies the CSP proof of each vote using its IsValid function but the
// result is only asserted if the census origin is CSP and the vote is real.
func (c StateTransitionCircuit) VerifyCSPCensusProofs(api frontend.API, mask []frontend.Variable) {
	isCSPCensus := census.IsCSPCensusOrigin(api, c.Process.CensusOrigin)
	curveID := census.CSPCensusOriginCurveID()
	for i := range params.VotesPerBatch {
		vote := c.Votes[i]
		cspProof := c.CensusProofs.CSPProofs[i]
		// verify the CSP proof
		isValidProof := cspProof.IsValid(api, curveID, c.CensusRoot, c.Process.ID, vote.Address, vote.VoteWeight)
		// the proof should be valid only if it's a real proof and the census origin is CSP
		shouldBeValid := api.And(mask[i], isCSPCensus)
		// assert the validity of the proof only if it should be valid, using
		// its value to compare with 1 only when it applies, otherwise compare
		// with 1 directly (to ignore dummy proofs and non-CSP census origins)
		api.AssertIsEqual(api.Select(shouldBeValid, isValidProof, 1), 1)
	}
}

// CalculateBallotIndex replicates spec.BallotIndex inside the circuit.
// It takes the low 16 bits of the address, applies the censusIndex offset,
// and shifts into the Ballot namespace (starting at params.BallotMin).
//
//	BallotIndex = BallotMin + (index * 2^CensusAddressBitLen) + (address mod 2^CensusAddressBitLen)
func CalculateBallotIndex(api frontend.API, address, censusIndex frontend.Variable) frontend.Variable {
	censusIndexShifted := api.Mul(censusIndex, 1<<params.CensusAddressBitLen)
	addressLE := api.ToBinary(address, common.AddressLength*8)
	addressTruncated := api.FromBinary(addressLE[:params.CensusAddressBitLen]...)
	return api.Add(params.BallotMin, censusIndexShifted, addressTruncated)
}
