#!/bin/sh
set -eu

SCRIPT_DIR="$(cd -- "$(dirname -- "$0")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd)"
CONFIG_FILE="${REPO_ROOT}/config/circuit_artifacts.go"

command -v go >/dev/null || {
  echo "error: go is required but not found in PATH" >&2
  exit 1
}

module_dir="$(cd "$REPO_ROOT" && go list -m -f '{{.Dir}}' github.com/vocdoni/davinci-contracts)"
contracts_dir="${module_dir}/src/verifiers"

extract_sol_hash() {
  local file=$1
  sed -n 's/.*PROVING_KEY_HASH = \(0x[0-9a-fA-F]\+\);/\1/p' "$file" | head -n1
}

extract_go_hash() {
  local name=$1
  sed -n "s/.*${name}[[:space:]]*=[[:space:]]*\"\([a-f0-9]\+\)\".*/\1/p" "$CONFIG_FILE" | head -n1
}

check_hash() {
  local name=$1
  local sol_file=$2
  local go_hash
  local sol_hash
  local config_line

  go_hash=$(extract_go_hash "$name")
  if [ -z "$go_hash" ]; then
    echo "need to bump davinci-contracts: failed to read ${name} from ${CONFIG_FILE}" >&2
    exit 1
  fi

  sol_hash=$(extract_sol_hash "$sol_file")
  if [ -z "$sol_hash" ]; then
    echo "need to bump davinci-contracts: failed to read PROVING_KEY_HASH from ${sol_file}" >&2
    exit 1
  fi

  sol_hash="${sol_hash#0x}"

  if [ "$go_hash" != "$sol_hash" ]; then
    config_line=$(grep -F "${name}" "$CONFIG_FILE" | head -n1)
    cat >&2 <<EOF
need to bump davinci-contracts:
${sol_file}
PROVING_KEY_HASH = 0x${sol_hash}

does not match

./config/circuit_artifacts.go
${config_line}
EOF
    exit 1
  fi
}

check_hash "StateTransitionProvingKeyHash" "${contracts_dir}/statetransition_vkey.sol"
check_hash "ResultsVerifierProvingKeyHash" "${contracts_dir}/resultsverifier_vkey.sol"

