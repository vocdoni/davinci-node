//	Blob‑evaluation proof  (EIP‑4844 / Proto‑Danksharding)
//
//	Proves in‑circuit that Y = P(Z)
//	where P is the polynomial that commits to the 4096‑cell “blob” and
//	(Z,Y) is a claimed evaluation point.
//
//	All arithmetic over BLS12‑381 Fr is emulated on top of the native BN254 scalar field
//
//	The heavy division              dᵢ−Y
//	                     qᵢ  =  ───────────────
//	                               ωᵢ−Z
//	is carried out as a hint and then verified inside the circuit.
//
//	Rationale:
//	• For every index i either:
//	     (dᵢ−Y)  =  qᵢ·(ωᵢ−Z)         if ωᵢ ≠ Z
//	      qᵢ     =  0                 if ωᵢ  = Z
//	  holds, so the circuit forces the quotient values produced by the hint.
//	• We additionally enforce
//	      Σ qᵢ·ωᵢ ≡ 0  (mod r)
//	  which is satisfied if Y = P(Z) for a poly of degree < N.
//	  With the per‑index equations this single sum rule is already sufficient;
//
// References:
//   - https://docs.sotazk.org/docs/zk_rollups_after_eip4844/#point-evaluation-precompile
//   - https://github.com/Consensys/gnark/blob/master/std/evmprecompiles/10-kzg_point_evaluation.go
package blobs

import (
	"fmt"
	"math/big"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
	gethkzg "github.com/ethereum/go-ethereum/crypto/kzg4844"
)

const (
	N = 1 << 12 // 4096 evaluation points
)

// FE is type modulus for BLS12‑381 Fr.
type FE = emulated.BLS12381Fr

// VerifyBarycentricEvaluation performs the barycentric evaluation verification.
//
// IMPORTANT: This function does NOT verify the KZG commitment or proof.
// It only checks that Y = P(Z) where P is the polynomial interpolating the blob data.
// For full KZG verification including commitment and proof, use VerifyBlobEvaluationBN254.
//
// This function can be imported and used by circuits that only need to verify
// the polynomial evaluation without the cryptographic commitment.
//
// Parameters:
//   - api: The frontend API for circuit operations
//   - z: The evaluation point Z (BLS12-381 Fr)
//   - y: The claimed evaluation result Y (BLS12-381 Fr)
//   - blob: The blob data (4096 BLS12-381 Fr elements)
func VerifyBarycentricEvaluation(
	api frontend.API,
	z *emulated.Element[FE],
	y *emulated.Element[FE],
	blob [N]emulated.Element[FE],
) error {
	// Field helpers
	fr, err := emulated.NewField[FE](api)
	if err != nil {
		return err
	}
	zero := fr.Zero()
	omegaAt := func(i int) *emulated.Element[FE] { return fr.NewElement(omegaHex[i]) }

	// Hint input packing
	in := make([]*emulated.Element[FE], 2+2*N) // [Y | Z | blob | ω]
	in[0], in[1] = y, z
	for i := range N {
		in[2+i] = &blob[i]
		in[2+N+i] = omegaAt(i)
	}

	// Produce q₀,…,q_{N−1}, Σ qᵢ·ωᵢ
	outs, err := fr.NewHint(quotHint, N+1, in...)
	if err != nil {
		return err
	}
	q := outs[:N]
	S1 := fr.Reduce(outs[N]) // Σ qᵢ·ωᵢ
	for i := range N {
		q[i] = fr.Reduce(q[i])
	}

	// Per‑index constraints
	direct := fr.Zero() // value to take when Z hits a grid‑point
	anyZero := frontend.Variable(0)

	for i := range N {
		ωi := omegaAt(i)
		denR := fr.Reduce(fr.Sub(ωi, z)) // ωᵢ − Z
		isZero := fr.IsZero(denR)        // boolean

		// (dᵢ − Y) = qᵢ·(ωᵢ − Z)
		lhs := fr.Reduce(fr.Select(isZero, zero,
			fr.Reduce(fr.Sub(&blob[i], y))))
		rhs := fr.Reduce(fr.Select(isZero, zero,
			fr.Reduce(fr.Mul(q[i], denR))))
		fr.AssertIsEqual(lhs, rhs)

		// qᵢ must be 0 on the collision branch
		fr.AssertIsEqual(fr.Select(isZero, q[i], zero), zero)

		// Track the direct‑hit value safely
		direct = fr.Reduce(fr.Select(isZero, fr.Reduce(&blob[i]), direct))
		anyZero = api.Or(anyZero, isZero)
	}

	// Degree‑bound check  Σ qᵢ·ωᵢ = 0
	fr.AssertIsEqual(S1, zero)

	// Collision vs barycentric branch select
	//
	//  • if Z = ωᵏ   result must equal blob[k]
	//  • else        result is already constrained to Y above
	//
	final := fr.Reduce(fr.Select(anyZero, direct, y))
	fr.AssertIsEqual(final, y)
	return nil
}

// VerifyFullBlobEvaluationBN254 performs COMPLETE blob verification including:
// 1. Barycentric evaluation: Y = P(Z) where P interpolates the blob
// 2. KZG commitment verification: proves the commitment matches the blob
// 3. KZG opening proof verification: proves Y is the correct evaluation at Z
//
// This is the FULL verification function that should be used in production circuits.
// It accepts native BN254 inputs and converts them to emulated BLS12-381 field elements.
//
// Parameters:
//   - api: The frontend API for circuit operations
//   - z: The evaluation point Z (native BN254 scalar)
//   - y: The claimed evaluation result Y (emulated BLS12-381 Fr)
//   - blob: The blob data (4096 native BN254 scalars)
//   - commitment: The KZG commitment to the blob (BLS12-381 G1 point)
//   - proof: The KZG opening proof (BLS12-381 G1 point)
//
// Returns an error if any verification step fails.
func VerifyFullBlobEvaluationBN254(
	api frontend.API,
	z frontend.Variable, // BN254
	y *emulated.Element[FE], // emulated BLS12-381 Fr
	blob [N]frontend.Variable, // BN254
	commitment *sw_bls12381.G1Affine, // BLS12-381 G1 point
	proof *sw_bls12381.G1Affine, // BLS12-381 G1 point
) error {
	fr, err := emulated.NewField[FE](api)
	if err != nil {
		return err
	}

	// convert all native scalars => emulated via the hint
	var blobEmu [N]emulated.Element[FE]
	for i := range N {
		e, err := hintNativeToEmu(api, fr, blob[i])
		if err != nil {
			return err
		}
		blobEmu[i] = *e
		// soundness: verify that the native input matches the emulated one created by the hint
		api.AssertIsEqual(emulatedToNative(api, &blobEmu[i]), blob[i])
	}

	// convert the native evaluation point Z => emulated
	zEmu, err := hintNativeToEmu(api, fr, z)
	if err != nil {
		return err
	}
	// verify that the native input matches the emulated one created by the hint
	api.AssertIsEqual(emulatedToNative(api, zEmu), z)

	// Verify the barycentric evaluation (does NOT check KZG commitment/proof)
	if err := VerifyBarycentricEvaluation(api, zEmu, y, blobEmu); err != nil {
		return err
	}

	// Verify the KZG opening proof (checks commitment and proof are valid)
	return VerifyKZGProof(api, commitment, proof, *zEmu, *y)
}

// emulatedToNative converts an emulated element to a native BN254 variable.
// This is used to ensure that the native input matches the emulated output
// in the circuit, ensuring soundness.
func emulatedToNative(api frontend.API, e *emulated.Element[FE]) frontend.Variable {
	nbBits := FE{}.BitsPerLimb() // 32 in gnark params
	acc := frontend.Variable(0)
	pow := big.NewInt(1)

	for i, limb := range e.Limbs {
		if i != 0 {
			pow = new(big.Int).Lsh(big.NewInt(1), nbBits*uint(i))
		}
		acc = api.Add(acc, api.Mul(limb, pow))
	}
	return acc
}

// KZGToCircuitInputs converts geth-kzg types to Gnark circuit-compatible types.
//
// Parameters:
//   - commitment: 48-byte compressed BLS12-381 G1 point from gethkzg.BlobToCommitment
//   - proof: 48-byte compressed BLS12-381 G1 point from gethkzg.ComputeProof
//   - claim: 32-byte BLS12-381 Fr element (Y value) from gethkzg.ComputeProof
//
// Returns:
//   - commitmentPoint: sw_bls12381.G1Affine point for witness assignment
//   - proofPoint: sw_bls12381.G1Affine point for witness assignment
//   - y: The claim value as a big.Int for emulated.ValueOf[FE]
func KZGToCircuitInputs(
	commitment gethkzg.Commitment,
	proof gethkzg.Proof,
	claim gethkzg.Claim,
) (commitmentPoint sw_bls12381.G1Affine, proofPoint sw_bls12381.G1Affine, y *big.Int, err error) {
	// Import the gnark-crypto bls12381 package for unmarshaling
	var commitmentCrypto, proofCrypto bls12381.G1Affine

	// Unmarshal commitment from compressed bytes
	if _, err = commitmentCrypto.SetBytes(commitment[:]); err != nil {
		return commitmentPoint, proofPoint, nil, fmt.Errorf("failed to unmarshal commitment: %w", err)
	}
	commitmentPoint = sw_bls12381.NewG1Affine(commitmentCrypto)

	// Unmarshal proof from compressed bytes
	if _, err = proofCrypto.SetBytes(proof[:]); err != nil {
		return commitmentPoint, proofPoint, nil, fmt.Errorf("failed to unmarshal proof: %w", err)
	}
	proofPoint = sw_bls12381.NewG1Affine(proofCrypto)

	// Convert claim (32 bytes) to big.Int
	y = new(big.Int).SetBytes(claim[:])

	return commitmentPoint, proofPoint, y, nil
}
