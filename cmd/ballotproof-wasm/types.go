//go:build js && wasm
// +build js,wasm

package main

import (
	"syscall/js"

	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

type BallotProofWasmInputs struct {
	Address       types.HexBytes    `json:"address"`
	ProcessID     types.HexBytes    `json:"processID"`
	Secret        types.HexBytes    `json:"secret"`
	EncryptionKey []*types.BigInt   `json:"encryptionKey"`
	K             *types.BigInt     `json:"k"`
	BallotMode    *types.BallotMode `json:"ballotMode"`
	Weight        *types.BigInt     `json:"weight"`
	FieldValues   []*types.BigInt   `json:"fieldValues"`
}

func (i *BallotProofWasmInputs) Unmarshal(v js.Value) error {
	res, err := FromJSONValue[BallotProofWasmInputs](v)
	if err != nil {
		return err
	}
	*i = res
	return nil
}

type CircomInputs struct {
	Fields           []string        `json:"fields"`
	MaxCount         string          `json:"maxCount"`
	ForceUniqueness  string          `json:"forceUniqueness"`
	MaxValue         string          `json:"maxValue"`
	MinValue         string          `json:"minValue"`
	MaxTotalCost     string          `json:"maxTotalCost"`
	MinTotalCost     string          `json:"minTotalCost"`
	CostExp          string          `json:"costExp"`
	CostFromWeight   string          `json:"costFromWeight"`
	Address          string          `json:"address"`
	Weight           string          `json:"weight"`
	ProcessID        string          `json:"processId"`
	PK               []string        `json:"pk"`
	K                string          `json:"k"`
	Ballot           *elgamal.Ballot `json:"ballot"`
	Nullifier        string          `json:"nullifier"`
	Commitment       string          `json:"commitment"`
	Secret           string          `json:"secret"`
	InputsHash       types.HexBytes  `json:"inputsHash"`
	InputsHashBigInt string          `json:"inputsHashBigInt"`
}

type BallotProofWasmResult struct {
	CircuitInputs *CircomInputs  `json:"circuitInputs"`
	SignatureHash types.HexBytes `json:"signatureHash"`
}
