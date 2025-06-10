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
# Copy source code
COPY . .
# Find and copy libwasmer.so to system location before building
# Build with cache mounts and proper library path
ARG BUILDARGS

# Copy the libwasmer.so from the cache to a system location
RUN --mount=type=cache,target=/go/pkg/mod \
    cp /go/pkg/mod/github.com/iden3/wasmer-go@v0.0.1/wasmer/packaged/lib/linux-amd64/libwasmer.so \
    /usr/lib/libwasmer.so

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 \
    go build -trimpath \
    -ldflags="-w -s -X=github.com/vocdoni/davinci-node/internal.Version=$(git describe --always --tags --dirty --match='v[0-9]*' 2>/dev/null || echo dev)" \
    -o davinci-sequencer $BUILDARGS ./cmd/davinci-sequencer

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
# Note: The libwasmer.so path might change, run ldd davinci-sequencer to find the correct path if needed
COPY --from=builder /usr/lib/libwasmer.so \
    /go/pkg/mod/github.com/iden3/wasmer-go@v0.0.1/wasmer/packaged/lib/linux-amd64/libwasmer.so

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
