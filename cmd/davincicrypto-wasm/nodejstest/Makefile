.PHONY: all test

all: test

test:
	@echo "Building WASM binary for Node.js..."
	cd .. && GOGC=off GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o nodejstest/davinci_crypto.wasm .
	@echo "Starting Node.js application..."
	node index.js
