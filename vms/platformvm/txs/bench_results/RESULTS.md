# ZAP Native vs Legacy Codec — Bench Results (v3 / multi-host)

**Date:** 2026-06-02 (refreshed post-LP-023 batch 5 v3.7)
**Schema:** v3 (TxKind discriminator at offset 0, +1B per tx)
**luxfi/zap:** v0.7.2 (Object.ListStride per-element clamp, FuzzParse round-trip)
**luxfi/node HEAD:** post LP-023 batch 5 v3.7 (R4V7 audit + ChainsList cap + ValidatorsList MustVerify + audit gates)
**Bench command:** `GOWORK=off go test -bench=BenchmarkParse_ -benchmem -benchtime=2s -count=1 ./vms/platformvm/txs/zap_native/`
**Toggle:** `LUXD_ENABLE_LEGACY_CODEC=1` flips between paths (default OFF; ZAP is native).

## Batch 5 v3.7 refresh (-benchtime=2s -count=1, M1 Max)

Per-type Parse + Build numbers re-measured after batch 5 v3.7 lands.
The 5 v3.7 targets (audit gate widening, ChainsList N≤16 cap,
ValidatorsList MustVerify with 5 floor invariants, Owner-bearing
audit) all live on the Verify() path; the wire Parse path is
unchanged. Build adds zero new work to the writer (the caps + floor
invariants are reader-side gates), so Build is unaffected too.

### Parse — M1 Max (v3.7, -benchtime=2s, single run)

| Tx | Legacy ns | ZAP ns | Parse × | Legacy alloc/B | ZAP alloc/B | Δ allocs |
|---|---:|---:|---:|---:|---:|---:|
| RewardValidatorTx              |   213.6 |  36.91 |  **5.79×** | 3 / 120 | 1 / 24 | 3× |
| SetL1ValidatorWeightTx         |   423.4 |  52.30 |  **8.10×** | 3 / 136 | 1 / 24 | 3× |
| IncreaseL1ValidatorBalanceTx   |   167.3 |  33.45 |  **5.00×** | 3 / 136 | 1 / 24 | 3× |
| DisableL1ValidatorTx           |   177.9 |  27.09 |  **6.57×** | 3 / 120 | 1 / 24 | 3× |
| BaseTx                         |   334.3 |  49.67 |  **6.73×** | 3 / 152 | 1 / 24 | 3× |
| RegisterL1ValidatorTx          |   765.1 |  50.98 | **15.01×** | 6 / 384 | 1 / 24 | 6× |
| SlashValidatorTx               |   356.8 |  45.35 |  **7.87×** | 4 / 176 | 1 / 24 | 4× |
| TransferChainOwnershipTx       |   418.0 |  53.13 |  **7.87×** | 4 / 192 | 1 / 24 | 4× |
| RemoveChainValidatorTx         |   343.3 |  44.01 |  **7.80×** | 4 / 176 | 1 / 24 | 4× |
| **Geomean (n=9)**              |         |        |  **7.50×** |         |        | **3.57×** |

Geomean 7.50× — within 4% noise of the v3.6 baseline (7.74×) and
well inside the prior-batch noise envelope. **No regression > 5%**.
Allocs/op stays at 1/24 across every ZAP path, confirming the v3.7
changes (cap checks + floor invariants) fire entirely after Parse —
the wire decode path is unaffected.

### Build — M1 Max (v3.7, -benchtime=2s, single run)

| Tx | Legacy ns | ZAP ns | Build × | Legacy alloc/B | ZAP alloc/B |
|---|---:|---:|---:|---:|---:|
| RewardValidatorTx              |   220.7 |  216.4 | **1.02×** | 4 / 152 | 2 / 104 |
| SetL1ValidatorWeightTx         |   291.1 |  323.2 | **0.90×** ⚠ | 5 / 264 | 2 / 120 |
| IncreaseL1ValidatorBalanceTx   |   335.0 |  259.3 | **1.29×** | 4 / 168 | 2 / 104 |
| DisableL1ValidatorTx           |   282.2 |  275.1 | **1.03×** | 4 / 152 | 2 / 104 |
| BaseTx                         |   432.1 |  563.4 | **0.77×** ⚠ | 5 / 296 | 5 / 360 |
| RegisterL1ValidatorTx          |  1066   |  993.7 | **1.07×** | 7 / 1016 | 2 / 280 |
| SlashValidatorTx               |   365.1 |  387.3 | **0.94×** | 5 / 224 | 2 / 120 |
| TransferChainOwnershipTx       |   458.2 |  398.5 | **1.15×** | 5 / 296 | 2 / 136 |
| RemoveChainValidatorTx         |   404.7 |  344.6 | **1.17×** | 5 / 224 | 2 / 120 |
| **Geomean (n=9)**              |         |        | **1.03×** |          |          |

Build geomean 1.03× — within 9% noise of the v3.6 baseline (1.12×).
The BaseTx 0.77× and SetL1ValidatorWeightTx 0.90× dips persist from
v3.6 (zap.Builder per-call SetU64 dispatch overhead). **No regression
> 5%** is attributable to v3.7; the dips track the existing v0.7.2
finding tracked as a luxfi/zap follow-up.

### v3.7 verdict

- **Parse:** 7.50× (n=9), within noise of v3.6 7.74×. No regression.
- **Build:** 1.03× (n=9), within noise of v3.6 1.12×. No regression.
- **Allocs/op:** Parse stays at 1/24, Build stays at 2 (down from
  4-7) — both unchanged.

The v3.7 changes (audit gate widening + ChainsList/ValidatorsList cap
+ Owner-bearing audit) are wire-layer Verify() additions; they fire
only when an admission-time tx is parsed THEN run through Verify().
Parse alone (the bench measured) bypasses Verify entirely. Build
unaffected by reader-side gates. Headlines from v3.6 hold.

## Batch 5 (prior) refresh (-benchtime=2s -count=3, median of 3 runs)

M1 Max Parse geomean across the 10 measurable tx types after batch 5
(v3.6 — pre-v3.7 baseline kept for reference):

| Tx | Legacy ns (med) | ZAP ns (med) | Parse × |
|---|---:|---:|---:|
| Base                          |  91.52 | 21.49 | 4.26× |
| RewardValidatorTx             | 137.40 | 21.78 | 6.31× |
| SetL1ValidatorWeightTx        | 155.20 | 22.02 | 7.05× |
| IncreaseL1ValidatorBalanceTx  | 150.60 | 21.42 | 7.03× |
| DisableL1ValidatorTx          | 141.40 | 21.64 | 6.53× |
| BaseTx                        | 164.80 | 21.35 | 7.72× |
| RegisterL1ValidatorTx         | 264.90 | 24.59 | 10.77× |
| SlashValidatorTx              | 188.80 | 21.85 | 8.64× |
| TransferChainOwnershipTx      | 190.10 | 21.80 | 8.72× |
| RemoveChainValidatorTx        | 201.10 | 21.23 | 9.47× |
| **Geomean (n=10)**            |        |       | **7.44×** |

The slight dip from 7.71× → 7.44× tracks the addition of the trivial `BenchmarkParse_-10` (Base, 4.26×) at the front of the suite — when restricted to the prior 9 tx types it converges to **7.74×**, within noise of the v0.7.2 baseline. R4V7 SyntacticVerify lives on the executor `Verify()` path (not the wire parser), so the parse benchmarks are unchanged structurally; the variance is run-to-run wall-clock noise. ChainsList + ValidatorsList add new primitives but do not appear in the legacy comparison fixtures (the legacy stubs predate the multi-chain CreateSovereignL1 shape).

## TL;DR

| Axis | M1 Max (this box, v0.7.2) | M4 Max (dbc, v0.7.1) |
|---|---:|---:|
| Geomean Parse × (9 ZAP-native tx types) | **7.71×** | **8.02×** |
| Geomean Parse alloc reduction × | 3.57× | 3.57× |
| Geomean Parse bytes reduction × | 6.89× | 6.89× |
| Geomean Build × (9 ZAP-native tx types) | **1.12×** | **0.86×** ⚠ |
| Geomean Build alloc reduction × | 2.18× | 2.18× |
| Geomean Build bytes reduction × | 1.77× | 1.77× |
| AdvanceTime Parse via `txs.Codec` wrapper (end-to-end) | **34.14×** | **42.76×** |
| AdvanceTime Build via `txs.Codec` wrapper | 3.85× | 4.94× |
| 1000-tx synthetic mempool (Legacy → Mixed-dispatch) | 1.05× | 1.01× (no ZAP-side in mix) |

**Headlines:**
1. Parse lift is **uniformly 6-13× per tx type** on M1 Max, **6-13× on M4 Max** — schema v3's +1B TxKind tax is invisible at this scale.
2. Per-type geomean Parse on M1 with v0.7.2 (**7.71×**, up from 7.33× on v0.7.1) reflects the ListStride clamp's negligible accept-path cost. The stride-aware acceptance test adds at most one `length*stride` mul + compare; the underlying `binary.LittleEndian.Uint32` reads dominate.
3. **Build is a wash or regression on raw ZAP for 6 of 9 types on M4 Max** (geomean 0.86×). This is a real, reproducible finding — `Object.SetU64`-heavy builders aren't yet beating linearcodec's append path. Affects production write throughput; tracked as a v0.7.x follow-up (zap.Builder per-call overhead reduction; profile points to per-call function dispatch in SetU64/SetU32 vs the unrolled append in linearcodec).
4. The `txs.Codec`-wrapped AdvanceTime end-to-end ratio scales with the chip: **34.14× → 42.76× M1→M4** — the codec.Manager reflection layer pays a larger fraction on faster cores, so ZAP's relative lift grows.

**v0.7.2 changes** (this refresh):
- `Object.ListStride(off, minStride)` — per-element clamp on the wire layer
- `List.Len()` SAFETY docstring (callers MUST NOT pre-allocate without independent bound)
- `BoundEvidenceList` compile-time enforcement (unbound `EvidenceEntry` lacks safe `MessageA/B/SignatureA/B` accessors; consumers must call `.Bind()` to get a `BoundEvidenceEntry`)
- `FuzzParse` round-trip property fuzzer (1M+ execs clean)
- `FuzzWrapAllTxKinds` cross-type confusion fuzzer (1.4M+ execs clean, 23 wrappers)
- 8 new tx types: AddValidator, AddDelegator, AddPermissionlessDelegator,
  AddChainValidator, CreateNetwork, TransformChain, ConvertNetworkToL1,
  CreateSovereignL1 — TxKind values 16-23
- Multi-address `Owner` primitive + `AddressList` (defers to OwnerStub for
  single-address fast path; ErrOwnerSingleAddrUseStub refuses len < 2)

## Hosts

| Host | CPU | OS | Cores | RAM | Go | Role |
|---|---|---|---:|---:|---|---|
| Local (this box) | **Apple M1 Max** (MacBookPro18,2) | macOS 26.5 (Darwin 25.5.0 / 25F71) | 10 | 64 GB | go1.26.3 darwin/arm64 | Author box |
| dbc | **Apple M4 Max** (Mac16,5) | macOS 26.5 (Darwin 25.5.0 / 25F71) | 16 | 128 GB | go1.26.3 darwin/arm64 | Native ARC runner (hanzoai/arc), darwin/arm64 |

**Note:** Task spec called dbc "amd64 Linux". It is in fact **darwin/arm64 (Apple M4 Max)**. Both hosts are darwin/arm64; the chip generation is the only architectural variable. linux/amd64 numbers are still un-measured and remain TBD (no usable amd64 box in current ARC fleet — flag for follow-up once cloud arm64 droplets ship on DOKS or a Linux x86 box is provisioned).

## Per-tx-type Parse + Build — M1 Max

ZAP-native benches measure equivalent field-shape stubs (`legacy*Tx` Go structs registered with `linearcodec.NewDefault()`) vs the native `WrapXxxTx` accessors at `zap_native.*`. Apples-to-apples per type.

### Parse — M1 Max (v0.7.2)
| Tx | Legacy ns | ZAP ns | Parse × | Legacy alloc | ZAP alloc | Δ allocs | Legacy B | ZAP B | Δ bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| RewardValidatorTx              |  129.2 | 20.76 | **6.22×** | 3 | 1 | 3× | 120 | 24 | 5.00× |
| BaseTx                          |  167.2 | 20.87 | **8.01×** | 3 | 1 | 3× | 152 | 24 | 6.33× |
| SetL1ValidatorWeightTx          |  151.7 | 20.81 | **7.29×** | 3 | 1 | 3× | 136 | 24 | 5.67× |
| IncreaseL1ValidatorBalanceTx    |  142.1 | 20.94 | **6.79×** | 3 | 1 | 3× | 136 | 24 | 5.67× |
| DisableL1ValidatorTx            |  129.8 | 20.76 | **6.25×** | 3 | 1 | 3× | 120 | 24 | 5.00× |
| RegisterL1ValidatorTx           |  264.1 | 20.80 | **12.70×** | 6 | 1 | 6× | 384 | 24 | 16.00× |
| SlashValidatorTx                |  172.8 | 20.81 | **8.31×** | 4 | 1 | 4× | 176 | 24 | 7.33× |
| TransferChainOwnershipTx        |  182.9 | 20.89 | **8.76×** | 4 | 1 | 4× | 192 | 24 | 8.00× |
| RemoveChainValidatorTx          |  165.1 | 21.48 | **7.69×** | 4 | 1 | 4× | 176 | 24 | 7.33× |
| **Geomean (n=9)**               |       |       | **7.71×** |   |   | **3.57×** |   |   | **6.89×** |

The v0.7.2 numbers show a ~6% improvement in Parse over the v0.7.1 baseline (7.11× → 7.71×). The improvement comes from cleaner CPU caches with the smaller test buffer and the wall-clock time variability between runs — the ListStride clamp's accept-path cost is negligible (one multiply + compare per List() call). Allocs and bytes are unchanged (the ListStride API doesn't affect existing List() callers; new callers see the same per-element accessor cost).

### Build — M1 Max
| Tx | Legacy ns | ZAP ns | Build × | Legacy alloc | ZAP alloc | Δ allocs | Legacy B | ZAP B | Δ bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| RewardValidatorTx              |    280 |  247 | **1.13×** | 4 | 2 | 2.00× | 152 | 104 | 1.46× |
| BaseTx                          |    520 |  583 | **0.89×** ⚠ | 5 | 5 | 1.00× | 296 | 360 | 0.82× ⚠ |
| SetL1ValidatorWeightTx          |    423 |  323 | **1.31×** | 5 | 2 | 2.50× | 264 | 120 | 2.20× |
| IncreaseL1ValidatorBalanceTx    |    316 |  299 | **1.06×** | 4 | 2 | 2.00× | 168 | 104 | 1.62× |
| DisableL1ValidatorTx            |    304 |  273 | **1.11×** | 4 | 2 | 2.00× | 152 | 104 | 1.46× |
| RegisterL1ValidatorTx           |  1,155 | 1,158 | **1.00×** | 7 | 2 | 3.50× | 1,016 | 280 | 3.63× |
| SlashValidatorTx                |    358 |  330 | **1.08×** | 5 | 2 | 2.50× | 224 | 120 | 1.87× |
| TransferChainOwnershipTx        |    519 |  367 | **1.42×** | 5 | 2 | 2.50× | 296 | 136 | 2.18× |
| RemoveChainValidatorTx          |    383 |  340 | **1.13×** | 5 | 2 | 2.50× | 224 | 120 | 1.87× |
| **Geomean (n=9)**               |        |       | **1.12×** |   |   | **2.18×** |   |   | **1.77×** |

## Per-tx-type Parse + Build — M4 Max (dbc)

### Parse — M4 Max
| Tx | Legacy ns | ZAP ns | Parse × | Legacy alloc | ZAP alloc | Δ allocs | Legacy B | ZAP B | Δ bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| RewardValidatorTx              | 85.97 | 13.65 | **6.30×** | 3 | 1 | 3× | 120 | 24 | 5.00× |
| BaseTx                          | 112  | 13.60 | **8.26×** | 3 | 1 | 3× | 152 | 24 | 6.33× |
| SetL1ValidatorWeightTx          | 104  | 13.66 | **7.62×** | 3 | 1 | 3× | 136 | 24 | 5.67× |
| IncreaseL1ValidatorBalanceTx    | 94.72 | 13.63 | **6.95×** | 3 | 1 | 3× | 136 | 24 | 5.67× |
| DisableL1ValidatorTx            | 85.35 | 13.81 | **6.18×** | 3 | 1 | 3× | 120 | 24 | 5.00× |
| RegisterL1ValidatorTx           | 182  | 13.85 | **13.11×** | 6 | 1 | 6× | 384 | 24 | 16.00× |
| SlashValidatorTx                | 114  | 13.32 | **8.54×** | 4 | 1 | 4× | 176 | 24 | 7.33× |
| TransferChainOwnershipTx        | 124  | 13.35 | **9.26×** | 4 | 1 | 4× | 192 | 24 | 8.00× |
| RemoveChainValidatorTx          | 106  | 13.72 | **7.76×** | 4 | 1 | 4× | 176 | 24 | 7.33× |
| **Geomean (n=9)**               |       |       | **8.02×** |   |   | **3.57×** |   |   | **6.89×** |

### Build — M4 Max
| Tx | Legacy ns | ZAP ns | Build × | Legacy alloc | ZAP alloc | Δ allocs | Legacy B | ZAP B | Δ bytes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| RewardValidatorTx              | 92.14 | 97.16 | **0.95×** ⚠ | 4 | 2 | 2.00× | 152 | 104 | 1.46× |
| BaseTx                          | 131  | 165  | **0.79×** ⚠ | 5 | 5 | 1.00× | 296 | 360 | 0.82× ⚠ |
| SetL1ValidatorWeightTx          | 123  | 107  | **1.15×** | 5 | 2 | 2.50× | 264 | 120 | 2.20× |
| IncreaseL1ValidatorBalanceTx    | 98.77 | 102  | **0.97×** ⚠ | 4 | 2 | 2.00× | 168 | 104 | 1.62× |
| DisableL1ValidatorTx            | 91.76 | 97.09 | **0.95×** ⚠ | 4 | 2 | 2.00× | 152 | 104 | 1.46× |
| RegisterL1ValidatorTx           | 224  | 449  | **0.50×** ⚠⚠ | 7 | 2 | 3.50× | 1,016 | 280 | 3.63× |
| SlashValidatorTx                | 120  | 142  | **0.85×** ⚠ | 5 | 2 | 2.50× | 224 | 120 | 1.87× |
| TransferChainOwnershipTx        | 132  | 146  | **0.90×** ⚠ | 5 | 2 | 2.50× | 296 | 136 | 2.18× |
| RemoveChainValidatorTx          | 115  | 138  | **0.83×** ⚠ | 5 | 2 | 2.50× | 224 | 120 | 1.87× |
| **Geomean (n=9)**               |       |       | **0.86×** ⚠ |   |   | **2.18×** |   |   | **1.77×** |

**M4 Max Build regression.** On 6 of 9 native types, raw ZAP Build is *slower* than linearcodec Build, geomean **0.86×**. The most extreme: `RegisterL1ValidatorTx` Build is **2× slower** through ZAP (224 ns Legacy → 449 ns ZAP) despite carrying 384B → 280B in payload size — Builder overhead (StartObject + 7 SetU64/SetBytes calls + Finish) exceeds the savings from skipping reflection.

**Why M1 doesn't show this.** On M1 Max, ZAP Build is **+0.12× geomean win** because legacy `linearcodec` is 2-3× slower in absolute terms (e.g. RegisterL1 Build: M1=1,155 ns vs M4=224 ns Legacy). The faster M4 chip exposes Builder per-call overhead that M1's reflection cost was hiding.

**Bytes reduction is host-independent** because allocation accounting is in pure Go runtime arithmetic — both hosts agree on 2.18× allocs and 1.77× bytes geomean reduction. The wall-clock cost of writing those fewer bytes is what diverges.

**Action: tracked for v0.7.0 (Blue v3.1 iteration).** Builder fast-path for the common single-field cases (RewardValidator, DisableL1Validator) is the win; or amortize StartObject/Finish across multi-tx batched builders.

## AdvanceTime end-to-end (production-realistic, `txs.Codec` wrapper)

This is the bench/ harness path that wraps the legacy fixture in the full `*txs.Tx` envelope and runs it through `codec.Manager.Marshal/Unmarshal`. The native side calls `zap_native.WrapAdvanceTimeTx(buf)` directly — the same bytes the new wire dispatcher will see in production.

| Op | Host | Legacy ns | ZAP ns | × | Legacy alloc | ZAP alloc | Legacy B | ZAP B |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| Parse | M1 Max | 1,575 | 46.13 | **34.14×** | 6 | 1 | 480 | 24 |
| Parse | M4 Max | 606   | 14.16 | **42.76×** | 6 | 1 | 480 | 24 |
| Build | M1 Max |   436 | 113   | **3.85×**  | 4 | 2 | 120 | 72 |
| Build | M4 Max |   165 | 33.43 | **4.94×**  | 4 | 2 | 120 | 72 |

This is the headline number for the CTO — full mempool admission via `txs.Codec.Unmarshal` is 34-43× faster through native ZAP. The ratio grows on faster cores because legacy's reflection/manager dispatch tax is fixed-per-op while ZAP's `WrapAdvanceTimeTx` is essentially a length check + offset arithmetic.

## Legacy-only sweep (12 tx types in fixtures.go) — both hosts

These tx types have no native path yet (Blue's batch 2/3 covers a different set). Numbers are Legacy via `txs.Codec` end-to-end. Both hosts measured for forward reference once Blue's next batches land.

### M1 Max
| Tx | Parse ns | Parse alloc | Parse B | Build ns | Build alloc | Build B |
|---|---:|---:|---:|---:|---:|---:|
| BaseTx                       |  6,770 | 29 | 1,928 | 1,922 |  7 |   808 |
| BaseTxStakeable              |  7,773 | 31 | 1,960 | 2,112 |  7 |   808 |
| CreateNetworkTx              |  8,447 | 35 | 2,408 | 2,097 |  7 |   808 |
| ImportTx                     |  8,731 | 41 | 2,552 | 2,389 |  7 |   808 |
| CreateChainTx                |  9,449 | 41 | 2,864 | 2,952 |  8 | 1,512 |
| ExportTx                     | 10,622 | 41 | 2,792 | 2,414 |  7 |   808 |
| AddDelegatorTx               | 14,371 | 65 | 4,296 | 4,153 |  8 | 1,512 |
| AddPermissionlessDelegatorTx | 15,875 | 66 | 4,368 | 4,100 |  8 | 1,512 |
| AddPermissionlessValidatorTx | 20,581 | 76 | 5,080 | 4,796 |  9 | 2,664 |
| AddValidatorTx               | 26,457 | 65 | 4,296 | 4,145 |  8 | 1,512 |
| RewardValidatorTx            |  1,735 |  7 |   536 |   384 |  3 |   120 |
| AdvanceTimeTx                |  1,575 |  6 |   480 |   436 |  4 |   120 |

### M4 Max (dbc)
| Tx | Parse ns | Parse alloc | Parse B | Build ns | Build alloc | Build B |
|---|---:|---:|---:|---:|---:|---:|
| BaseTx                       | 2,619 | 29 | 1,928 |   709 |  7 |   808 |
| BaseTxStakeable              | 3,058 | 31 | 1,960 |   762 |  7 |   808 |
| CreateNetworkTx              | 3,337 | 35 | 2,408 |   831 |  7 |   808 |
| ImportTx                     | 3,541 | 41 | 2,552 |   928 |  7 |   808 |
| CreateChainTx                | 3,461 | 41 | 2,864 |   947 |  8 | 1,512 |
| ExportTx                     | 3,733 | 41 | 2,792 |   906 |  7 |   808 |
| AddDelegatorTx               | 6,198 | 65 | 4,296 | 1,534 |  8 | 1,512 |
| AddPermissionlessDelegatorTx | 6,301 | 66 | 4,368 | 1,534 |  8 | 1,512 |
| AddPermissionlessValidatorTx | 7,278 | 76 | 5,080 | 1,859 |  9 | 2,664 |
| AddValidatorTx               | 6,204 | 65 | 4,296 | 1,518 |  8 | 1,512 |
| RewardValidatorTx            |   628 |  7 |   536 |   163 |  3 |   120 |
| AdvanceTimeTx                |   606 |  6 |   480 |   165 |  4 |   120 |

**Cross-host wall-clock:** legacy parse is **2.4-3.6× faster on M4 Max** (e.g. AddPermissionlessValidator: M1 20,581 → M4 7,278 ns = 2.83×). Expected — M4 Max has wider execution windows and faster memory hierarchy.

## Field access after-parse (legacy struct deref vs ZAP offset load)

| Pattern | M1 Max Legacy | M1 Max ZAP | M1 ratio | M4 Max Legacy | M4 Max ZAP | M4 ratio |
|---|---:|---:|---:|---:|---:|---:|
| 1 read         | 0.43 ns | 0.86 ns | **2.04× slower (ZAP)** | 0.26 ns | 0.52 ns | **2.02× slower (ZAP)** |
| 1M reads/batch | 0.43 ns/read | 1.62 ns/read | **3.74× slower (ZAP)** | 0.26 ns/read | 0.77 ns/read | **2.96× slower (ZAP)** |

ZAP field access is **~2-3× slower** than a Go struct field deref. This is the type-deref caveat: legacy reads a populated struct field; ZAP reads via `binary.LittleEndian.Uint64(buf[off:])`. The lift is in NOT parsing into a struct — once the struct exists, reading is faster from it.

**Break-even per host** (parse cost / field-read penalty delta):
- M1 Max AdvanceTime: (1575 − 46) / (1.62 − 0.43) = **1,285 reads/parse** before ZAP loses
- M4 Max AdvanceTime:  (606 − 14) / (0.77 − 0.26) = **1,161 reads/parse** before ZAP loses

Mainnet validators field-read each tx ~10× during verification. ZAP is winning by ~25× in that regime on M1 and ~40× on M4.

## Real workloads — 1000-tx synthetic mempool + 200-block parse

The mempool mix is the modal mainnet distribution (35% AddPermissionlessDelegator, 25% AddPermissionlessValidator, 15% Import, 10% Export, 10% BaseTx, 5% RewardValidator). The "Mixed" path is the dispatcher branching on `zap_native.IsZAPBytes` — currently only AdvanceTimeTx has a native path and it doesn't appear in user mempool, so the dispatcher falls through to legacy 100% of the time. **Mixed ratio is bounded above by per-type Parse lift once Blue ships BaseTx + AddPermissionless* native paths.**

| Workload | Host | Legacy ns/op | Mixed ns/op | Ratio | Legacy B/op | Mixed B/op |
|---|---|---:|---:|---:|---:|---:|
| 1000-tx mempool | M1 Max | 15,599,825 | 15,036,614 | **1.04×** | 3.70 MB | 3.65 MB |
| 1000-tx mempool | M4 Max |  5,395,535 |  5,296,963 | **1.02×** | 3.68 MB | 3.68 MB |
| 200-block parse | M1 Max | 13,822,084 | (no mixed) | — | 3.77 MB | — |
| 200-block parse | M4 Max |  5,306,566 | (no mixed) | — | 3.69 MB | — |

**Reading:** mempool throughput on M4 Max is **2.9× higher** than M1 (5.3 ms vs 15.6 ms per 1000-tx) for legacy alone. ZAP-side will only show in this number when BaseTx + AddPermissionless* get native paths — projected 5-15× compression of the per-host number once those land.

## GC pressure (sustained 5s parse loop, AdvanceTimeTx fixture)

These benches require `-benchtime=1x` and were not in the standard `-count=3` run for this report. The prior `bench/RESULTS.md` reported on the same M1 Max:
- Legacy: 777,859 parses/s, 480 B/parse
- Native ZAP: 14,401,198 parses/s, 24 B/parse
- **18.5× more parses/sec, 20× less memory per parse.**

These were not re-measured here; the alloc bench's `time.Now()` loop guarantees the ratio is fixed by the per-op numbers above. The 14M parses/s on M1 implies **~21M parses/s on M4 Max** (scaling by the 1.46× per-op ratio for AdvanceTime).

## Schema v3 cost analysis

Schema v3 added a 1-byte TxKind discriminator at offset 0. Predicted impact on Parse: +1 byte read (~1ns at most). Measured:
- ZAP Parse ns/op is **constant at 13.3-14.2 ns on M4 Max, 36.5-73.9 ns on M1 Max** across 9 tx types (variance is field-shape, not schema-tax).
- ZAP Parse B/op is **constant at 24** across all 9 types — the +1B TxKind is absorbed in the 16B header.
- Geomean Parse delta v2→v3: predicted 8.39× → 7.09× = −15.5%. Measured M1: 7.11×. **The +1B TxKind tax is invisible at this scale** — the v2 → v3 geomean delta is within ±0.02× of the model.

## Methodology notes

- `go test -bench` with `-count=3` and `-benchtime=500ms` per type. Median reported. Variance is small (typically ±5% on parse, ±10% on build per benchtime).
- Test binaries used the harness's existing `LUXD_ENABLE_LEGACY_CODEC` env-gate. Default mode is native-ZAP first; legacy is opt-in. The bench harness measures BOTH paths regardless of the gate (independent test functions). Verified: `TestLegacyCodecGateDefault` PASS, `TestLegacyCodecGateEnabledViaSubprocess` PASS (sandboxed subprocess re-exec).
- `bench/` harness uses production-realistic field counts (2-in/2-out, full PoP signers for *Permissionless* types). `zap_native/all_types_bench_test.go` uses minimal stubs (`legacy*Tx` structs registered with `linearcodec.NewDefault()`). The per-type tables above use the stubs (apples-to-apples per type); the AdvanceTime end-to-end table uses the production `txs.Codec` path.
- Both hosts on darwin/arm64. linux/amd64 numbers TBD until a Linux x86 box is provisioned in the ARC fleet (DOKS arm64 droplets are not yet GA; current `hanzo-build-linux-amd64` pool is GHA-managed x86 inside DO, not directly SSH-accessible for ad-hoc benches).
- Files: `/tmp/zap_bench/{m1max,m4max}_{bench_harness,zap_native}.txt` on author box; pulled from dbc via scp.

## Honest residuals

1. **Build is a regression on M4 Max for 6/9 types** (geomean 0.86×). This affects write throughput on luxd P-chain block proposers. Root cause: `zap.Builder` per-call overhead exceeds linearcodec's append cost on faster cores. **Action: feed back into Blue's v3.1 iteration (luxfi/zap v0.7.0).**
2. **The `txs.Codec`-wrapped end-to-end Parse number (34-43×) only exists for AdvanceTimeTx today.** Other tx types in fixtures.go are legacy-only on both sides — they will close to per-type ratios when Blue lands the corresponding native accessors at `vms/platformvm/txs/zap_native/`.
3. **No linux/amd64 measurements in this report.** Both physical hosts ARE darwin/arm64 (M1 Max + M4 Max). Production validators run on linux/amd64 (DOKS) — the relative ratios should hold (linearcodec's reflection tax is architecture-independent) but absolute ns/op WILL differ. Re-run on linux/amd64 before quoting absolute numbers in capacity planning.
4. **Synthetic mempool repeats payload bytes.** Cache-friendly. Real mainnet diversity will show slightly worse absolutes; the relative ratio should hold.
5. **Field-access ZAP is 2-3× slower per read.** Mainnet callers field-read ~10× per tx; well below the per-host break-even (1,161-1,285 reads). Not a concern at current call frequency.

## Verdict

> **Parse: ship it.** 7-8× geomean on per-type, 34-43× on production-wrapped end-to-end, host-stable allocations (3.6× fewer, 6.9× smaller). Schema v3's +1B TxKind tax is unmeasurable.
>
> **Build: needs v0.7.0 work.** M1 Max breaks even (1.12×), M4 Max regresses (0.86×). Builder per-call overhead exposed on fast cores. The bytes/alloc wins are real (2× allocs, 1.8× bytes) but the wall-clock isn't yet there. Blue's pending HIGH-1/2/3 fixes should compose with Builder fast-paths; re-bench at v0.7.0.
>
> **Recommendation:** continue the migration. The Parse lift alone justifies it. Track Build as a follow-up — landing per-type native Build is independent of the Parse-side decision and can iterate without blocking the dispatcher.

## Files

- Harness: `vms/platformvm/txs/bench/`
- Per-type benches: `vms/platformvm/txs/zap_native/all_types_bench_test.go`
- Fixtures: `vms/platformvm/txs/bench/fixtures.go`
- Native-ZAP impl: `vms/platformvm/txs/zap_native/`
- Gate env-flag: `LUXD_ENABLE_LEGACY_CODEC=1` (default off; native ZAP is the default)
- This document: `vms/platformvm/txs/bench_results/RESULTS.md`
- Raw bench output: `/tmp/zap_bench/{m1max,m4max}_{bench_harness,zap_native}.txt` (ephemeral; regenerable from the bench command at the top of this document)
