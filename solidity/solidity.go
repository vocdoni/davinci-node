package solidity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/backend/groth16"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
)

// SolidityProof represents a Groth16 proof for Solidity (without commitments).
type SolidityProof struct {
	Ar  [2]*big.Int    `json:"Ar"`
	Bs  [2][2]*big.Int `json:"Bs"`
	Krs [2]*big.Int    `json:"Krs"`
}

// Groth16CommitmentProof represents a Groth16 proof with commitments, for Solidity.
type Groth16CommitmentProof struct {
	Proof         SolidityProof `json:"proof"`
	Commitments   [2]*big.Int   `json:"commitments"`
	CommitmentPok [2]*big.Int   `json:"commitment_pok"`
}

// FromGnarkProof converts a gnark groth16 proof to a Solidity‑compatible proof.
func (p *Groth16CommitmentProof) FromGnarkProof(proof groth16.Proof) error {
	g16proof, ok := proof.(*groth16_bn254.Proof)
	if !ok {
		return fmt.Errorf("expected groth16_bn254.Proof, got %T", proof)
	}

	solProof := SolidityProof{
		Ar: [2]*big.Int{
			g16proof.Ar.X.BigInt(new(big.Int)),
			g16proof.Ar.Y.BigInt(new(big.Int)),
		},
		Bs: [2][2]*big.Int{
			{
				g16proof.Bs.X.A1.BigInt(new(big.Int)),
				g16proof.Bs.X.A0.BigInt(new(big.Int)),
			},
			{
				g16proof.Bs.Y.A1.BigInt(new(big.Int)),
				g16proof.Bs.Y.A0.BigInt(new(big.Int)),
			},
		},
		Krs: [2]*big.Int{
			g16proof.Krs.X.BigInt(new(big.Int)),
			g16proof.Krs.Y.BigInt(new(big.Int)),
		},
	}

	com := [2]*big.Int{
		g16proof.Commitments[0].X.BigInt(new(big.Int)),
		g16proof.Commitments[0].Y.BigInt(new(big.Int)),
	}

	comPok := [2]*big.Int{
		g16proof.CommitmentPok.X.BigInt(new(big.Int)),
		g16proof.CommitmentPok.Y.BigInt(new(big.Int)),
	}

	p.Proof = solProof
	p.Commitments = com
	p.CommitmentPok = comPok
	return nil
}

// String returns a JSON representation of the Groth16CommitmentProof as a
// string. This is useful for debugging or logging purposes. If marshalling
// fails, it returns an empty JSON object as a string.
func (p *Groth16CommitmentProof) String() string {
	jsonProof, err := json.Marshal(p)
	if err != nil {
		return "{}" // Return empty JSON if marshalling fails
	}
	return string(jsonProof)
}

// ABIEncode returns the raw proof bytes expected by the Solidity verifier.
func (p *Groth16CommitmentProof) ABIEncode() ([]byte, error) {
	return p.MarshalSolidity(), nil
}

// MarshalSolidity returns the proof as the byte layout expected by gnark's
// Solidity verifier: proof points followed by commitments and proof of
// knowledge.
func (p *Groth16CommitmentProof) MarshalSolidity() []byte {
	var buf bytes.Buffer
	appendUint256 := func(x *big.Int) {
		b := x.Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		buf.Write(padded)
	}

	appendUint256(p.Proof.Ar[0])
	appendUint256(p.Proof.Ar[1])
	appendUint256(p.Proof.Bs[0][0])
	appendUint256(p.Proof.Bs[0][1])
	appendUint256(p.Proof.Bs[1][0])
	appendUint256(p.Proof.Bs[1][1])
	appendUint256(p.Proof.Krs[0])
	appendUint256(p.Proof.Krs[1])
	appendUint256(p.Commitments[0])
	appendUint256(p.Commitments[1])
	appendUint256(p.CommitmentPok[0])
	appendUint256(p.CommitmentPok[1])

	return buf.Bytes()
}
