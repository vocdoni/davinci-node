//go:build js && wasm
// +build js,wasm

package main

import (
	"fmt"
	"math/big"
	"syscall/js"

	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

type BallotProofWasmInputs struct {
	Address       types.HexBytes       `json:"address"`
	ProcessID     types.HexBytes       `json:"processID"`
	Secret        types.HexBytes       `json:"secret"`
	EncryptionKey *types.EncryptionKey `json:"encryptionKey"`
	K             *types.BigInt        `json:"k"`
	BallotMode    *types.BallotMode    `json:"ballotMode"`
	Weight        *types.BigInt        `json:"weight"`
	FieldValues   []*types.BigInt      `json:"fieldValues"`
}

func (i *BallotProofWasmInputs) Unmarshal(v js.Value) error {
	res, err := FromJSONValue[JSONBallotProofWasmInputs](v)
	if err != nil {
		return err
	}
	inputs := BallotProofWasmInputs{
		Address:   res.Address,
		ProcessID: res.ProcessID,
		Secret:    res.Secret,
	}
	if len(res.EncryptionKey) != 2 {
		return fmt.Errorf("invalid encryption key")
	}
	encryptionKeyX, ok := new(big.Int).SetString(res.EncryptionKey[0], 10)
	if !ok {
		return fmt.Errorf("invalid encryption key X value")
	}
	encryptionKeyY, ok := new(big.Int).SetString(res.EncryptionKey[1], 10)
	if !ok {
		return fmt.Errorf("invalid encryption key Y value")
	}
	inputs.EncryptionKey = &types.EncryptionKey{X: encryptionKeyX, Y: encryptionKeyY}

	k, ok := new(big.Int).SetString(res.K, 10)
	if !ok {
		return fmt.Errorf("invalid K value")
	}
	inputs.K = (*types.BigInt)(k)
	if res.BallotMode == nil {
		return fmt.Errorf("missing ballot mode")
	}
	inputs.BallotMode = &types.BallotMode{
		MaxCount:        res.BallotMode.MaxCount,
		ForceUniqueness: res.BallotMode.ForceUniqueness,
		MaxValue:        (*types.BigInt)(big.NewInt(int64(res.BallotMode.MaxValue))),
		MinValue:        (*types.BigInt)(big.NewInt(int64(res.BallotMode.MinValue))),
		MaxTotalCost:    (*types.BigInt)(big.NewInt(int64(res.BallotMode.MaxTotalCost))),
		MinTotalCost:    (*types.BigInt)(big.NewInt(int64(res.BallotMode.MinTotalCost))),
		CostExponent:    res.BallotMode.CostExponent,
		CostFromWeight:  res.BallotMode.CostFromWeight,
	}
	weight, ok := new(big.Int).SetString(res.Weight, 10)
	if !ok {
		return fmt.Errorf("invalid weight value")
	}
	inputs.Weight = (*types.BigInt)(weight)
	if len(res.FieldValues) == 0 {
		return fmt.Errorf("missing field values")
	}
	for _, v := range res.FieldValues {
		bv, ok := new(big.Int).SetString(v, 10)
		if !ok {
			return fmt.Errorf("invalid field value: %s", v)
		}
		inputs.FieldValues = append(inputs.FieldValues, (*types.BigInt)(bv))
	}

	*i = inputs
	return nil
}

type CircomInputs struct {
	Fields          []string `json:"fields"`
	MaxCount        string   `json:"max_count"`
	ForceUniqueness string   `json:"force_uniqueness"`
	MaxValue        string   `json:"max_value"`
	MinValue        string   `json:"min_value"`
	MaxTotalCost    string   `json:"max_total_cost"`
	MinTotalCost    string   `json:"min_total_cost"`
	CostExp         string   `json:"cost_exp"`
	CostFromWeight  string   `json:"cost_from_weight"`
	Address         string   `json:"address"`
	Weight          string   `json:"weight"`
	ProcessID       string   `json:"process_id"`
	PK              []string `json:"pk"`
	K               string   `json:"k"`
	CipherFields    []string `json:"cipherfields"`
	Nullifier       string   `json:"nullifier"`
	Commitment      string   `json:"commitment"`
	Secret          string   `json:"secret"`
	InputsHash      string   `json:"inputs_hash"`
}

type BallotProofWasmResult struct {
	CircuitInputs *CircomInputs  `json:"circuitInputs"`
	SignatureHash types.HexBytes `json:"signatureHash"`
}

type JSONBallotMode struct {
	MaxCount        uint8 `json:"maxCount"`
	ForceUniqueness bool  `json:"forceUniqueness"`
	MaxValue        uint  `json:"maxValue"`
	MinValue        uint  `json:"minValue"`
	MaxTotalCost    uint  `json:"maxTotalCost"`
	MinTotalCost    uint  `json:"minTotalCost"`
	CostExponent    uint8 `json:"costExponent"`
	CostFromWeight  bool  `json:"costFromWeight"`
}

type JSONBallotProofWasmInputs struct {
	Address       types.HexBytes  `json:"address"`
	ProcessID     types.HexBytes  `json:"processID"`
	Secret        types.HexBytes  `json:"secret"`
	EncryptionKey []string        `json:"encryptionKey"`
	K             string          `json:"k"`
	BallotMode    *JSONBallotMode `json:"ballotMode"`
	Weight        string          `json:"weight"`
	FieldValues   []string        `json:"fieldValues"`
}
