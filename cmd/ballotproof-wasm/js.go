//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"
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
