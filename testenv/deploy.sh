#!/usr/bin/env bash
set -euo pipefail

# wait for Anvil's RPC
echo -n "⏳ Waiting for Anvil at anvil:8545"
while ! (echo >/dev/tcp/anvil/8545) &>/dev/null; do
  printf "."
  sleep 1
done
echo " ✅"

# clone if necessary
if [ ! -d davinci-contracts ]; then
  BRANCH=${BRANCH:-main}
  echo "📥 Cloning davinci-contracts branch: $BRANCH"
  git clone --branch "$BRANCH" --single-branch https://github.com/vocdoni/davinci-contracts.git
fi
cd davinci-contracts

echo "🔧 Configuring foundry..."
head -n -5 foundry.toml > foundry.tmp && mv foundry.tmp foundry.toml

echo "🏗️ Building contracts..."
forge clean && forge build

echo "🚀 Deploying contracts..."
LOG=$(forge script \
  --chain-id 1337 \
  script/non-proxy/DeployAll.s.sol:TestDeployAllScript \
  --rpc-url http://anvil:8545 \
  --broadcast \
  --slow \
  --optimize \
  --optimizer-runs 200 \
  -- \
  --vvvv)

# echo it so you still see it in CI logs
echo "$LOG"

# 4) extract addresses into JSON and create environment file
OUTPUT_JSON=/workspace/addresses.json
OUTPUT_ENV=/workspace/addresses.env

cp broadcast/DeployAll.s.sol/1337/run-latest.json $OUTPUT_JSON

echo "✅ Addresses written to $OUTPUT_JSON"

# 5) Parse JSON with jq and create environment file
echo "🔧 Parsing contract addresses with jq..."

# Extract contract addresses using jq and create environment variables
PROCESS_REGISTRY=$(jq -r '.transactions[] | select(.contractName == "ProcessRegistry") | .contractAddress' $OUTPUT_JSON)
ORG_REGISTRY=$(jq -r '.transactions[] | select(.contractName == "OrganizationRegistry") | .contractAddress' $OUTPUT_JSON)

# Create environment file
cat > $OUTPUT_ENV << EOF
PROCESS_REGISTRY=$PROCESS_REGISTRY
ORG_REGISTRY=$ORG_REGISTRY
EOF

echo "✅ Environment file created at $OUTPUT_ENV"
echo "📋 Contract addresses:"
echo "  ProcessRegistry: $PROCESS_REGISTRY"
echo "  OrganizationRegistry: $ORG_REGISTRY"
