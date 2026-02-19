#!/bin/sh
set -eu

echo "🚀 Starting Sequencer initialization..."

# Fetch contract addresses from deployer
echo "📡 Fetching contract addresses from deployer..."
curl -s http://deployer:8000/addresses.env > /tmp/addresses.env

if [ ! -s /tmp/addresses.env ]; then
    echo "❌ Failed to fetch addresses.env from deployer"
    exit 1
fi

echo "✅ Successfully fetched addresses.env"

# Source the environment file to load contract addresses
. /tmp/addresses.env

# Verify we got the addresses
if [ -z "$PROCESS_REGISTRY" ] || [ -z "$ORG_REGISTRY" ]; then
    echo "❌ Failed to load contract addresses from environment file"
    echo "Environment file content:"
    cat /tmp/addresses.env
    exit 1
fi

echo "📋 Contract addresses loaded:"
echo "  ProcessRegistry: $PROCESS_REGISTRY"

# Set environment variables for the sequencer
export DAVINCI_WEB3_PROCESS="$PROCESS_REGISTRY"
export DAVINCI_WEB3_ORGS="$ORG_REGISTRY"

echo "🔧 Environment variables set:"
echo "  DAVINCI_WEB3_PROCESS=$DAVINCI_WEB3_PROCESS"
echo "  DAVINCI_WEB3_ORGS=$DAVINCI_WEB3_ORGS"

# Clean up
rm -f /tmp/addresses.env

echo "🎯 Starting Sequencer with contract addresses..."

# Execute the original entrypoint with all arguments
exec davinci-sequencer "$@"
