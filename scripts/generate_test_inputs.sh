#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF' >&2
Usage: scripts/generate_test_inputs.sh [ci.log] [output.sol]

The first argument is the path to the integration log file to parse, run the
following command to generate it:

    go test -run ^TestIntegration$ github.com/vocdoni/davinci-node/tests -timeout=1h -v -count=1 > ci.log

Extracts the statetransition and results artifacts from the integration log
and renders a Solidity TestInputs contract that mirrors the data observed in
the run.
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

command -v jq >/dev/null || {
    echo "error: jq is required but not found in PATH" >&2
    exit 1
}

LOG_PATH=${1:-ci.log}
OUTPUT_PATH=${2:-output.sol}
# Keep the organization address stable unless explicitly overridden.
ORGANIZATION_ADDRESS=${ORGANIZATION_ADDRESS:-0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266}

if [[ ! -f "$LOG_PATH" ]]; then
    echo "error: log file '$LOG_PATH' not found" >&2
    exit 1
fi

tmp_log=$(mktemp)
invalid_inputs_json=$(mktemp)
valid_inputs_json=$(mktemp)
invalid_proof_json=$(mktemp)
valid_proof_json=$(mktemp)
results_inputs_json=$(mktemp)
results_proof_json=$(mktemp)

cleanup() {
    rm -f "$tmp_log" "$invalid_inputs_json" "$valid_inputs_json" "$invalid_proof_json" \
        "$valid_proof_json" "$results_inputs_json" "$results_proof_json"
}
trap cleanup EXIT

# Strip ANSI escape sequences once so parsing becomes deterministic.
perl -pe 's/\x1B\[[0-9;]*[A-Za-z]//g' "$LOG_PATH" >"$tmp_log"

extract_field() {
    local line=$1
    local key=$2
    local segment=${line#*${key}=}
    if [[ "$segment" == "$line" ]]; then
        return 1
    fi
    echo "${segment%% *}"
}

extract_json_block() {
    local line=$1
    local key=$2
    local next_key=${3:-}
    local segment=${line#*${key}=}
    if [[ "$segment" == "$line" ]]; then
        return 1
    fi
    if [[ -n "$next_key" ]]; then
        local marker=" ${next_key}="
        segment=${segment%%${marker}*}
    fi
    echo "$segment"
}

decode_json_string() {
    local raw=$1
    python3 - "$raw" <<'PY'
import json
import sys

raw = sys.argv[1]
def load_json(value):
    try:
        return json.loads(value)
    except json.JSONDecodeError as exc:
        sys.stderr.write(f"failed to decode json: {exc}\n")
        sys.exit(1)


data = load_json(raw)
if isinstance(data, str):
    data = load_json(data)


def stringify_ints(value):
    if isinstance(value, int):
        return str(value)
    if isinstance(value, list):
        return [stringify_ints(item) for item in value]
    if isinstance(value, dict):
        return {key: stringify_ints(val) for key, val in value.items()}
    return value


json.dump(stringify_ints(data), sys.stdout)
PY
}

readarray -t transition_lines < <(grep -F "proof ready to submit to the contract" "$tmp_log")
if ((${#transition_lines[@]} < 2)); then
    echo "error: expected at least two state transition entries in the log" >&2
    exit 1
fi

invalid_line=${transition_lines[0]}
valid_line=${transition_lines[1]}

results_line=$(grep -F "verified results ready to upload to contract" "$tmp_log" | head -n1 || true)
if [[ -z "$results_line" ]]; then
    echo "error: unable to find results verifier entry in the log" >&2
    exit 1
fi

invalid_inputs_raw=$(extract_json_block "$invalid_line" "strInputs" "strProof") || {
    echo "error: failed to parse invalid statetransition inputs" >&2
    exit 1
}
valid_inputs_raw=$(extract_json_block "$valid_line" "strInputs" "strProof") || {
    echo "error: failed to parse valid statetransition inputs" >&2
    exit 1
}
invalid_proof_raw=$(extract_json_block "$invalid_line" "strProof") || {
    echo "error: failed to parse invalid statetransition proof" >&2
    exit 1
}
valid_proof_raw=$(extract_json_block "$valid_line" "strProof") || {
    echo "error: failed to parse valid statetransition proof" >&2
    exit 1
}
results_inputs_raw=$(extract_json_block "$results_line" "strInputs" "strProof") || {
    echo "error: failed to parse results inputs" >&2
    exit 1
}
results_proof_raw=$(extract_json_block "$results_line" "strProof") || {
    echo "error: failed to parse results proof" >&2
    exit 1
}

decode_json_string "$invalid_inputs_raw" >"$invalid_inputs_json"
decode_json_string "$valid_inputs_raw" >"$valid_inputs_json"
decode_json_string "$invalid_proof_raw" >"$invalid_proof_json"
decode_json_string "$valid_proof_raw" >"$valid_proof_json"
decode_json_string "$results_inputs_raw" >"$results_inputs_json"
decode_json_string "$results_proof_raw" >"$results_proof_json"

declare -A blob_hash_by_after
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    blob_hash=$(extract_field "$line" "blobHash" || true)
    root_after=$(extract_field "$line" "rootHashAfter" || true)
    if [[ -n "$blob_hash" && -n "$root_after" ]]; then
        blob_hash_by_after["$root_after"]=$blob_hash
    fi
done < <(grep -F "state transition proof generated" "$tmp_log")

valid_root_before=$(jq -r '.rootHashBefore' "$valid_inputs_json")
valid_root_after=$(jq -r '.rootHashAfter' "$valid_inputs_json")
invalid_root_after=$(jq -r '.rootHashAfter' "$invalid_inputs_json")
voters_count=$(jq -r '.votersCount // .numNewVotes' "$valid_inputs_json")
overwritten_votes_count=$(jq -r '.overwrittenVotesCount // .numOverwritten' "$valid_inputs_json")
census_root=$(jq -r '.censusRoot' "$valid_inputs_json")
mapfile -t blob_commitment_limbs < <(jq -r '.blobCommitmentLimbs[]?' "$valid_inputs_json")
if ((${#blob_commitment_limbs[@]} != 3)); then
    echo "error: expected three blobCommitmentLimbs entries in statetransition inputs" >&2
    exit 1
fi

valid_blob_hash=${blob_hash_by_after["$valid_root_after"]-}
if [[ -z "$valid_blob_hash" ]]; then
    echo "error: could not match blob hash for rootHashAfter=$valid_root_after" >&2
    exit 1
fi

statetransition_abi_inputs=$(extract_field "$valid_line" "abiInputs") || {
    echo "error: missing abiInputs in valid statetransition" >&2
    exit 1
}
statetransition_abi_proof=$(extract_field "$valid_line" "abiProof") || {
    echo "error: missing abiProof in valid statetransition" >&2
    exit 1
}
statetransition_abi_proof_invalid=$(extract_field "$invalid_line" "abiProof") || {
    echo "error: missing abiProof in invalid statetransition" >&2
    exit 1
}
results_abi_inputs=$(extract_field "$results_line" "abiInputs") || {
    echo "error: missing abiInputs in results entry" >&2
    exit 1
}
results_abi_proof=$(extract_field "$results_line" "abiProof") || {
    echo "error: missing abiProof in results entry" >&2
    exit 1
}

if [[ -n "${ORGANIZATION_ADDRESS:-}" ]]; then
    organization_address=$ORGANIZATION_ADDRESS
else
    processid_value=$(extract_field "$valid_line" "processID") || {
        echo "error: missing process id" >&2
        exit 1
    }
    organization_address=${processid_value:0:42}
fi

mapfile -t st_ar < <(jq -r '.proof.Ar[]' "$valid_proof_json")
mapfile -t st_bs0 < <(jq -r '.proof.Bs[0][]' "$valid_proof_json")
mapfile -t st_bs1 < <(jq -r '.proof.Bs[1][]' "$valid_proof_json")
mapfile -t st_krs < <(jq -r '.proof.Krs[]' "$valid_proof_json")
mapfile -t st_commitments < <(jq -r '.commitments[]' "$valid_proof_json")
mapfile -t st_commitment_pok < <(jq -r '.commitment_pok[]' "$valid_proof_json")

results_state_root=$(jq -r '.stateRoot' "$results_inputs_json")
mapfile -t final_results < <(jq -r '.results[]' "$results_inputs_json")

mapfile -t results_ar < <(jq -r '.proof.Ar[]' "$results_proof_json")
mapfile -t results_bs0 < <(jq -r '.proof.Bs[0][]' "$results_proof_json")
mapfile -t results_bs1 < <(jq -r '.proof.Bs[1][]' "$results_proof_json")
mapfile -t results_krs < <(jq -r '.proof.Krs[]' "$results_proof_json")
mapfile -t results_commitments < <(jq -r '.commitments[]' "$results_proof_json")
mapfile -t results_commitment_pok < <(jq -r '.commitment_pok[]' "$results_proof_json")

if [[ "$results_state_root" != "$valid_root_after" ]]; then
    echo "error: results stateRoot ($results_state_root) does not match statetransition rootHashAfter ($valid_root_after)" >&2
    exit 1
fi

normalize_hex() {
    local value=$1
    value=${value#0x}
    echo "${value,,}"
}

format_hex_bytes() {
    local value
    value=$(normalize_hex "$1")
    echo "hex\"$value\""
}

format_array_inline() {
    local separator=$1
    shift
    local out=""
    local first=1
    for element in "$@"; do
        if ((first)); then
            out=$element
            first=0
        else
            out+="${separator}${element}"
        fi
    done
    echo "$out"
}

cat >"$OUTPUT_PATH" <<EOF
// SPDX-License-Identifier: AGPL-3.0-or-later
pragma solidity ^0.8.28;

abstract contract TestInputs {
    address public constant ORGANIZATION_ADDRESS = $organization_address;

    uint256 public constant ROOT_HASH_BEFORE =
        $valid_root_before;
    uint256 public constant ROOT_HASH_AFTER =
        $valid_root_after;
    uint256 public constant ROOT_HASH_AFTER_BAD =
        $invalid_root_after;
    uint256 public constant VOTERS_COUNT = $voters_count;
    uint256 public constant OVERWRITTEN_VOTES_COUNT = $overwritten_votes_count;
    uint256 public constant CENSUS_ROOT = $census_root;
    uint256 public constant BLOBS_COMMITMENT_L1 = ${blob_commitment_limbs[0]};
    uint256 public constant BLOBS_COMMITMENT_L2 = ${blob_commitment_limbs[1]};
    uint256 public constant BLOBS_COMMITMENT_L3 = ${blob_commitment_limbs[2]};

    bytes32 public constant BLOB_VERSIONEDHASH = $(format_hex_bytes "$valid_blob_hash");

    bytes public constant STATETRANSITION_ABI_PROOF =
        $(format_hex_bytes "$statetransition_abi_proof");

    bytes public constant STATETRANSITION_ABI_PROOF_INVALID =
        $(format_hex_bytes "$statetransition_abi_proof_invalid");

    bytes public constant STATETRANSITION_ABI_INPUTS =
        $(format_hex_bytes "$statetransition_abi_inputs");

    uint256[2] public STATETRANITION_PROOF_AR = [
        ${st_ar[0]},
        ${st_ar[1]}
    ];
    uint256[2][2] public STATETRANITION_PROOF_BS = [
        [
            ${st_bs0[0]},
            ${st_bs0[1]}
        ],
        [
            ${st_bs1[0]},
            ${st_bs1[1]}
        ]
    ];
    uint256[2] public STATETRANITION_PROOF_KRS = [
        ${st_krs[0]},
        ${st_krs[1]}
    ];

    uint256[2] public STATETRANITION_PROOF_COMMITMENTS = [
        ${st_commitments[0]},
        ${st_commitments[1]}
    ];

    uint256[2] public STATETRANITION_PROOF_COMMITMENTSPOK = [
        ${st_commitment_pok[0]},
        ${st_commitment_pok[1]}
    ];

    bytes public constant RESULTS_ABI_PROOF =
        $(format_hex_bytes "$results_abi_proof");

    bytes public constant RESULTS_ABI_INPUTS =
        $(format_hex_bytes "$results_abi_inputs");

    uint256[2] public RESULTS_PROOF_AR = [
        ${results_ar[0]},
        ${results_ar[1]}
    ];
    uint256[2][2] public RESULTS_PROOF_BS = [
        [
            ${results_bs0[0]},
            ${results_bs0[1]}
        ],
        [
            ${results_bs1[0]},
            ${results_bs1[1]}
        ]
    ];
    uint256[2] public RESULTS_PROOF_KRS = [
        ${results_krs[0]},
        ${results_krs[1]}
    ];

    uint256[2] public RESULTS_PROOF_COMMITMENTS = [
        ${results_commitments[0]},
        ${results_commitments[1]}
    ];

    uint256[2] public RESULTS_PROOF_COMMITMENTSPOK = [
        ${results_commitment_pok[0]},
        ${results_commitment_pok[1]}
    ];

    uint256[8] public FINAL_RESULTS = [$(format_array_inline ", " "${final_results[@]}")];
}
EOF

echo "wrote $OUTPUT_PATH from $LOG_PATH"
