# Build webapp
FROM node:24-alpine AS webapp
WORKDIR /app
# Install pnpm
RUN corepack enable && corepack prepare pnpm@latest --activate
# Copy dependency files
COPY webapp/package.json webapp/pnpm-lock.yaml ./
# Install dependencies with cache mount
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile
# Copy source and build
COPY webapp ./
RUN pnpm run build

# Build Go binary
FROM golang:1.24 AS builder
WORKDIR /src
# Copy go mod files
COPY go.mod go.sum ./
# Download dependencies with cache
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
# Copy source code and helper script
COPY . .
COPY scripts/find-wasmer-lib.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/find-wasmer-lib.sh

# Build with cache mounts and proper library path
ARG BUILDARGS

# Find and copy libwasmer.so dynamically, then build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    # First, prepare a libwasmer.so for the build
    /usr/local/bin/find-wasmer-lib.sh save && \
    # Build the binary
    CGO_ENABLED=1 \
    go build -trimpath \
    -ldflags="-w -s -X=github.com/vocdoni/davinci-node/internal.Version=$(git describe --always --tags --dirty --match='v[0-9]*' 2>/dev/null || echo dev)" \
    -o davinci-sequencer $BUILDARGS ./cmd/davinci-sequencer && \
    # After building, use ldd to find the correct libwasmer.so path
    /usr/local/bin/find-wasmer-lib.sh save-after-build

# Final minimal image
FROM debian:bookworm-slim
WORKDIR /app

# Install runtime dependencies
RUN apt-get update && \
    apt-get install --no-install-recommends -y ca-certificates libc6-dev libomp-dev openmpi-common libgomp1 && \
    apt-get autoremove -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Support for go-rapidsnark witness calculator (https://github.com/iden3/go-rapidsnark/tree/main/witness)
# Copy the helper script and library files from builder
COPY --from=builder /usr/local/bin/find-wasmer-lib.sh /usr/local/bin/
COPY --from=builder /src/libwasmer.so /src/wasmer_path.txt ./
RUN chmod +x /usr/local/bin/find-wasmer-lib.sh && \
    /usr/local/bin/find-wasmer-lib.sh restore && \
    rm /usr/local/bin/find-wasmer-lib.sh libwasmer.so wasmer_path.txt

# Copy binaries and webapp
COPY --from=builder /src/davinci-sequencer ./
COPY --from=webapp /app/dist ./webapp

# Create entrypoint script
RUN echo '#!/bin/sh' > entrypoint.sh && \
    echo 'export SEQUENCER_API_URL=${SEQUENCER_API_URL:-http://localhost:9090}' >> entrypoint.sh && \
    echo 'export BLOCK_EXPLORER_URL=${BLOCK_EXPLORER_URL:-https://sepolia.etherscan.io/address}' >> entrypoint.sh && \
    echo 'if [ -f webapp/config.js ]; then' >> entrypoint.sh && \
    echo '  sed -i "s|__SEQUENCER_API_URL__|${SEQUENCER_API_URL}|g" webapp/config.js' >> entrypoint.sh && \
    echo '  sed -i "s|__BLOCK_EXPLORER_URL__|${BLOCK_EXPLORER_URL}|g" webapp/config.js' >> entrypoint.sh && \
    echo 'fi' >> entrypoint.sh && \
    echo 'exec ./davinci-sequencer "$@"' >> entrypoint.sh && \
    chmod +x entrypoint.sh

EXPOSE 9090
ENTRYPOINT ["/app/entrypoint.sh"]
