//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"
)

func FromJSONValue[T any](v js.Value) (T, error) {
	if v.IsNull() || v.IsUndefined() {
		return *new(T), fmt.Errorf("value is null or undefined")
	}
	if v.Type() != js.TypeString {
		return *new(T), fmt.Errorf("expected struct encoded into hex string")
	}
	var result T
	if err := json.Unmarshal([]byte(v.String()), &result); err != nil {
		return *new(T), fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return result, nil
}

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
