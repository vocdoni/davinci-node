//go:build js && wasm
// +build js,wasm

package main

import (
  "syscall/js"
)

// registerCallbacks exposes Go functions to JavaScript via syscall/js.
func registerCallbacks() {
  // encrypt: takes an integer, returns status code
  js.Global().Set("encrypt", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    var v int32
    if len(args) > 0 {
      v = int32(args[0].Int())
    }
    status := encrypt(v)
    return int(status)
  }))

  // Expose result getters
  js.Global().Set("getResultX1", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    return resultX1
  }))

  js.Global().Set("getResultY1", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    return resultY1
  }))

  js.Global().Set("getResultX2", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    return resultX2
  }))

  js.Global().Set("getResultY2", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    return resultY2
  }))

  // Expose commitment and nullifier generator
  js.Global().Set("genCommitmentAndNullifier", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    if len(args) < 3 {
      return 0 // Error code
    }
    address := args[0].String()
    processID := args[1].String()
    secret := args[2].String()
    status := genCommitmentAndNullifier(address, processID, secret)
    return int(status)
  }))

  // Expose commitment and nullifier getters
  js.Global().Set("getCommitment", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    return commitment
  }))

  js.Global().Set("getNullifier", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
    return nullifier
  }))
}

// main is the entry point for wasm; registers callbacks and blocks forever
func main() {
  registerCallbacks()
  // Prevent Go program from exiting
  select {}
}