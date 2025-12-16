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
git clone --recurse-submodules https://github.com/vocdoni/davinci-contracts.git
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

# copy verifier contracts into the cloned repo
CONFIG_SOURCE_DIR=${CONFIG_SOURCE_DIR:-/opt/davinci/config}
VERIFIER_SOURCE_DIR=$CONFIG_SOURCE_DIR
if [ ! -f "${VERIFIER_SOURCE_DIR}/resultsverifier_vkey.sol" ] || [ ! -f "${VERIFIER_SOURCE_DIR}/statetransition_vkey.sol" ]; then
  SCRIPT_SOURCE=${BASH_SOURCE[0]-$0}
  SCRIPT_DIR=$(cd "$(dirname "${SCRIPT_SOURCE}")" && pwd)
  REPO_ROOT=$(cd "${SCRIPT_DIR}/../.." && pwd)
  ALT_CONFIG_DIR="${REPO_ROOT}/config"
  if [ -f "${ALT_CONFIG_DIR}/resultsverifier_vkey.sol" ] && [ -f "${ALT_CONFIG_DIR}/statetransition_vkey.sol" ]; then
    VERIFIER_SOURCE_DIR=$ALT_CONFIG_DIR
  else
    echo "❌ Could not find verifier contracts. Checked ${CONFIG_SOURCE_DIR} and ${ALT_CONFIG_DIR}"
    exit 1
  fi
fi

mkdir -p src/verifiers
cp -f "${VERIFIER_SOURCE_DIR}/resultsverifier_vkey.sol" src/verifiers/
cp -f "${VERIFIER_SOURCE_DIR}/statetransition_vkey.sol" src/verifiers/
echo "📄 Verification keys contracts copied to src/verifiers/"

cp .env.example .env

export CHAIN_ID=1337
export PRIVATE_KEY=${SEPOLIA_PRIVATE_KEY}
export FOUNDRY_DISABLE_NIGHTLY_WARNING=1

forge clean && forge build

forge script \
  script/DeployAll.s.sol:DeployAllScript \
  --rpc-url http://anvil:8545 \
  --chain-id 1337 \
  --broadcast \
  --slow \
  --optimize \
  --optimizer-runs 200 \
  --gas-price 0 \
  --base-fee 0 \
  -vvvv

# 4) extract addresses into JSON
OUTPUT=/addresses.json
cp broadcast/DeployAll.s.sol/1337/run-latest.json $OUTPUT

echo "✅ Addresses written to $OUTPUT"
