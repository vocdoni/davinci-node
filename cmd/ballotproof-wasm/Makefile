.PHONY: all build optimize gzip copy

all: build optimize gzip copy

build:
	@echo "Building WASM binary..."
	GOGC=off GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o ts/main.wasm .

optimize:
	@echo "Optimizing WASM binary..."
	wasm-opt --enable-bulk-memory -Oz -o ts/main.wasm ts/main.wasm

gzip:
	@echo "Compressing WASM binary with gzip..."
	gzip -k ts/main.wasm

copy:
	@echo "Copying wasm_exec.js..."
	cp $(shell go env GOROOT)/lib/wasm/wasm_exec.js ./ts/dist
