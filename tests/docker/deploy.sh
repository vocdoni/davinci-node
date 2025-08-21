#!/usr/bin/env bash
set -euo pipefail

apt install 

# wait for Anvil’s RPC
echo -n "⏳ Waiting for Anvil at anvil:8545"
while ! (echo >/dev/tcp/anvil/8545) &>/dev/null; do
  printf "."
  sleep 1
done
echo " ✅"

# remove any existing davinci-contracts directory
if [ -d davinci-contracts ]; then
  rm -rf davinci-contracts
fi

BRANCH=${BRANCH:-main}
echo "📥 Cloning davinci-contracts branch: $BRANCH"
git clone https://github.com/vocdoni/davinci-contracts.git
cd davinci-contracts

echo "🔍 Using commit: ${COMMIT:-latest}"

# fetch and checkout
if [ -n "${COMMIT:-}" ]; then
  echo "🔀 Checking out specific commit: $COMMIT"
  git fetch origin "$COMMIT" || echo "⚠️  Could not fetch commit directly (may already be present)"
  git checkout "$COMMIT"
else
  BRANCH=${BRANCH:-main}
  echo "🔀 No COMMIT set, checking out latest from branch: $BRANCH"
  git checkout "$BRANCH"
  git pull origin "$BRANCH"
fi

head -n -5 foundry.toml > foundry.tmp && mv foundry.tmp foundry.toml

cp .env.example .env

export CHAIN_ID=1337

forge clean && forge build

forge script \
  --chain-id 1337 \
  script/DeployAll.s.sol:DeployAllScript \
  --rpc-url http://anvil:8545 \
  --broadcast \
  --slow \
  --optimize \
  --optimizer-runs 200 \
  -- \
  --vvvv

# 4) extract addresses into JSON
OUTPUT=/addresses.json
cp broadcast/DeployAll.s.sol/1337/run-latest.json $OUTPUT

echo "✅ Addresses written to $OUTPUT"
