# Vote Verifier Ballotproof Public Inputs Alignment Design

**Goal:** Make the vote verifier circuit compile and tests pass after the
ballotproof Circom circuit changed to three public inputs and Poseidon hashing.

## Context

The ballotproof Circom circuit now exposes three public inputs in this order:
`inputs_hash`, `address`, `vote_id`. The gnark vote verifier currently builds a
recursive proof witness with only one public input (`BallotHash`). This
mismatch causes compile-time failures in `TestCompileAndPrintConstraints`.
Regenerating Circom artifacts is required so the embedded test assets reflect
the updated circuit interface and hashing changes.

## Design

- Update `voteverifier.VerifyVoteCircuit.verifyCircomProof` to pass three public
  inputs to the recursion verifier in Circom order:
  1. `BallotHash` (already BN254 emulated element)
  2. `Address` (already BN254 emulated element)
  3. `VoteID` converted from `frontend.Variable` to BN254 emulated element
- Regenerate the Circom ballotproof artifacts (`.wasm`, `.zkey`, `.json`) from
  `../davinci-circom` and replace the embedded copies under
  `circuits/test/ballotproof/circom_assets/`.
- Refresh the dummy proof/public input constants in
  `circuits/voteverifier/dummy.go` to match the regenerated artifacts.

## Scope Boundaries

- No changes to signature verification logic, census proof verification, or
  general vote package semantics.
- No refactors unrelated to the public input mismatch.

## Testing

- `RUN_CIRCUIT_TESTS=true go test ./circuits/test/voteverifier -run TestCompileAndPrintConstraints -count=1`
- `RUN_CIRCUIT_TESTS=true go test ./circuits/test/voteverifier -run TestVerifyMerkletreeVoteCircuit -count=1`
- `RUN_CIRCUIT_TESTS=true go test ./circuits/test/voteverifier -run TestVerifyCSPVoteCircuit -count=1`

## Risks

- Public input ordering mismatch between Circom and gnark recursion witness.
- Stale embedded Circom artifacts that no longer match the circuit source.
