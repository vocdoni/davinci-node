//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/vocdoni/davinci-node/types"
)

// FromJSONValue converts a JavaScript value to a Go struct of type T.
// It expects a JSON string that can be unmarshaled into the struct.
func FromJSONValue[T any](v js.Value) (T, error) {
	if v.IsNull() || v.IsUndefined() {
		return *new(T), fmt.Errorf("value is null or undefined")
	}
	if v.Type() != js.TypeString {
		return *new(T), fmt.Errorf("expected struct encoded into JSON string")
	}
	var result T
	if err := json.Unmarshal([]byte(v.String()), &result); err != nil {
		return *new(T), fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return result, nil
}

// FromUint8 converts a JavaScript value to an uint8 type. It expects a number
// that represents a uint8 value. If the value is not a valid uint8 number, it
// returns an error.
func FromUint8(v js.Value) (uint8, error) {
	if v.Type() != js.TypeNumber {
		return 0, fmt.Errorf("value is not a number")
	}
	return uint8(v.Int()), nil
}

// FromHexBytes converts a JavaScript value to a HexBytes type. It expects a
// string that represents a hexadecimal value. If the value is not a valid hex
// string, it returns an error.
func FromHexBytes(v js.Value) (types.HexBytes, error) {
	// check if is a valid hex string
	if v.Type() != js.TypeString {
		return types.HexBytes{}, fmt.Errorf("value provided is not a string")
	}
	// parse the private key seed
	hexValue, err := types.HexStringToHexBytes(v.String())
	if err != nil {
		return types.HexBytes{}, fmt.Errorf("value provided is not a valid hex string")
	}
	return hexValue, nil
}

// JSResult creates a JavaScript object with data and/or error fields,
// compatible with both browser and Node.js environments.
func JSResult(data any, err ...error) js.Value {
	res := map[string]any{}
	if data != nil {
		res["data"] = data
	}
	if len(err) > 0 {
		strErr := make([]string, len(err))
		for i, e := range err {
			strErr[i] = e.Error()
		}
		res["error"] = strings.Join(strErr, ", ")
	}

	return js.ValueOf(res)
}
