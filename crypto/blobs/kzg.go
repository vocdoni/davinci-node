package blobs

import (
	"bytes"
	"fmt"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	kzg_bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381/kzg"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/commitments/kzg"
	"github.com/consensys/gnark/std/conversion"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/math/uints"
)

// verificationKey holds the EIP-4844 KZG SRS verification key.
var verificationKey *kzg.VerifyingKey[sw_bls12381.G1Affine, sw_bls12381.G2Affine]

func init() {
	verificationKey = initVerificationKey()
}

// srsData contains the compressed G1 || G2[0] || G2[1] entries for the EIP-4844 verification key.
var srsData = []byte{
	// G1 point (compressed, 48 bytes)
	0x97, 0xf1, 0xd3, 0xa7, 0x31, 0x97, 0xd7, 0x94, 0x26, 0x95, 0x63, 0x8c, 0x4f, 0xa9, 0xac, 0x0f,
	0xc3, 0x68, 0x8c, 0x4f, 0x97, 0x74, 0xb9, 0x05, 0xa1, 0x4e, 0x3a, 0x3f, 0x17, 0x1b, 0xac, 0x58,
	0x6c, 0x55, 0xe8, 0x3f, 0xf9, 0x7a, 0x1a, 0xef, 0xfb, 0x3a, 0xf0, 0x0a, 0xdb, 0x22, 0xc6, 0xbb,
	// G2[0] point (compressed, 96 bytes)
	0x93, 0xe0, 0x2b, 0x60, 0x52, 0x71, 0x9f, 0x60, 0x7d, 0xac, 0xd3, 0xa0, 0x88, 0x27, 0x4f, 0x65,
	0x59, 0x6b, 0xd0, 0xd0, 0x99, 0x20, 0xb6, 0x1a, 0xb5, 0xda, 0x61, 0xbb, 0xdc, 0x7f, 0x50, 0x49,
	0x33, 0x4c, 0xf1, 0x12, 0x13, 0x94, 0x5d, 0x57, 0xe5, 0xac, 0x7d, 0x05, 0x5d, 0x04, 0x2b, 0x7e,
	0x02, 0x4a, 0xa2, 0xb2, 0xf0, 0x8f, 0x0a, 0x91, 0x26, 0x08, 0x05, 0x27, 0x2d, 0xc5, 0x10, 0x51,
	0xc6, 0xe4, 0x7a, 0xd4, 0xfa, 0x40, 0x3b, 0x02, 0xb4, 0x51, 0x0b, 0x64, 0x7a, 0xe3, 0xd1, 0x77,
	0x0b, 0xac, 0x03, 0x26, 0xa8, 0x05, 0xbb, 0xef, 0xd4, 0x80, 0x56, 0xc8, 0xc1, 0x21, 0xbd, 0xb8,
	// G2[1] point (compressed, 96 bytes)
	0xb5, 0xbf, 0xd7, 0xdd, 0x8c, 0xde, 0xb1, 0x28, 0x84, 0x3b, 0xc2, 0x87, 0x23, 0x0a, 0xf3, 0x89,
	0x26, 0x18, 0x70, 0x75, 0xcb, 0xfb, 0xef, 0xa8, 0x10, 0x09, 0xa2, 0xce, 0x61, 0x5a, 0xc5, 0x3d,
	0x29, 0x14, 0xe5, 0x87, 0x0c, 0xb4, 0x52, 0xd2, 0xaf, 0xaa, 0xab, 0x24, 0xf3, 0x49, 0x9f, 0x72,
	0x18, 0x5c, 0xbf, 0xee, 0x53, 0x49, 0x27, 0x14, 0x73, 0x44, 0x29, 0xb7, 0xb3, 0x86, 0x08, 0xe2,
	0x39, 0x26, 0xc9, 0x11, 0xcc, 0xec, 0xea, 0xc9, 0xa3, 0x68, 0x51, 0x47, 0x7b, 0xa4, 0xc6, 0x0b,
	0x08, 0x70, 0x41, 0xde, 0x62, 0x10, 0x00, 0xed, 0xc9, 0x8e, 0xda, 0xda, 0x20, 0xc1, 0xde, 0xf2,
}

// initVerificationKey initializes the KZG verification key from the embedded SRS data.
func initVerificationKey() *kzg.VerifyingKey[sw_bls12381.G1Affine, sw_bls12381.G2Affine] {
	var vk kzg_bls12381.VerifyingKey
	dec := bls12381.NewDecoder(bytes.NewBuffer(srsData), bls12381.NoSubgroupChecks())

	if err := dec.Decode(&vk.G1); err != nil {
		panic(fmt.Sprintf("failed to decode G1: %v", err))
	}
	if err := dec.Decode(&vk.G2[0]); err != nil {
		panic(fmt.Sprintf("failed to decode G2[0]: %v", err))
	}
	if err := dec.Decode(&vk.G2[1]); err != nil {
		panic(fmt.Sprintf("failed to decode G2[1]: %v", err))
	}

	vk.Lines[0] = bls12381.PrecomputeLines(vk.G2[0])
	vk.Lines[1] = bls12381.PrecomputeLines(vk.G2[1])

	vkw, err := kzg.ValueOfVerifyingKeyFixed[sw_bls12381.G1Affine, sw_bls12381.G2Affine](vk)
	if err != nil {
		panic(fmt.Sprintf("failed to convert verification key: %v", err))
	}

	return &vkw
}

// UnmarshalKZGCommitment decompresses a KZG commitment from three 16-byte limbs.
// The commitment is a compressed BLS12-381 G1 point (48 bytes total).
func UnmarshalKZGCommitment(
	api frontend.API,
	compressedLimbs [3]frontend.Variable,
) (*sw_bls12381.G1Affine, error) {
	var commitmentBytes [bls12381.SizeOfG1AffineCompressed]uints.U8

	for i := range 3 {
		limbBytes, err := conversion.NativeToBytes(api, compressedLimbs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert limb %d to bytes: %w", i, err)
		}
		copy(commitmentBytes[i*16:(i+1)*16], limbBytes[16:])
	}

	g1, err := sw_bls12381.NewG1(api)
	if err != nil {
		return nil, fmt.Errorf("failed to create G1 instance: %w", err)
	}

	commitment, err := g1.UnmarshalCompressed(commitmentBytes[:])
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal commitment: %w", err)
	}

	return commitment, nil
}

// UnmarshalKZGProof decompresses a KZG proof from three 16-byte limbs.
// The proof is a compressed BLS12-381 G1 point (48 bytes total).
func UnmarshalKZGProof(
	api frontend.API,
	compressedLimbs [3]frontend.Variable,
) (*sw_bls12381.G1Affine, error) {
	var proofBytes [bls12381.SizeOfG1AffineCompressed]uints.U8

	for i := range 3 {
		limbBytes, err := conversion.NativeToBytes(api, compressedLimbs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert limb %d to bytes: %w", i, err)
		}
		copy(proofBytes[i*16:(i+1)*16], limbBytes[16:])
	}

	g1, err := sw_bls12381.NewG1(api)
	if err != nil {
		return nil, fmt.Errorf("failed to create G1 instance: %w", err)
	}

	proof, err := g1.UnmarshalCompressed(proofBytes[:])
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal proof: %w", err)
	}

	return proof, nil
}

// VerifyKZGProof verifies a KZG opening proof using the EIP-4844 SRS.
// It checks that the commitment C opens at evaluation point z to value y.
//
// Parameters:
//   - api: The circuit API
//   - commitment: The KZG commitment point (G1)
//   - proof: The KZG opening proof point (G1)
//   - z: The evaluation point (BLS12-381 scalar field element)
//   - y: The claimed value at z (BLS12-381 scalar field element)
func VerifyKZGProof(
	api frontend.API,
	commitment *sw_bls12381.G1Affine,
	proof *sw_bls12381.G1Affine,
	z emulated.Element[sw_bls12381.ScalarField],
	y emulated.Element[sw_bls12381.ScalarField],
) error {
	verifier, err := kzg.NewVerifier[
		emulated.BLS12381Fr,
		sw_bls12381.G1Affine,
		sw_bls12381.G2Affine,
		sw_bls12381.GTEl,
	](api)
	if err != nil {
		return fmt.Errorf("failed to create KZG verifier: %w", err)
	}

	kzgCommitment := kzg.Commitment[sw_bls12381.G1Affine]{
		G1El: *commitment,
	}

	kzgOpening := kzg.OpeningProof[sw_bls12381.ScalarField, sw_bls12381.G1Affine]{
		Quotient:     *proof,
		ClaimedValue: y,
	}

	if err := verifier.CheckOpeningProof(kzgCommitment, kzgOpening, z, *verificationKey); err != nil {
		return fmt.Errorf("KZG proof verification failed: %w", err)
	}

	return nil
}
