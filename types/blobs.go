package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"math/big"
	"unsafe"

	eth2deneb "github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/consensys/gnark/frontend"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"
	gethparams "github.com/ethereum/go-ethereum/params"
)

// Blob represents a unified blob type that can be converted to/from external blob types.
// This centralizes blob handling and reduces direct dependencies on external types.

// Blob represents a 4844 data blob.
type Blob [131072]byte // 4096 * 32 bytes per field element

// BlobLength is the number of bytes in a data blob.
const BlobLength = 131072

// Each blob has CellProofsPerBlob (128) cell proofs
const CellProofsPerBlob = gethkzg.CellProofsPerBlob

// NewBlobFromBytes creates a new Blob from raw bytes.
// If the input is not exactly 131072 bytes (4096 field elements * 32 bytes), returns an error.
func NewBlobFromBytes(data []byte) (*Blob, error) {
	if len(data) != BlobLength {
		return nil, fmt.Errorf("invalid blob size: got %d bytes, expected %d", len(data), BlobLength)
	}

	b := &Blob{}
	copy(b[:], data)
	return b, nil
}

// MustBlobFromBytes creates a new Blob from raw bytes.
// If the input is not exactly 131072 bytes (4096 field elements * 32 bytes), panics.
func MustBlobFromBytes(data []byte) *Blob {
	b, err := NewBlobFromBytes(data)
	if err != nil {
		panic(err)
	}
	return b
}

// Assert at compile time that Blob is identical to gethkzg.Blob and eth2deneb.Blob,
// rather than let unsafe.Pointer panic at runtime
var (
	_ [unsafe.Sizeof(Blob{})]byte = [unsafe.Sizeof(gethkzg.Blob{})]byte{}
	_ [unsafe.Sizeof(Blob{})]byte = [unsafe.Sizeof(eth2deneb.Blob{})]byte{}
)

// BlobFromGeth converts (without copy) a gethkzg.Blob into a Blob.
func BlobFromGeth(gethBlob *gethkzg.Blob) *Blob { return (*Blob)(unsafe.Pointer(gethBlob)) }

// BlobFromDeneb converts (without copy) a eth2deneb.Blob into a Blob.
func BlobFromDeneb(denebBlob *eth2deneb.Blob) *Blob { return (*Blob)(unsafe.Pointer(denebBlob)) }

// AsGeth converts (without copy) the blob to a gethkzg.Blob.
func (b *Blob) AsGeth() *gethkzg.Blob { return (*gethkzg.Blob)(unsafe.Pointer(b)) }

// AsDeneb converts (without copy) the blob to an eth2deneb.Blob.
func (b *Blob) AsDeneb() *eth2deneb.Blob { return (*eth2deneb.Blob)(unsafe.Pointer(b)) }

// Bytes returns a slice over the blob data.
// Writing to this slice will modify the underlying array.
func (b *Blob) Bytes() []byte { return b[:] }

// Clone returns a copy of the Blob
func (b *Blob) Clone() Blob { return *b }

// ToGnark splits the blob into 4096 big.Int and returns them as a slice of frontend.Variable
func (b *Blob) ToGnark() [gethparams.BlobTxFieldElementsPerBlob]frontend.Variable {
	var s [gethparams.BlobTxFieldElementsPerBlob]frontend.Variable
	for i := range s {
		offset := i * gethparams.BlobTxBytesPerFieldElement
		s[i] = new(big.Int).SetBytes(b[offset : offset+gethparams.BlobTxBytesPerFieldElement])
	}
	return s
}

// ComputeCommitment creates a small commitment out of a data blob.
func (b *Blob) ComputeCommitment() (KZGCommitment, error) {
	commitment, err := gethkzg.BlobToCommitment(b.AsGeth())
	if err != nil {
		return KZGCommitment{}, err
	}
	return KZGCommitment(commitment), nil
}

// ComputeCommitment creates a small commitment out of a data blob.
func (b *Blob) ComputeCellProofs() ([]KZGProof, error) {
	proofs, err := gethkzg.ComputeCellProofs(b.AsGeth())
	if err != nil {
		return nil, err
	}
	return SliceOf(proofs, KZGProofFromGeth), nil
}

// ComputeBlobProof returns the KZG proof that is used to verify the blob against
// the commitment.
//
// This method does not verify that the commitment is correct with respect to blob.
func (b *Blob) ComputeBlobProof(commitment KZGCommitment) (KZGProof, error) {
	proof, err := gethkzg.ComputeBlobProof(b.AsGeth(), *commitment.AsGeth())
	if err != nil {
		return KZGProof{}, err
	}
	return KZGProof(proof), nil
}

// ComputeProof computes the KZG proof at the given point for the polynomial
// represented by the blob.
//
// If the absolute value of point doesn't fit in [32]byte, returns an error.
func (b *Blob) ComputeProof(point *big.Int) (proof KZGProof, claim *big.Int, err error) {
	var gethPoint gethkzg.Point
	if point.BitLen() > len(gethPoint)*8 {
		return KZGProof{}, nil, fmt.Errorf("point does not fit in %d bytes", len(gethPoint))
	}
	point.FillBytes(gethPoint[:])
	gethProof, gethClaim, err := gethkzg.ComputeProof(b.AsGeth(), gethPoint)
	if err != nil {
		return KZGProof{}, nil, err
	}
	return KZGProof(gethProof), new(big.Int).SetBytes(gethClaim[:]), nil
}

// ComputeCommitmentAndProof calculates the commitment and blob proof of the passed blob, using geth kzg4844.
// This can be used to construct a Version 0 BlobTxSidecar.
func (b *Blob) ComputeCommitmentAndProof() (KZGCommitment, KZGProof, error) {
	commitment, err := b.ComputeCommitment()
	if err != nil {
		return KZGCommitment{}, KZGProof{}, fmt.Errorf("commitment: %w", err)
	}
	proof, err := b.ComputeBlobProof(commitment)
	if err != nil {
		return KZGCommitment{}, KZGProof{}, fmt.Errorf("blob proof: %w", err)
	}
	return commitment, proof, nil
}

// ComputeCommitmentAndCellProofs calculates the commitment and cell proofs of the passed blob, using geth kzg4844.
// This can be used to construct a Version 1 BlobTxSidecar.
func (b *Blob) ComputeCommitmentAndCellProofs() (KZGCommitment, []KZGProof, error) {
	commitment, err := b.ComputeCommitment()
	if err != nil {
		return KZGCommitment{}, nil, fmt.Errorf("commitment: %w", err)
	}
	proofs, err := b.ComputeCellProofs()
	if err != nil {
		return KZGCommitment{}, nil, fmt.Errorf("cell proofs: %w", err)
	}
	return commitment, proofs, nil
}

// KZGCommitment is a serialized commitment to a polynomial.
type KZGCommitment [48]byte

// Assert at compile time that Commitment is identical to gethkzg.Commitment
var _ [unsafe.Sizeof(KZGCommitment{})]byte = [unsafe.Sizeof(gethkzg.Commitment{})]byte{}

// KZGCommitmentFromGeth converts a gethkzg.Commitment to a KZGCommitment
func KZGCommitmentFromGeth(c gethkzg.Commitment) KZGCommitment { return KZGCommitment(c) }

// AsGeth converts (without copy) the Commitment to a gethkzg.Commitment.
func (c *KZGCommitment) AsGeth() *gethkzg.Commitment { return (*gethkzg.Commitment)(unsafe.Pointer(c)) }

// CalcBlobHashV1 calculates the 'versioned blob hash' of a commitment.
// The given hasher must be a sha256 hash instance, otherwise the result will be invalid!
func (c *KZGCommitment) CalcBlobHashV1(hasher hash.Hash) (vh [32]byte) {
	return gethkzg.CalcBlobHashV1(hasher, c.AsGeth())
}

// ToLimbs splits a 48-byte KZG commitment into 3 × 16-byte limbs.
func (c KZGCommitment) ToLimbs() [3]*big.Int { return split48bToLimbs(c) }

// ToGnarkLimbs splits a 48-byte KZG commitment into 3 × 16-byte limbs.
func (c KZGCommitment) ToGnarkLimbs() [3]frontend.Variable { return split48bToGnarkLimbs(c) }

// String returns a string representation of the KZGCommitment.
func (c KZGCommitment) String() string { return hex.EncodeToString(c[:]) }

// KZGProof is a serialized commitment to the quotient polynomial.
type KZGProof [48]byte

// Assert at compile time that KZGProof is identical to gethkzg.Proof
var _ [unsafe.Sizeof(KZGProof{})]byte = [unsafe.Sizeof(gethkzg.Proof{})]byte{}

// KZGProofFromGeth converts a gethkzg.Proof to a KZGProof
func KZGProofFromGeth(p gethkzg.Proof) KZGProof { return KZGProof(p) }

// AsGeth converts (without copy) the KZGProof to a gethkzg.Proof.
func (p *KZGProof) AsGeth() *gethkzg.Proof { return (*gethkzg.Proof)(unsafe.Pointer(p)) }

// String returns a string representation of the KZGProof.
func (p KZGProof) String() string { return hex.EncodeToString(p[:]) }

// ToLimbs splits a 48-byte KZG proof into 3 × 16-byte limbs.
func (p KZGProof) ToLimbs() [3]*big.Int { return split48bToLimbs(p) }

// ToGnarkLimbs splits a 48-byte KZG proof into 3 × 16-byte limbs.
func (p KZGProof) ToGnarkLimbs() [3]frontend.Variable { return split48bToGnarkLimbs(p) }

// BlobSidecar represents a unified blob sidecar that can be converted to/from external types.
type BlobSidecar struct {
	Index      uint64
	Blob       *Blob
	Commitment KZGCommitment
	Proof      KZGProof
}

// NewBlobSidecarFromDeneb creates a new BlobSidecar from an eth2deneb.BlobSidecar.
func NewBlobSidecarFromDeneb(denebSidecar *eth2deneb.BlobSidecar) *BlobSidecar {
	return &BlobSidecar{
		Index:      uint64(denebSidecar.Index),
		Blob:       BlobFromDeneb(&denebSidecar.Blob),
		Commitment: KZGCommitment(denebSidecar.KZGCommitment),
		Proof:      KZGProof(denebSidecar.KZGProof),
	}
}

// ToDenebSidecar converts the BlobSidecar to an eth2deneb.BlobSidecar.
func (bs *BlobSidecar) ToDenebSidecar() *eth2deneb.BlobSidecar {
	return &eth2deneb.BlobSidecar{
		Index:         eth2deneb.BlobIndex(bs.Index),
		Blob:          *bs.Blob.AsDeneb(),
		KZGCommitment: eth2deneb.KZGCommitment(bs.Commitment),
		KZGProof:      eth2deneb.KZGProof(bs.Proof),
	}
}

// VersionedBlobHash returns the versioned blob hash, calculated from the bs.Commitment
func (bs *BlobSidecar) VersionedBlobHash() common.Hash {
	vh := bs.Commitment.CalcBlobHashV1(sha256.New())
	return common.BytesToHash(vh[:])
}

// String returns a string representation of the BlobSidecar.
func (bs *BlobSidecar) String() string {
	if bs == nil {
		return "BlobSidecar<nil>"
	}
	return fmt.Sprintf("BlobSidecar{Index: %d, Commitment: %x, Proof: %x, Blob: (%d bytes)}",
		bs.Index, bs.Commitment[:], bs.Proof[:], len(bs.Blob))
}

const (
	BlobTxSidecarVersion0 = gethtypes.BlobSidecarVersion0
	BlobTxSidecarVersion1 = gethtypes.BlobSidecarVersion1
)

// BlobTxSidecar represents a unified transaction blob sidecar.
type BlobTxSidecar struct {
	Version     uint8
	Blobs       []*Blob
	Commitments []KZGCommitment
	Proofs      []KZGProof
}

func NewBlobTxSidecar(version byte, blobs []*Blob, commitments []KZGCommitment, proofs []KZGProof) *BlobTxSidecar {
	return &BlobTxSidecar{
		Version:     version,
		Blobs:       blobs,
		Commitments: commitments,
		Proofs:      proofs,
	}
}

// NewBlobTxSidecarFromGeth creates a new BlobTxSidecar from a types.BlobTxSidecar.
func NewBlobTxSidecarFromGeth(gethSidecar *gethtypes.BlobTxSidecar) *BlobTxSidecar {
	return &BlobTxSidecar{
		Version:     gethSidecar.Version,
		Blobs:       SliceOf(gethSidecar.Blobs, func(b gethkzg.Blob) *Blob { return BlobFromGeth(&b) }),
		Commitments: SliceOf(gethSidecar.Commitments, KZGCommitmentFromGeth),
		Proofs:      SliceOf(gethSidecar.Proofs, KZGProofFromGeth),
	}
}

// AsGethSidecar converts the BlobTxSidecar to a gethtypes.BlobTxSidecar.
// Returns a shallow copy: all slices are newly allocated, but their elements
// may share underlying memory with the original (no deep copy).
func (bts *BlobTxSidecar) AsGethSidecar() *gethtypes.BlobTxSidecar {
	return gethtypes.NewBlobTxSidecar(
		bts.Version,
		SliceOf(bts.Blobs, func(b *Blob) gethkzg.Blob { return *b.AsGeth() }),
		SliceOf(bts.Commitments, func(c KZGCommitment) gethkzg.Commitment { return *c.AsGeth() }),
		SliceOf(bts.Proofs, func(p KZGProof) gethkzg.Proof { return *p.AsGeth() }),
	)
}

// BlobHashes returns the blob hashes for the sidecar.
func (bts *BlobTxSidecar) BlobHashes() []common.Hash { return bts.AsGethSidecar().BlobHashes() }

// ComputeBlobTxSidecar calculates commitments and proofs of N passed blobs, using geth kzg4844.
// Returns a BlobTxSidecar with either 128*N cell proofs or N blob proofs depending on the passed version.
func ComputeBlobTxSidecar(version byte, blobs []*Blob) (*BlobTxSidecar, error) {
	n := len(blobs)
	if n == 0 {
		return nil, fmt.Errorf("no blobs")
	}

	var proofs []KZGProof
	switch version {
	case BlobTxSidecarVersion0:
		proofs = make([]KZGProof, n)
	case BlobTxSidecarVersion1:
		proofs = make([]KZGProof, n*gethkzg.CellProofsPerBlob)
	default:
		return nil, fmt.Errorf("unsupported sidecar version %d", version)
	}
	sidecar := NewBlobTxSidecar(
		version,
		make([]*Blob, n),
		make([]KZGCommitment, n),
		proofs,
	)

	for i, b := range blobs {
		sidecar.Blobs[i] = b

		switch version {
		case BlobTxSidecarVersion0:
			commitment, proof, err := sidecar.Blobs[i].ComputeCommitmentAndProof()
			if err != nil {
				return nil, fmt.Errorf("compute %d: %w", i, err)
			}
			sidecar.Commitments[i] = commitment
			sidecar.Proofs[i] = proof
		case BlobTxSidecarVersion1:
			commitment, cellProofs, err := sidecar.Blobs[i].ComputeCommitmentAndCellProofs()
			if err != nil {
				return nil, fmt.Errorf("compute %d: %w", i, err)
			}
			sidecar.Commitments[i] = commitment
			copy(sidecar.Proofs[(i)*gethkzg.CellProofsPerBlob:(i+1)*gethkzg.CellProofsPerBlob], cellProofs)
		default:
			return nil, fmt.Errorf("unsupported sidecar version %d", version)
		}
	}

	return sidecar, nil
}

// split48bToLimbs splits 48 bytes into 3 × 16-byte limbs (big-endian).
func split48bToLimbs(b [48]byte) [3]*big.Int {
	return [3]*big.Int{
		new(big.Int).SetBytes(b[0:16]),
		new(big.Int).SetBytes(b[16:32]),
		new(big.Int).SetBytes(b[32:48]),
	}
}

// split48bToGnarkLimbs splits 48 bytes into 3 × 16-byte limbs (big-endian).
func split48bToGnarkLimbs(b [48]byte) [3]frontend.Variable {
	bl := split48bToLimbs(b)
	return [3]frontend.Variable{bl[0], bl[1], bl[2]}
}
