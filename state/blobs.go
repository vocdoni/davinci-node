package state

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unsafe"

	bn254 "github.com/consensys/gnark-crypto/ecc/bn254"
	ckzg4844 "github.com/ethereum/c-kzg-4844/v2/bindings/go"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/hash/poseidon"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// BN254 scalar-field modulus
var pBN *big.Int

// init initializes the KZG trusted setup from embedded data
func init() {
	// Initialize the BN254 scalar field modulus
	pBN = bn254.ID.ScalarField()

	// Load the embedded trusted setup from the config
	if err := LoadEmbeddedTrustedSetup(config.KZGTrustedSetup, 0); err != nil {
		panic(fmt.Errorf("failed to load KZG trusted setup: %w", err))
	}
	log.Infow("KZG trusted setup loaded, ready to process Ethereum blobs", "maxVotesPerBlob", CalculateMaxVotesPerBlob())
}

// BlobData represents the structured data extracted from a blob
type BlobData struct {
	Votes      []*Vote
	ResultsAdd []*big.Int
	ResultsSub []*big.Int
}

// findValidZ finds a valid evaluation point z using Poseidon hash over BabyJubJub.
// z = PoseidonHash(processId, rootHashBefore, batchNum, nonce)
// Returns the valid z and the nonce used to find it.
// It continues until it finds a z such that the KZG proof y < pBN. This way we can use
// native bn254 arithmetic (kzg commitments on ethereum are bls12-381).
func findValidZ(processID, rootHashBefore *big.Int, batchNum uint64, blob ckzg4844.Blob) (z *big.Int, nonce uint64, err error) {
	for nonce = 0; ; nonce++ {
		// Calculate z = PoseidonHash(processId, rootHashBefore, batchNum, nonce)
		z, err = poseidon.MultiPoseidon(processID, rootHashBefore, big.NewInt(int64(batchNum)), big.NewInt(int64(nonce)))
		if err != nil {
			return nil, 0, err
		}

		// Mask z to 250 bits as required
		z.And(z, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 250), big.NewInt(1)))
		zBytes := bigIntToBytes32LE(z)

		// Test if this z produces a valid y < pBN
		_, yBytes, err := ckzg4844.ComputeKZGProof(&blob, zBytes)
		if err != nil {
			continue // Try next nonce
		}

		y := bytes32LEtoBigInt(yBytes)
		if y.Cmp(pBN) < 0 {
			// Found valid z
			return z, nonce, nil
		}
	}
}

// BuildKZGCommitment collects the raw batch-data, packs it into one blob and
// produces (blob, commitment, proof, z, y, versionedHash, nonce).
//
// blob layout:
//  1. ResultsAdd (types.FieldsPerBallot * 4 coordinates)
//  2. ResultsSub (types.FieldsPerBallot * 4 coordinates)
//  3. Votes sequentially until voteID = 0x0 (sentinel)
//     Each vote: voteID + address + encryptedBallot coordinates
func (st *State) BuildKZGCommitment(batchNum uint64) (
	blob ckzg4844.Blob,
	commit ckzg4844.KZGCommitment,
	proof ckzg4844.KZGProof,
	z, y *big.Int,
	versionedHash [32]byte,
	nonce uint64,
	err error,
) {
	var cells [ckzg4844.FieldElementsPerBlob][ckzg4844.BytesPerFieldElement]byte
	cell := 0
	push := func(bi *big.Int) {
		if cell >= ckzg4844.FieldElementsPerBlob {
			panic("blob overflow")
		}
		// Store blob cells in big-endian as expected by c-kzg-4844
		bi.FillBytes(cells[cell][:])
		cell++
	}

	// First, add results (always present)
	for _, p := range st.newResultsAdd.BigInts() {
		push(p)
	}
	for _, p := range st.newResultsSub.BigInts() {
		push(p)
	}

	// Then add votes sequentially (no padding)
	for _, v := range st.Votes() {
		push(new(big.Int).SetBytes(v.VoteID))  // voteId hash
		push(v.Address)                        // address
		for _, p := range v.Ballot.BigInts() { // ballot coordinates
			push(p)
		}
	}

	// Add sentinel (voteID = 0x0) to mark end of votes
	// remaining cells are zero-initialised already
	push(big.NewInt(0))

	// Convert 2D cell array to flat blob format required by c-kzg-4844
	// The blob is a fixed-size array (FieldElementsPerBlob * BytesPerFieldElement)
	for i := 0; i < ckzg4844.FieldElementsPerBlob; i++ {
		start := i * ckzg4844.BytesPerFieldElement
		end := start + ckzg4844.BytesPerFieldElement
		copy(blob[start:end], cells[i][:])
	}

	// Generate KZG commitment first
	commit, err = ckzg4844.BlobToKZGCommitment(&blob)
	if err != nil {
		err = fmt.Errorf("blob_to_kzg_commitment failed: %w", err)
		return
	}

	// Find valid evaluation point z using Poseidon hash over BabyJubJub
	// z = PoseidonHash(processId, rootHashBefore, batchNum, nonce)
	z, nonce, err = findValidZ(st.processID, st.rootHashBefore, batchNum, blob)
	if err != nil {
		return
	}

	// Generate KZG proof with the valid z
	zBytes := bigIntToBytes32LE(z)
	var yBytes ckzg4844.Bytes32
	proof, yBytes, err = ckzg4844.ComputeKZGProof(&blob, zBytes)
	if err != nil {
		err = fmt.Errorf("compute_kzg_proof failed: %w", err)
		return
	}
	y = bytes32LEtoBigInt(yBytes)

	// Create versioned hash: H = 0x01 || keccak256(commit)
	hash := ethereum.HashRaw(commit[:])
	versionedHash[0] = 0x01
	copy(versionedHash[1:], hash[:])
	return
}

// ParseBlobData extracts vote and results data from a blob
func ParseBlobData(blob ckzg4844.Blob) (*BlobData, error) {
	coordsPerBallot := types.FieldsPerBallot * 4 // each field has 4 coordinates (C1.X, C1.Y, C2.X, C2.Y)

	data := &BlobData{
		Votes:      make([]*Vote, 0),
		ResultsAdd: make([]*big.Int, coordsPerBallot),
		ResultsSub: make([]*big.Int, coordsPerBallot),
	}

	// extract big.Int from blob cell
	blobBytes := (*(*[ckzg4844.BytesPerBlob]byte)(unsafe.Pointer(&blob)))[:]
	getCell := func(cellIndex int) *big.Int {
		if cellIndex >= ckzg4844.FieldElementsPerBlob {
			return big.NewInt(0)
		}
		start := cellIndex * ckzg4844.BytesPerFieldElement
		cellBytes := blobBytes[start : start+ckzg4844.BytesPerFieldElement]
		// Read blob cells as big-endian (canonical form)
		return new(big.Int).SetBytes(cellBytes)
	}

	cellIndex := 0

	// Extract ResultsAdd (first coordsPerBallot cells)
	for i := 0; i < coordsPerBallot; i++ {
		data.ResultsAdd[i] = getCell(cellIndex)
		cellIndex++
	}

	// Extract ResultsSub (next coordsPerBallot cells)
	for i := 0; i < coordsPerBallot; i++ {
		data.ResultsSub[i] = getCell(cellIndex)
		cellIndex++
	}

	// Extract votes until we find voteID = 0x0 (sentinel)
	for {
		voteID := getCell(cellIndex)
		cellIndex++

		// Check for sentinel (voteID = 0x0)
		if voteID.Cmp(big.NewInt(0)) == 0 {
			break
		}

		// Check if we have enough cells for a complete vote
		if cellIndex+1+coordsPerBallot > ckzg4844.FieldElementsPerBlob {
			return nil, fmt.Errorf("incomplete vote data in blob")
		}

		// Extract address
		address := getCell(cellIndex)
		cellIndex++

		// Extract ballot coordinates
		ballotCoords := make([]*big.Int, coordsPerBallot)
		for j := 0; j < coordsPerBallot; j++ {
			ballotCoords[j] = getCell(cellIndex)
			cellIndex++
		}

		// Create ballot from coordinates using elgamal.NewBallot
		ballot, err := elgamal.NewBallot(Curve).SetBigInts(ballotCoords)
		if err != nil {
			return nil, err
		}

		// Convert voteID back to StateKeyMaxLen-byte array
		voteIDBytes := make([]byte, types.StateKeyMaxLen)
		voteID.FillBytes(voteIDBytes)

		vote := &Vote{
			Address: address,
			VoteID:  voteIDBytes,
			Ballot:  ballot,
		}
		data.Votes = append(data.Votes, vote)

		// Safety check to prevent infinite loop
		if len(data.Votes) > types.VotesPerBatch {
			return nil, fmt.Errorf("too many votes in blob")
		}
	}

	return data, nil
}

// CalculateMaxVotesPerBlob calculates the maximum number of votes that can fit in a blob
func CalculateMaxVotesPerBlob() int {
	coordsPerBallot := types.FieldsPerBallot * 4
	resultsCells := 2 * coordsPerBallot     // resultsAdd + resultsSub
	cellsPerVote := 1 + 1 + coordsPerBallot // voteID + address + ballot
	sentinelCells := 1                      // voteID = 0x0 sentinel

	availableCells := ckzg4844.FieldElementsPerBlob - resultsCells - sentinelCells
	return availableCells / cellsPerVote
}

// ApplyBlobToState applies the data from a blob to restore state
func (st *State) ApplyBlobToState(blobData *BlobData) error {
	if err := st.StartBatch(); err != nil {
		return err
	}

	// Add votes from blob
	for _, vote := range blobData.Votes {
		if err := st.AddVote(vote); err != nil {
			return err
		}
	}

	// End the batch to calculate the results properly
	if err := st.EndBatch(); err != nil {
		return err
	}

	// Now set the final results from the blob data
	resultsAdd, err := elgamal.NewBallot(Curve).SetBigInts(blobData.ResultsAdd)
	if err != nil {
		return err
	}
	st.SetResultsAdd(resultsAdd)

	resultsSub, err := elgamal.NewBallot(Curve).SetBigInts(blobData.ResultsSub)
	if err != nil {
		return err
	}
	st.SetResultsSub(resultsSub)

	return nil
}

// bigIntToBytes32LE converts a big.Int to little-endian ckzg4844.Bytes32.
func bigIntToBytes32LE(x *big.Int) ckzg4844.Bytes32 {
	var out ckzg4844.Bytes32
	// First get big-endian representation, padded to 32 bytes
	be := make([]byte, 32)
	x.FillBytes(be) // This fills with big-endian, left-padded

	// Convert to little-endian by reversing the entire 32-byte array
	for i := 0; i < 32; i++ {
		out[i] = be[31-i]
	}
	return out
}

// bytes32LEtoBigInt converts little-endian ckzg4844.Bytes32 back to *big.Int.
func bytes32LEtoBigInt(b ckzg4844.Bytes32) *big.Int {
	// Convert from little-endian to big-endian for big.Int.SetBytes()
	be := make([]byte, 32)
	for i := 0; i < 32; i++ {
		be[i] = b[31-i]
	}
	return new(big.Int).SetBytes(be)
}

// LoadEmbeddedTrustedSetup converts the textual trusted-setup
// and initialises the C KZG library.
//
//	data: the raw file content (newline / space separated tokens)
//	precompute: 0-15, same meaning as the C API
//
// reference implementation => https://github.com/ethereum/c-kzg-4844/blob/main/src/setup/setup.c
func LoadEmbeddedTrustedSetup(data []byte, precompute uint) error {
	const (
		bytesPerG1 = 48
		bytesPerG2 = 96
	)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(bufio.ScanWords)

	next := func() (string, error) {
		if !scanner.Scan() {
			return "", fmt.Errorf("unexpected end-of-file while reading trusted setup")
		}
		return scanner.Text(), nil
	}

	// Read point counts (first two decimal numbers)
	raw, err := next()
	if err != nil {
		return err
	}
	nG1, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("parsing G1 count: %w", err)
	}

	raw, err = next()
	if err != nil {
		return err
	}
	nG2, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("parsing G2 count: %w", err)
	}

	// Allocate destination slices
	g1Lagrange := make([]byte, nG1*bytesPerG1)
	g2Monomial := make([]byte, nG2*bytesPerG2)
	g1Monomial := make([]byte, nG1*bytesPerG1)

	readPoint := func(dst []byte) error {
		token, err := next()
		if err != nil {
			return err
		}
		token = strings.TrimPrefix(token, "0x") // tolerate an optional 0x
		if len(token) != len(dst)*2 {
			return fmt.Errorf("hex string has %d chars, want %d",
				len(token), len(dst)*2)
		}
		b, err := hex.DecodeString(token)
		if err != nil {
			return err
		}
		copy(dst, b)
		return nil
	}

	// Copy G1(Lagrange) - G2(Monomial) - G1(Monomial) in that order
	for i := 0; i < nG1; i++ {
		if err := readPoint(g1Lagrange[i*bytesPerG1 : (i+1)*bytesPerG1]); err != nil {
			return fmt.Errorf("G1-Lagrange #%d: %w", i, err)
		}
	}
	for i := 0; i < nG2; i++ {
		if err := readPoint(g2Monomial[i*bytesPerG2 : (i+1)*bytesPerG2]); err != nil {
			return fmt.Errorf("G2-Monomial #%d: %w", i, err)
		}
	}
	for i := 0; i < nG1; i++ {
		if err := readPoint(g1Monomial[i*bytesPerG1 : (i+1)*bytesPerG1]); err != nil {
			return fmt.Errorf("G1-Monomial #%d: %w", i, err)
		}
	}

	// Hand everything to C
	if err := ckzg4844.LoadTrustedSetup(
		g1Monomial, g1Lagrange, g2Monomial, precompute,
	); err != nil {
		return fmt.Errorf("ckzg4844.LoadTrustedSetup: %w", err)
	}
	return nil
}
