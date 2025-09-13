#!/usr/bin/env bash
set -euo pipefail

# Install git (required for cloning contracts repo)
apt-get update && apt-get install -y git

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
  echo "🔀 Resolving revision: $COMMIT"
  # Make sure we have up-to-date refs (branches + tags)
  git fetch --all --tags --prune --quiet

  # Try to resolve the input to a full commit id. This works for branch, tag, SHA, or abbrev SHA.
  if ! FULL_SHA=$(git rev-parse --verify --quiet "$COMMIT^{commit}"); then
    echo "❌ Could not resolve '$COMMIT' to a commit (branch/tag/SHA)."
    echo "   Tip: use a branch, tag, or a full 40-char commit hash."
    exit 1
  fi

  echo "🔐 Detaching at $FULL_SHA"
  git -c advice.detachedHead=false checkout --quiet --detach "$FULL_SHA"
else
  BRANCH=${BRANCH:-main}
  echo "🔀 No COMMIT set, checking out latest from branch: $BRANCH"
  git fetch origin "$BRANCH" --quiet
  git checkout --quiet "$BRANCH"
  git pull --quiet origin "$BRANCH"
fi

head -n -5 foundry.toml > foundry.tmp && mv foundry.tmp foundry.toml

cp .env.example .env

export CHAIN_ID=1337
export PRIVATE_KEY=${SEPOLIA_PRIVATE_KEY}

forge clean && forge build

forge script \
  script/DeployAll.s.sol:DeployAllScript \
  --rpc-url http://anvil:8545 \
  --chain-id 1337 \
  --broadcast \
  --slow \
  --optimize \
  --optimizer-runs 200 \
  -vvvv

# 4) extract addresses into JSON
OUTPUT=/addresses.json
cp broadcast/DeployAll.s.sol/1337/run-latest.json $OUTPUT

echo "✅ Addresses written to $OUTPUT"
