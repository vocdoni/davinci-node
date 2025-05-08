//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
)

const (
	jsClassName         = "BallotProofWasm"
	jsBallotProofInputs = "proofInputs"
	nArgs               = 1 // hex string with BallotProofWasmInputs bytes
)

func generateProofInputs(args []js.Value) any {
	if len(args) != nArgs {
		return JSResult(nil, fmt.Errorf("Invalid number of arguments, expected %d got %d", nArgs, len(args)))
	}
	// parse the inputs from the first argument
	inputs, err := FromJSONValue[ballotproof.BallotProofWasmInputs](args[0])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid inputs: %v", err))
	}
	// generate the circom inputs
	circomInputs, err := ballotproof.WasmCircomInputs(&inputs)
	if err != nil {
		return JSResult(nil, fmt.Errorf("Error generating circom inputs: %v", err))
	}
	// encode result to json to return it
	bRes, err := json.Marshal(circomInputs)
	if err != nil {
		return JSResult(nil, fmt.Errorf("Error marshaling result: %v", err.Error()))
	}
	return JSResult(string(bRes))
}

// main sets up the JavaScript interface and starts the WASM module
func main() {
	// Create an object to hold the BallotProofWasm functions
	ballotProofClass := js.ValueOf(map[string]any{})
	// Register the proofInputs function
	ballotProofClass.Set(jsBallotProofInputs, js.FuncOf(func(this js.Value, args []js.Value) any {
		return generateProofInputs(args)
	}))
	// Register the class in the global scope so it can be accessed from JavaScript
	js.Global().Set(jsClassName, ballotProofClass)
	// Keep the Go program running
	select {}
}
