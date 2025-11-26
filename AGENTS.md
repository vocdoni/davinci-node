# AGENTS.md

## Curated guidance
- Treat this AGENTS.md as the single source that distills historical reviewers’ expectations. Use it for every AI review, test run, and documentation update.
- Keep these rules up-to-date: add patterns when they become common, update standards as they evolve, and remove outdated guidance. The agent tooling applies this file automatically, so changes here matter.

## Architecture & dependencies
- Expect Go services to include API layers, background workers, persistence stacks, and optional crypto/math helpers. Prefer `go-chi`/`cors` for routing, `zerolog`/`log` for structured logs, `viper`/`pflag` for config, zk tooling (`gnark`, `gnark-crypto`) for proof work, `go-ethereum` for chain plumbing, `pebble`/`goleveldb` for storage, and AWS SDK v2 when interacting with cloud artifacts.
- Keep packages focused per responsibility, design modular interfaces, and configure behavior through environment variables instead of hard-coded constants.

## Style & Go idioms
1. Always run `gofmt` before submitting changes; let the formatter handle indentation and alignment.
2. Use structured logging (`log.Debugw`, `log.Errorw`, `log.Infow`) with contextual fields (IDs, durations, counts); prefer these over `fmt.Printf`.
3. Create errors via `fmt.Errorf` (not `errors.New`) so `%w` wrapping is always possible. Wrap every caller with context (`fmt.Errorf("fetch user: %w", err)`) and include the operation name.
4. Use `bytes.Equal` or direct array comparisons for slices, and avoid conversions (e.g., `common.BytesToAddress`) unless you truly need the struct.
5. Favor early returns, drop redundant `else` blocks, and keep control flow shallow so reviewers can focus on the important branch.
6. Exported identifiers must have GoDoc comments that start with the identifier’s name. Use descriptive camelCase names and the `test` prefix for `_test.go` helpers.
7. Iterate with `range`, initialize slices/maps/channels via `make`, and call `defer` right after acquiring resources (locks, files, contexts).
8. Prefer composition over inheritance; embed structs only when it simplifies delegation.
9. Drive concurrency through channels and semaphores, drain completion signals, and guard cancellation with `select`+`default`.
10. Reserve `panic`+`recover` for deliberate parse failures and re-panic when the value isn’t the expected type.
11. Use `select` with `default` for non-blocking cases (e.g., returning buffers to a pool); rely on buffered channels to cap throughput.
12. Build resource pools via buffered channels that drop excess entries so garbage collection can reclaim unused buffers.
13. Prefer request-specific reply channels or channel-of-channel patterns when concurrent callers need individual responses.

## Safety & concurrency
- Guard every pointer before dereferencing; if a pointer isn’t needed, accept a value instead (reducing nil checks).
- Wrap external calls with contexts (`context.WithTimeout`, `WithCancel`) and cancel them immediately once the work is done.
- Protect storage transactions with locks/`defer` unlocks; ensure iterators release resources even if contexts cancel.
- Use buffered channel semaphores to throttle goroutines; draining completion channels prevents leaks.
- Document and handle asynchronous errors—don’t ignore the result of sends/receives that can block or fail.

## Testing & automation
1. Run `go test ./...` and `go test ./... -race` when concurrency or shared state is affected.
2. Re-run heavyweight automation commands whenever your change touches circuits, wasm artifacts, contract deployments, or CLI jobs.
3. Integration suites (Docker/Testcontainers) combine API, worker, and persistence flows; rerun them when contracts or artifacts are touched.
4. Favor table-driven tests with subtests; keep helpers localized to their test files.
5. Track TODOs (especially those touching telemetry, docs, or proto exports) and open follow-up issues when they remain.

## Tooling & maintenance
- Run `go vet`, `golangci-lint`, and `go mod tidy` when adjusting imports or tooling.
- Use short declarations (`:=`) for new variables and keep the scope tight to prevent accidental reuse.

## HTTP & client-facing behavior
- Wrap raw SQL/internal errors before returning them to clients. Messages should identify the failure and map to appropriate HTTP statuses (404 for missing resources, 204 for empties, etc.).

## Go testing patterns (QuickTest)
- Use `c.Assert(value, qt.HasLen, N)` for length checks instead of comparing `len`.
- Prefer `qt.DeepEquals`, `qt.IsNil`, `qt.Not(qt.IsNil)`, `qt.IsTrue`, `qt.IsFalse`, and `qt.Contains` over manual comparisons.
- Avoid anti-patterns like `c.Assert(len(items), qt.Equals, N)` or `c.Assert(obj == nil, qt.Equals, false)`.

## Communication expectations
- Be factually rigorous; don’t invent or speculate unless you clearly label it as such.
- Question unclear statements; avoid reflexive agreement or hype language.
- Respond directly and concisely; skip filler.
- Admit uncertainty explicitly and ask for clarification when necessary.
- Prioritize accuracy over politeness and don’t oversell capabilities.

## PR expectations
- Title format: `[<component>] <brief description>` when applicable; otherwise, use a concise but descriptive title.
- Document exported API/config changes with GoDoc comments and provide Example functions when they clarify usage.
- List structured logging fields, error wrapping, nil guards, and any control-flow simplifications in the PR body.
- Mention the exact commands needed to reproduce heavy CI steps so reviewers can rerun them verbatim.

## Example language (tone)
- “Use `log.Debugw()` always and attach fields.”
- “This change touches concurrency—please run `go test -race` and justify the goroutine limits.”
- “Document the exported config settings so downstream users know how to enable them.”
