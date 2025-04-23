.PHONY: all build start

all: build

build:
	@echo "Building WASM binary for Node.js..."
	cd .. && GOGC=off GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o nodejstest/ballotproof.wasm .

start: build
	@echo "Starting Node.js application..."
	node index.js