#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
NC='\033[0m' # No Color

# Function to print colored output
print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${WHITE}$1${NC}"
    echo -e "${BLUE}================================${NC}"
}

print_info() {
    echo -e "${CYAN}ℹ️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Function to cleanup on exit
cleanup() {
    echo
    print_warning "Shutting down services..."
    docker compose down -v --remove-orphans 2>/dev/null || true
    print_info "Cleanup completed"
}

# Set trap for cleanup
trap cleanup EXIT INT TERM

# Change to testenv directory
cd "$(dirname "$0")"

print_header "🚀 Starting Davinci Node Test Environment"

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    print_error "Docker is not running. Please start Docker and try again."
    exit 1
fi

# Check if docker compose is available
if ! command -v docker compose >/dev/null 2>&1; then
    print_error "docker compose is not installed. Please install it and try again."
    exit 1
fi

print_info "Checking environment configuration..."

# Load environment variables
if [ -f .env ]; then
    source .env
    print_success "Environment file loaded"
else
    print_warning "No .env file found, using defaults"
fi

# Display configuration
print_info "Configuration:"
echo -e "  ${PURPLE}Anvil Port:${NC} ${ANVIL_PORT:-8545}"
echo -e "  ${PURPLE}Deployer Port:${NC} ${DEPLOYER_PORT:-8000}"
echo -e "  ${PURPLE}Sequencer Port:${NC} ${SEQUENCER_PORT:-9090}"
echo -e "  ${PURPLE}Contracts Branch:${NC} ${CONTRACTS_BRANCH:-main}"
echo -e "  ${PURPLE}Node Tag:${NC} ${DAVINCI_NODE_TAG:-main}"

if [ -n "${STOP_AFTER_FETCH:-}" ]; then
    echo -e "  ${PURPLE}Deployer Auto-stop:${NC} ${STOP_AFTER_FETCH} seconds"
fi

echo

# Clean up any existing containers
print_info "Cleaning up existing containers..."
docker compose down -v --remove-orphans >/dev/null 2>&1 || true

# Start services
print_header "🏗️  Building and Starting Services"

print_info "Starting Anvil (Ethereum test network)..."
docker compose up -d anvil

# Wait for Anvil to be ready
print_info "Waiting for Anvil to be ready..."
timeout=30
elapsed=0
while [ $elapsed -lt $timeout ]; do
    if curl -s -X POST -H "Content-Type: application/json" \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        http://localhost:${ANVIL_PORT:-8545} >/dev/null 2>&1; then
        print_success "Anvil is ready!"
        break
    fi
    sleep 1
    elapsed=$((elapsed + 1))
    printf "."
done

if [ $elapsed -ge $timeout ]; then
    print_error "Anvil failed to start within $timeout seconds"
    exit 1
fi

echo

print_info "Starting Deployer (Smart contract deployment)..."
docker compose up -d deployer

# Wait for deployer to be healthy
print_info "Waiting for contracts to be deployed..."
timeout=300  # 5 minutes for contract deployment
elapsed=0
while [ $elapsed -lt $timeout ]; do
    if docker compose ps deployer | grep -q "healthy"; then
        print_success "Contracts deployed successfully!"
        break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    printf "."
done

if [ $elapsed -ge $timeout ]; then
    print_error "Contract deployment failed or timed out"
    print_info "Deployer logs:"
    docker compose logs deployer
    exit 1
fi

echo

# Show contract addresses
print_info "Contract addresses:"
if curl -s http://localhost:${DEPLOYER_PORT:-8000}/addresses.env 2>/dev/null; then
    echo
else
    print_warning "Could not fetch contract addresses"
fi

print_info "Starting Sequencer (Davinci Node)..."
docker compose up -d sequencer

# Wait for sequencer to be ready
print_info "Waiting for Sequencer to be ready..."
timeout=1800 # 30 minutes for sequencer to be ready
elapsed=0
while [ $elapsed -lt $timeout ]; do
    if curl -s http://localhost:${SEQUENCER_PORT:-9090}/info >/dev/null 2>&1; then
        print_success "Sequencer is ready!"
        break
    fi
    sleep 10
    elapsed=$((elapsed + 2))
    printf "."
done

if [ $elapsed -ge $timeout ]; then
    print_error "Sequencer may not be fully ready yet (timeout after $timeout seconds)"
    print_info "Sequencer logs:"
    docker compose logs sequencer
    exit 1
fi

echo

print_header "🎉 Test Environment Started Successfully!"

echo -e "${GREEN}Services are running:${NC}"
echo -e "  ${CYAN}🔗 Anvil RPC:${NC} http://localhost:${ANVIL_PORT:-8545}"
echo -e "  ${CYAN}📄 Contract Addresses:${NC} http://localhost:${DEPLOYER_PORT:-8000}/addresses.env"
echo -e "  ${CYAN}🚀 Sequencer API:${NC} http://localhost:${SEQUENCER_PORT:-9090}"
echo
print_info "To follow the logs: docker compose logs -f --tail=50 sequencer"
echo
