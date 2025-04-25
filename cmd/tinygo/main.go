package main

//export encrypt
func encrypt(val int32) int32 {
   return val * 2
}

// The main entrypoint for WASI; kept empty to prevent deadlocks.
func main() {}

