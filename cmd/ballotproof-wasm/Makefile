.PHONY: all build

all: build

build:
	@echo "Building WASM binary..."
	GOGC=off GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o ballotproof.wasm .
	wasm-opt --enable-bulk-memory -Oz -o ballotproof.wasm ballotproof.wasm
