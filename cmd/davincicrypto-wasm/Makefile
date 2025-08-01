.PHONY: all build

all: build

build:
	@echo "Building WASM binary..."
	GOGC=off GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o davinci_crypto.wasm .
	wasm-opt --enable-bulk-memory -Oz -o davinci_crypto.wasm davinci_crypto.wasm
