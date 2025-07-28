package csp

import (
	"fmt"

	"github.com/consensys/gnark-crypto/ecc/twistededwards"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/csp/eddsa"
	"github.com/vocdoni/davinci-node/types"
)

type CSP interface {
	SetSeed(seed []byte) error
	CensusOrigin() types.CensusOrigin
	CensusRoot() types.HexBytes
	GenerateProof(processID *types.ProcessID, address common.Address) (*types.CensusProof, error)
	VerifyProof(proof *types.CensusProof) error
}

func New(origin types.CensusOrigin, seed []byte) (CSP, error) {
	switch origin {
	case types.CensusOriginCSPEdDSABLS12377:
		csp, err := eddsa.New(twistededwards.BLS12_377)
		if err != nil {
			return nil, fmt.Errorf("failed to create EdDSA CSP: %w", err)
		}
		if err := csp.SetSeed(seed); err != nil {
			return nil, fmt.Errorf("failed to set seed for EdDSA CSP: %w", err)
		}
		return csp, nil
	default:
		return nil, fmt.Errorf("unsupported census origin: %s", origin)
	}
}

func VerifyCensusProof(proof *types.CensusProof) error {
	var csp CSP
	switch proof.CensusOrigin {
	case types.CensusOriginCSPEdDSABLS12377:
		var err error
		csp, err = eddsa.New(twistededwards.BLS12_377)
		if err != nil {
			return fmt.Errorf("failed to create EdDSA CSP: %w", err)
		}
	default:
		return fmt.Errorf("unsupported census origin: %s", proof.CensusOrigin)
	}

	return csp.VerifyProof(proof)
}
