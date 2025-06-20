.PHONY: all build

all: build

build:
	@echo "Building WASM binary..."
	GOGC=off GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o ballot_proof_inputs.wasm .
	wasm-opt --enable-bulk-memory -Oz -o ballot_proof_inputs.wasm ballot_proof_inputs.wasm
