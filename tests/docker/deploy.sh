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

# clone if necessary
if [ ! -d /workspace/contracts-z ]; then
  BRANCH=${BRANCH:-main}
  git clone --branch "$BRANCH" --single-branch https://github.com/vocdoni/contracts-z.git
fi
cd contracts-z

head -n -5 foundry.toml > foundry.tmp && mv foundry.tmp foundry.toml

forge clean && forge build

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

# 4) extract addresses into JSON
OUTPUT=/addresses.json
cp broadcast/DeployAll.s.sol/1337/run-latest.json $OUTPUT

echo "✅ Addresses written to $OUTPUT"