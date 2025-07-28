package circomgnark

import (
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	recursion "github.com/consensys/gnark/std/recursion/groth16"
)

// CircomProof represents the proof structure output by SnarkJS.
type CircomProof struct {
	PiA      []string   `json:"pi_a"`
	PiB      [][]string `json:"pi_b"`
	PiC      []string   `json:"pi_c"`
	Protocol string     `json:"protocol"`
}

// CircomVerificationKey represents the verification key structure output by SnarkJS.
type CircomVerificationKey struct {
	Protocol      string       `json:"protocol"`
	Curve         string       `json:"curve"`
	NPublic       int          `json:"nPublic"`
	VkAlpha1      []string     `json:"vk_alpha_1"`
	VkBeta2       [][]string   `json:"vk_beta_2"`
	VkGamma2      [][]string   `json:"vk_gamma_2"`
	VkDelta2      [][]string   `json:"vk_delta_2"`
	IC            [][]string   `json:"IC"`
	VkAlphabeta12 [][][]string `json:"vk_alphabeta_12"` // Not used in verification
}

// GnarkRecursionPlaceholders is a set of placeholders that can be used to define recursive circuits.
type GnarkRecursionPlaceholders struct {
	Vk      recursion.VerifyingKey[sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl]
	Witness recursion.Witness[sw_bn254.ScalarField]
	Proof   recursion.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine]
}

// GnarkRecursionProof is a proof that can be used with recursive circuits.
type GnarkRecursionProof struct {
	Proof        recursion.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine]
	Vk           recursion.VerifyingKey[sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl]
	PublicInputs recursion.Witness[sw_bn254.ScalarField]
}

// GnarkProof is a proof that can be used with non-recursive circuits.
type GnarkProof struct {
	Proof        *groth16_bn254.Proof
	VerifyingKey *groth16_bn254.VerifyingKey
	PublicInputs []bn254fr.Element
}
