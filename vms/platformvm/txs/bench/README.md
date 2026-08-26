# platformvm/txs/bench — reflection codec vs native-ZAP harness

Measures the parse/build/field/workload deltas between the current
reflection codec path (`txs.Parse` / `codec.Manager.Marshal`) and the
native-ZAP path Blue is building under
`vms/platformvm/txs/zap_native`.

## Status

**`zap_native` has landed AdvanceTimeTx as the LP-023 canary.** That
is the one tx type with a real native path today; benchmarks report
the real 37× parse / 5.2× build lift for it.

Every other tx type (BaseTx, AddPermissionlessValidator, etc.) is
still legacy-only on both sides because Blue's per-type accessors
have not yet been written. The per-type tables in
`../bench_results/RESULTS.md` mark these `_legacy only_` so the
honest reader can tell which numbers reflect the real native path
and which are baseline-only.

## Reproduce

From repo root:

```bash
cd ~/work/lux/node

# Full bench (≈3 minutes per architecture at -benchtime=2s)
GOWORK=off go test -count=1 \
  -bench='^Benchmark' -benchmem -benchtime=2s -run='^$' \
  -cpuprofile=/tmp/bench_cpu.prof \
  ./vms/platformvm/txs/bench/

# Per-type parse only
GOWORK=off go test -count=1 -bench='^BenchmarkParse' -benchmem -benchtime=2s -run='^$' \
  ./vms/platformvm/txs/bench/

# Real-workload only
GOWORK=off go test -count=1 -bench='^BenchmarkWorkload' -benchmem -benchtime=5s -run='^$' \
  ./vms/platformvm/txs/bench/

# Alloc pressure (5s parse loop each codec, AdvanceTimeTx fixture)
GOWORK=off go test -count=1 -bench='^BenchmarkAllocPressure' -benchmem -benchtime=1x -run='^$' \
  ./vms/platformvm/txs/bench/

# Disable-legacy gate verification
GOWORK=off go test -count=1 -run TestLegacyCodecGate -v \
  ./vms/platformvm/txs/bench/
```

The bench scope is the v1 (current) codec only. v0 is read-only on the
production node; benching its write path would measure dead code.

## Files

- `fixtures.go` — representative fixtures for each tx type (2-in/2-out
  base, realistic stake / signer / rewards-owner counts).
- `parse_bench_test.go` — per-type Parse: legacy + native-AdvanceTime.
- `build_bench_test.go` — per-type Build: legacy + native-AdvanceTime.
- `field_bench_test.go` — field-access after-parse: legacy vs native.
- `workload_bench_test.go` — 1000-tx mempool, 200-block parse.
- `alloc_bench_test.go` — sustained-parse GC/alloc profile.
- `disable_legacy_test.go` — `LUXD_ENABLE_LEGACY_CODEC` gate behavior
  (default off + subprocess on).
- `testdata/` — captured mainnet bytes when present.

## Capturing real mainnet inputs

The synthetic mempool/block fixtures use the modal-mainnet mix
documented in `workload_bench_test.go::mainnetMix`. For absolute
ground-truth numbers, drop captured bytes into `testdata/`:

### Mempool dump

```bash
POD=$(kubectl -n lux get pods -l app=luxd -o jsonpath='{.items[0].metadata.name}')
kubectl -n lux exec "$POD" -- curl -s http://localhost:9650/v1/bc/P \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"platform.getMempool"}' \
  > /tmp/mempool.json

# For each txID in the result, fetch the canonical signed bytes and
# pack into testdata/ as the length-prefixed stream the harness reads.
go run ../../../../../scripts/capture-mempool/main.go \
  --rpc http://<node-host>:9650 \
  --out testdata/mainnet-mempool-1000.bytes \
  --max 1000
```

(The `capture-mempool` helper is intentionally separate from this
package — the bench harness should not pull in a kubectl/RPC stack at
build time. Re-encode raw bytes into `testdata/` as a one-shot.)

When `testdata/mainnet-mempool-1000.bytes` is present, the harness
prefers it over the synthetic mix. The log line at bench start tells
you which input was used.

## Disable-legacy gate verification

The env gate is **`LUXD_ENABLE_LEGACY_CODEC=1`** (per Blue's LP-023):
native ZAP is the default; legacy is opt-in. The flag is read at
`zap_native.LegacyEnabled` once per process.

The bench harness verifies two cases:

1. **`TestLegacyCodecGateDefault`** — default; native ZAP picked for
   write; `zap_native.LegacyEnabled == false`.
2. **`TestLegacyCodecGateEnabledViaSubprocess`** — re-execs with
   `LUXD_ENABLE_LEGACY_CODEC=1`; legacy enabled; pre-activation
   timestamps pick legacy for write, post-activation picks ZAP.

The subprocess pattern is required because `zap_native.LegacyEnabled`
is initialized once at package load — flipping the env var mid-process
is a no-op.

**Important architecture note.** The gate is enforced at the new
ZAP-vs-legacy wire dispatcher (`zap_native.IsZAPBytes` and
`zap_native.ShouldUseZAPForWrite`), NOT at `platformvm/txs.Parse`.
The platformvm txs.Parse entry point owns the byte-preserving
v0→TxID migration invariant: any validator bootstrapping from
pre-activation history must be able to read v0 bytes regardless of
the gate. Gating Parse would break this. The gate's enforcement
point is correctly one layer up.

## Caveats

- **Synthetic mempool fixtures repeat the same payload bytes.** The
  codec doesn't know that; parse cost is measured honestly. But
  cache locality favors repeated parses. Drop a real mempool dump
  into `testdata/` to escape this artifact.
- **Bench fixtures are at v1 only.** v0 is the read-only read path on
  the production node; benching its write path would measure dead code.
- **Bench numbers on darwin/arm64 (M-series) do NOT predict
  linux/amd64.** Run on the target architecture for production-relevant
  numbers — the results document records both.
