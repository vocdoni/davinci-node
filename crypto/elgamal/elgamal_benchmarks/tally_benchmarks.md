# ElGamal Tally Benchmarks

## Scope

This report summarizes benchmark results for the current ElGamal tally
implementation in this repository, with emphasis on weighted ERC20 voting and
large accumulator values.

The benchmarks were added in:

- `crypto/elgamal/elgamal_benchmark_test.go`

The core decryption algorithm benchmarked here is:

- `crypto/elgamal/elgamal.go`, `BabyStepGiantStepECC`

The tally finalization path that consumes these decryptions is:

- `sequencer/finalizer.go`

## What is being benchmarked

The final tally is represented as a `uint256[8]` array.

For each tally slot `i`, the finalizer decrypts:

- `ResultsAdd[i]`
- `ResultsSub[i]`

and then computes:

- `Result[i] = ResultsAdd[i] - ResultsSub[i]`

So a full finalization currently performs:

- 8 decryptions for `ResultsAdd`
- 8 decryptions for `ResultsSub`
- 16 decryptions total

Unless otherwise noted, each benchmark result in this document is for one
decryption of one tally slot. For those single-slot benchmarks, estimate
full-tally decryption time by multiplying the per-slot decryption time by 16.
This does not include proof generation or the rest of finalization. The later
"Full tally RSS measurements" section already benchmarks all 16 decryptions.

## Important current stopper

The code currently ships with a fixed decryption bound in `sequencer/finalizer.go`:

```go
const maxValue = 2 << 24 // 33,554,432
```

This is the first hard stop for large weighted tallies.

Example:

- With 2 decimals, a single voter with weight `1,000,000.00` contributes
  `100,000,000` to one tally slot.
- That already exceeds the current fixed bound of `33,554,432`.

So large-weight scenarios do not work today without raising that bound.

## Benchmark environment

All benchmark numbers below were measured on:

- CPU: `12th Gen Intel(R) Core(TM) i7-1270P, 32GB RAM`

## Core algorithm behavior

The current decryption path uses bounded baby-step/giant-step (BSGS).

Practical consequence:

- time grows roughly with `sqrt(maxMessage)`
- memory grows roughly with `sqrt(maxMessage)`

This means decryption cost is driven by the maximum value in one tally slot, not
the sum of all 8 slots together.

## Generic worst-case decryption benchmarks

These are worst-case single-slot decryptions where the plaintext value is equal
to the search bound.

| `maxMessage` | time/op | alloc/op |
| --- | ---: | ---: |
| `33,554,432` | `59 ms` | `9.6 MB` |
| `268,435,456` | `166 ms` | `27.4 MB` |
| `4,294,967,296` | `686 ms` | `109 MB` |
| `68,719,476,736` | `2.84 s` | `438 MB` |
| `170,000,000,000` | `5.47 s` | `680 MB` |

## Weighted voting scenarios

These benchmarks model weighted tallies with different average balances and
decimal truncation choices.

| scenario | total per slot | time/op | alloc/op |
| --- | ---: | ---: | ---: |
| `1 token avg, 0 decimals` | `50,000` | `2.3 ms` | `0.42 MB` |
| `1 token avg, 3 decimals` | `50,000,000` | `72 ms` | `11.8 MB` |
| `1 token avg, 4 decimals` | `500,000,000` | `230 ms` | `37.1 MB` |
| `1 token avg, 5 decimals` | `5,000,000,000` | `739 ms` | `117.8 MB` |
| `1 token avg, 6 decimals` | `50,000,000,000` | `2.37 s` | `371.6 MB` |
| `100 tokens avg, 2 decimals` | `500,000,000` | `231 ms` | `37.1 MB` |

We can conclude that raw ERC20 `18` decimals are not representable within the current `uint64 maxMessage` search bound for realistic totals.


- voters: `10k`, `25k`, `50k`, `100k`, `250k`, `500k`
- average weight: `1.00`, `5.00`, `10.00`, `25.00`, `50.00`, `100.00`

### Representative points

| voters | avg weight | total per slot | time/op | alloc/op |
| --- | ---: | ---: | ---: | ---: |
| `50,000` | `10.00` | `50,000,000` | `73 ms` | `11.9 MB` |
| `50,000` | `100.00` | `500,000,000` | `230 ms` | `37.1 MB` |
| `100,000` | `100.00` | `1,000,000,000` | `345 ms` | `52.9 MB` |
| `500,000` | `10.00` | `500,000,000` | `240 ms` | `37.1 MB` |


With 2 decimals:

- around `50,000,000` per slot is comfortable
- around `500,000,000` per slot is still practical
- around `1,000,000,000` per slot is heavier but still usable on this hardware

With 2 decimals, average weight 1,000,000.00

Per voter, that means one tally slot can grow by:

- `1,000,000.00 * 100 = 100,000,000`

### Measured single-slot decryption results

| voters | total per slot | time/op | alloc/op |
| --- | ---: | ---: | ---: |
| `1` | `100,000,000` | `0.107 s` | `16.6 MB` |
| `5` | `500,000,000` | `0.236 s` | `37.1 MB` |
| `10` | `1,000,000,000` | `0.332 s` | `52.9 MB` |
| `25` | `2,500,000,000` | `0.521 s` | `82.6 MB` |
| `50` | `5,000,000,000` | `0.736 s` | `117.8 MB` |
| `100` | `10,000,000,000` | `1.07 s` | `165.1 MB` |
| `250` | `25,000,000,000` | `1.70 s` | `262.5 MB` |
| `500` | `50,000,000,000` | `2.84 s` | `371.1 MB` |
| `1,000` | `100,000,000,000` | `4.90 s` | `525.1 MB` |
| `2,000` | `200,000,000,000` | `7.16 s` | `742.7 MB` |
| `5,000` | `500,000,000,000` | `10.82 s` | `1.17 GB` |
| `10,000` | `1,000,000,000,000` | `15.25 s` | `1.67 GB` |


Full tally decryption is approximately `16x` the single-slot time:

| voters | approx full decryption time |
| --- | ---: |
| `1,000` | `~78 s` |
| `2,000` | `~115 s` |
| `5,000` | `~173 s` |
| `10,000` | `~244 s` (`~4.1 min`) |


Under a 5-minute decryption budget:

- `10,000` voters at average weight `1,000,000.00` is near the upper edge
- `50,000` voters at average weight `1,000,000.00` is not feasible with the
  current implementation

The one-slot benchmark allocates heavily, but allocation churn is not the same
as peak resident memory. To measure actual RAM use for the real tally shape, we added a full tally RSS section below.

## Full tally RSS measurements

To capture realistic process memory usage, the following benchmark was added:

- `BenchmarkDecryptFullTallyMillionAvg2DecimalsBJJ`

This benchmark performs all 16 decryptions sequentially, matching the current
finalizer structure more closely than the single-slot measurements.

### Full tally benchmark results

| voters | avg weight | full tally time | alloc/op | max RSS |
| --- | ---: | ---: | ---: | ---: |
| `1,000` | `1,000,000.00` | `72.5 s` | `8.40 GB` | included in combined run below |
| `5,000` | `1,000,000.00` | `165.2 s` | `18.73 GB` | included in combined run below |
| `10,000` | `1,000,000.00` | `236.1 s` | `26.75 GB` | `325 MB` |

### Combined RSS sweep

Running the `1,000`, `5,000`, and `10,000` voter full-tally scenarios together
produced:

- maximum resident set size: `324,756 kB` (`~317 MB`)

The isolated `10,000` voter run produced:

- maximum resident set size: `325,328 kB` (`~318 MB`)
- `alloc/op` is allocation churn across the whole run
- `alloc/op` is not the same as peak live heap
- for the current sequential implementation, full tally decryption at the
  measured `10,000` voter, `1,000,000.00` average-weight scenario is time-bound,
  not RAM-bound on a 32 GB machine

For a node planning around `32 GB` RAM:

- decrypt-only RSS is not currently the blocker in the measured scenarios
- wall-clock time is the blocker
- however, this does not include proof generation or the rest of the full
  finalization pipeline, so production memory should still be budgeted above
  the decrypt-only RSS measurements

## Other useful information

- These are worst-case slot values. If weight is guaranteed to be spread across
  multiple slots, actual cost can be lower.
- The relevant bound is on `ResultsAdd[i]` and `ResultsSub[i]`, not only on the
  final `Result[i]`.
- Full tally time is higher than `16x decrypt` because proof generation and the
  rest of finalization are not included in these benchmarks.
- Results are hardware-dependent.
- These results do not imply the current fixed bound in `sequencer/finalizer.go`
  is sufficient. It is not sufficient for large-weight scenarios.
- `alloc/op` and max RSS answer different questions:
  - `alloc/op` shows churn and GC pressure
  - max RSS shows approximate resident RAM usage
