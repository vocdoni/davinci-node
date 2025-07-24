package recursion

import (
	"fmt"

	curve "github.com/consensys/gnark-crypto/ecc/bn254"
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
)

// ConvertCircomToGnark converts a Circom proof, verification key, and public
// signals to the Gnark proof format. The proof can be verified using the
// VerifyProof() function.
func ConvertCircomToGnark(circomVk *CircomVerificationKey,
	circomProof *CircomProof, circomPublicSignals []string,
) (*GnarkProof, error) {
	// Convert public signals to field elements
	publicInputs, err := ConvertPublicInputs(circomPublicSignals)
	if err != nil {
		return nil, err
	}

	// Convert the proof and verification key to gnark types
	gnarkProof, err := ConvertProof(circomProof)
	if err != nil {
		return nil, err
	}
	gnarkVk, err := ConvertVerificationKey(circomVk)
	if err != nil {
		return nil, err
	}

	return &GnarkProof{
		Proof:        gnarkProof,
		VerifyingKey: gnarkVk,
		PublicInputs: publicInputs,
	}, nil
}

// ConvertPublicInputs parses an array of strings representing public inputs
// into a slice of bn254fr.Element.
func ConvertPublicInputs(publicSignals []string) ([]bn254fr.Element, error) {
	publicInputs := make([]bn254fr.Element, len(publicSignals))
	for i, s := range publicSignals {
		bi, err := stringToBigInt(s)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public input %d: %v", i, err)
		}
		publicInputs[i].SetBigInt(bi)
	}
	return publicInputs, nil
}

// ConvertProof converts a CircomProof into a Gnark-compatible Proof structure.
func ConvertProof(snarkProof *CircomProof) (*groth16_bn254.Proof, error) {
	// Parse PiA (G1 point)
	arG1, err := stringToG1(snarkProof.PiA)
	if err != nil {
		return nil, fmt.Errorf("failed to convert PiA: %v", err)
	}
	// Parse PiC (G1 point)
	krsG1, err := stringToG1(snarkProof.PiC)
	if err != nil {
		return nil, fmt.Errorf("failed to convert PiC: %v", err)
	}
	// Parse PiB (G2 point)
	bsG2, err := stringToG2(snarkProof.PiB)
	if err != nil {
		return nil, fmt.Errorf("failed to convert PiB: %v", err)
	}
	// Construct the Proof
	gnarkProof := &groth16_bn254.Proof{
		Ar:  *arG1,
		Krs: *krsG1,
		Bs:  *bsG2,
		// Assuming no commitments
	}
	return gnarkProof, nil
}

// ConvertVerificationKey converts a CircomVerificationKey into a
// Gnark-compatible VerifyingKey structure.
func ConvertVerificationKey(snarkVk *CircomVerificationKey) (*groth16_bn254.VerifyingKey, error) {
	// Parse vk_alpha_1 (G1 point)
	alphaG1, err := stringToG1(snarkVk.VkAlpha1)
	if err != nil {
		return nil, fmt.Errorf("failed to convert VkAlpha1: %v", err)
	}
	// Parse vk_beta_2 (G2 point)
	betaG2, err := stringToG2(snarkVk.VkBeta2)
	if err != nil {
		return nil, fmt.Errorf("failed to convert VkBeta2: %v", err)
	}
	// Parse vk_gamma_2 (G2 point)
	gammaG2, err := stringToG2(snarkVk.VkGamma2)
	if err != nil {
		return nil, fmt.Errorf("failed to convert VkGamma2: %v", err)
	}
	// Parse vk_delta_2 (G2 point)
	deltaG2, err := stringToG2(snarkVk.VkDelta2)
	if err != nil {
		return nil, fmt.Errorf("failed to convert VkDelta2: %v", err)
	}

	// Parse IC (G1 points for public inputs)
	numIC := len(snarkVk.IC)
	G1K := make([]curve.G1Affine, numIC)
	for i, icPoint := range snarkVk.IC {
		icG1, err := stringToG1(icPoint)
		if err != nil {
			return nil, fmt.Errorf("failed to convert IC[%d]: %v", i, err)
		}
		G1K[i] = *icG1
	}

	// Construct the VerifyingKey
	vk := &groth16_bn254.VerifyingKey{}

	// Set G1 elements
	vk.G1.Alpha = *alphaG1
	vk.G1.K = G1K

	// Set G2 elements
	vk.G2.Beta = *betaG2
	vk.G2.Gamma = *gammaG2
	vk.G2.Delta = *deltaG2

	// Precompute the necessary values (e, gammaNeg, deltaNeg)
	if err := vk.Precompute(); err != nil {
		return nil, fmt.Errorf("failed to precompute verification key: %v", err)
	}

	return vk, nil
}

// VerifyProof verifies the Gnark proof using the provided verification key and
// public inputs.
func VerifyProof(proof *GnarkProof) (bool, error) {
	err := groth16_bn254.Verify(proof.Proof, proof.VerifyingKey, proof.PublicInputs)
	if err != nil {
		return false, fmt.Errorf("proof verification failed: %v", err)
	}
	return true, nil
}
