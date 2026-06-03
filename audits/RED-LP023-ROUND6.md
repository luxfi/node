# RED LP-023 ROUND 6 — Adversarial Review of v3.7 (67cff34428)

Scope: diff 3f8f7e79ec..67cff34428 across `vms/platformvm/txs/zap_native/`,
`vms/platformvm/network/zap_native_admission.go`, `.github/workflows/zap-audit.yml`.

Verdict: **DO-NOT-SHIP / HOLD**.

---

## V1 — CRITICAL — GOVERNANCE — TransferChainOwnershipTx.Verify regressed to no-op (R6V8 undone)

**Class**: ADMISSION / GOVERNANCE. **Severity**: CRITICAL.

`vms/platformvm/txs/zap_native/tx_verify.go:330-336` ships a Verify body that
does NOT call `SyntacticVerify`. Body:

```go
// Verify — ATTACK PROBE V3: gate-bypass via string-literal SyntacticVerify token.
// Removed the actual call. Audit gate should fail; if it passes, V3 is confirmed.
func (t TransferChainOwnershipTx) Verify() error {
    _ = "SyntacticVerify"
    return nil
}
```

This is the exact regression Blue's R6V8 closed at commit 30c9608255 and that the
new audit gate was supposed to catch.

**Repro** (already failing in 67cff34428 tree, no PoC needed):

```
cd vms/platformvm/txs/zap_native && go test ./...
--- FAIL: TestTransferChainOwnershipTx_Verify_RejectsZeroThreshold
--- FAIL: TestTransferChainOwnershipTx_Verify_RejectsThresholdAboveOne
--- FAIL: TestTransferChainOwnershipTx_Verify_AdversarialWireBuffer
```

**Impact**: An attacker submits a `TransferChainOwnershipTx` with
`OwnerThreshold == 0` (no signer required) or `OwnerThreshold > addrcount`
(unsatisfiable — chain ownership permanently DoS'd). Either path takes over
chain ownership without authorization or destroys it. Wire-canonically valid;
admission boundary admits it; standard_tx_executor commits it.

**Detectability**: Zero. Package tests already fail at HEAD; nobody ran them
before tagging v3.7. The audit gate (V2 below) PASSES because the token
"SyntacticVerify" appears in the function body as a string literal.

**Fix**: Restore the gate body that existed at 30c9608255:

```go
func (t TransferChainOwnershipTx) Verify() error {
    o := stubFromTuple(t.OwnerThreshold(), t.OwnerLocktime(), t.OwnerAddress())
    if err := o.SyntacticVerify(); err != nil {
        return fmt.Errorf("TransferChainOwnershipTx.Owner: %w", err)
    }
    return nil
}
```

Delete the "ATTACK PROBE V3" doc-comment; intentional gate-bypass code never
lives in the production tree.

---

## V2 — CRITICAL — ADMISSION — Audit gate fooled by string-literal token (meta-attack on R4V7 audit)

**Class**: ADMISSION. **Severity**: CRITICAL (proof: V1 above ships and passes).

`audit_test.go:359` and `.github/workflows/zap-audit.yml`
owner-bearing-syntacticverify-gate use `strings.Contains(body, "SyntacticVerify")`
on the brace-bounded Verify body. The check is purely textual; it does not
distinguish:

- A real `stub.SyntacticVerify()` call site
- A `_ = "SyntacticVerify"` no-op string literal
- A `// SyntacticVerify` source comment
- A constant-false branch: `if false { x.SyntacticVerify() }`
- A name-conflict shadow: `var SyntacticVerify int`

Blue's own self-audit vector #5 ("audit gate meta-attack: construct a Verify
body that satisfies textual SyntacticVerify match but doesn't call at
runtime") materialized in V1. The audit gate cannot be the only line of
defense for `R4V7`.

The same defect applies to the `ChainsListEmbeddersCallMustVerify` and
`ValidatorsListEmbeddersCallMustVerify` gates — they check for `.MustVerify(`
the same way, and any string literal containing `.MustVerify(` defeats them.

**Impact**: Any future Verify body that drops the call but keeps the token
text passes CI. Same primitive enables silent removal of MustVerify on
ChainsList and ValidatorsList.

**Fix** (the only correct fix): switch from textual to AST-level. Either:
1. `go/parser` + `go/ast` walk inside the audit test — locate the
   `*ast.FuncDecl` for each receiver type, then scan for an
   `*ast.CallExpr` with `Sel.Name == "SyntacticVerify"` or `"MustVerify"`.
   This rejects string literals, comments, and dead-code branches by
   construction.
2. Replace the audit gate entirely with positive runtime tests: every
   Owner-bearing tx type MUST have a `TestT_Verify_RejectsZeroThreshold`
   row in `r6_verify_test.go` (one and only one way to assert the gate
   ran). Regression caught by `go test ./...`, not a custom grep.

Bonus: Move BOTH audit gates (Owner-bearing + MustVerify) to AST-based at
the same time so the meta-attack class is closed.

---

## V3 — HIGH — ADMISSION — Audit gate scope leak: legacy txs `*Owner()` consumers bypass

**Class**: ADMISSION / GOVERNANCE. **Severity**: HIGH (acknowledged scope, but
the scope description in the brief is wrong).

The audit gate scopes the grep to the `zap_native` package. The legacy AVAX
tx interface (`vms/platformvm/txs/*.go`) defines `RewardsOwner()`,
`ValidationRewardsOwner()`, `DelegationRewardsOwner()` on `AddValidatorTx`,
`AddDelegatorTx`, `AddPermissionlessValidatorTx`,
`AddPermissionlessDelegatorTx`. These accessors return `fx.Owner` (not the
`(uint32, uint64, ids.ShortID)` tuple), so the audit gate's regex skips
them by design.

**But the executor consumes them without `Verify()`**:

- `vms/platformvm/txs/executor/proposal_tx_executor.go:303` —
  `uValidatorTx.ValidationRewardsOwner()`
- `vms/platformvm/txs/executor/proposal_tx_executor.go:340` —
  `uValidatorTx.DelegationRewardsOwner()`
- `vms/platformvm/txs/executor/proposal_tx_executor.go:429` —
  `uDelegatorTx.RewardsOwner()`
- `vms/platformvm/service.go:748-755` — same set, RPC path.

These run on the staking reward path. A legacy tx with a malformed Owner
(`Threshold > Addrs.Len()`) currently relies on the AVAX legacy parser to
reject. The audit gate would not detect a new code path that consumes
`*RewardsOwner()` without verification.

**Impact**: A future tx type or executor body that consumes Owner data via
the `fx.Owner` interface bypasses the gate silently. Quorum-skew / authz
bypass primitive on the staking reward path. Detectability: zero CI signal.

**Fix**: Either (a) broaden the audit gate to include `fx.Owner`-typed
returns (the AST refactor in V2 naturally subsumes this), or (b) document
the legacy boundary explicitly in `tx_verify.go` and add a positive test
matrix asserting every legacy `*Owner()` access site has a corresponding
SyntacticVerify upstream.

---

## V4 — HIGH — CRYPTO/DOS — BLS pubkey allocation amplification (Blue's self-audit V1, NOT fixed)

**Class**: DOS. **Severity**: HIGH.

`validators_list.go:101-131` — `ValidatorRecord.BLSPubKey()` and
`BLSPoP()` allocate a fresh `[]byte` per call. `MustVerify` walks N
records, calling each accessor once (line 224, 229 — used inside
`bytes.Equal`). At N=1024 cap that's:

- 1024 × 48B pubkey copies = 48 KB
- 1024 × 96B PoP copies   = 96 KB
- Total: 144 KB per `MustVerify` call

But `tx_verify.go:285-311` calls `vals.At(i).BLSPubKey()` AND `BLSPoP()`
AGAIN per validator (for the BLS pairing). Cumulative: ~288 KB allocs
per `CreateSovereignL1Tx.Verify`. Mempool-DoS primitive: a peer that
gossips well-formed-but-cap-bound L1 spawn txs (or replays existing
ones) forces 288 KB GC churn per gossip event. At 1k txs/sec this is
288 MB/sec of allocation in admission.

Blue's self-audit V1 listed this and shipped no mitigation.

**Fix**: either (a) `MustVerify` reads bytes directly via the underlying
`zap.Object.Uint8` loop without copy-out, or (b) cache the
`[BLSPubKeySize]byte` and `[BLSPoPSize]byte` arrays on `ValidatorRecord`
under a lazy-init guard. (a) is simpler and orthogonal — bytes are
read-only at verify time.

---

## V5 — MEDIUM — DOS — No per-block tx-count cap (Blue self-audit V2, NOT fixed)

**Class**: DOS. **Severity**: MEDIUM.

`MaxChainsPerL1=16` and `MaxValidatorsPerL1=1024` are per-tx caps.
There is no block-level aggregate cap. An attacker submits N copies of
a 1024-validator `CreateSovereignL1Tx`; each tx independently passes
MustVerify, the block packer accepts up to mempool capacity, and the
verifier walks 1024N BLS pairings on block accept. At a 2-second block
time and tx min-fee, the amplification is bounded only by mempool RAM.

Blue's self-audit V2 listed this and shipped no mitigation.

**Fix**: Add a block-level cap in the standard_tx_executor commit path:
`sum(vals.Len()) over Verify calls in block <= GlobalMaxValidatorsPerBlock`
(e.g. 4096). Out-of-budget txs are deferred to next block, not dropped —
the encoder paid the fee.

---

## V6 — MEDIUM — ADMISSION — Admission gate parse-and-rewrap inefficiency + ordering bug

**Class**: ADMISSION. **Severity**: MEDIUM.

`zap_native_admission.go:113-131` (`zapNativeWireKindNotYetExecutable`)
calls `WrapCreateSovereignL1Tx`, `WrapRegisterL1ValidatorTx`, `WrapConvertNetworkToL1Tx`
in sequence. Each wrap reparses the header to check the kind discriminator.
The brief comments "cheap" but the parse goes through `parseAndCheckKind`,
which is the same body that ran on the inbound RPC — a measurable hot-path
re-walk.

Worse, **order matters**: kind 7 (`RegisterL1Validator`) is in the gated
set, but `WrapCreateSovereignL1Tx` is called FIRST. A wire buffer with
discriminator kind 7 will fail `WrapCreateSovereignL1Tx` with
`ErrWrongTxKind`, then succeed `WrapRegisterL1ValidatorTx`. Fine for the
returned `kind`, but the early-fail path consumes parse work unnecessarily.

Cheap fix: read the kind discriminator once via `parseAndCheckKind` directly,
then a single `switch`. Saves 2/3 of the work on the common-case path.

**Detectability**: Not a security bug per se; it's a microoptimization
miss that compounds with V4/V5 amplification.

---

## V7 — LOW — GOVERNANCE — `ATTACK PROBE V3` comment ships in production source

**Class**: GOVERNANCE. **Severity**: LOW (process bug; the underlying
vulnerability is V1).

`tx_verify.go:330-331` documents the attack vector inline, in production
Go source, with `ATTACK PROBE V3` as the function-level comment. Even
after V1 is fixed, the comment ships as a forensic breadcrumb advertising
the audit-gate weakness to any reader with `git log`.

**Fix**: When restoring the gate body (V1), strip the entire comment.
Production source is not a scratchpad for red-team probes.

---

## V8 — INFO — GOVERNANCE — `ListStride` overflow surface (Blue's free-form probe answer)

**Class**: CRYPTO/DOS. **Severity**: INFO.

Blue asked: "Stride*N uint32 multiply overflow in BoundChainsList /
BoundValidatorsList: can you construct N such that stride*N overflows
uint32 wraparound to a small value?"

Answer: NO. `zap/zap.go:393-427` `ListStride` performs `uint64(length) *
uint64(minStride) > bufRem` — both operands are uint32, product is uint64,
no wraparound. The clamp is correct. This vector is closed.

Documenting here so future audits don't re-probe.

---

## Blue Handoff

What Blue got right:
- `MaxValidatorsPerL1 = 1024` and `MaxChainsPerL1 = 16` caps are well-chosen
  and wired BEFORE BLS pairing. Cheap-gate-first ordering is correct.
- `ValidatorsList.MustVerify` 5-floor invariants (cap, weight, BLS-non-zero,
  expiry-non-zero) are the right set and tests pass per-invariant.
- `ListStride` uint64 product avoids overflow — V8 closed.
- The `MustVerify` receiver-name choice (R7V8) genuinely does make
  "I forgot to call this" grep-able — at the call site. The PROBLEM is the
  call-site grep, not the receiver-name choice.
- Admission gate (`zap_native_admission.go`) correctly orders Path 1 / Path 2
  with the wrapping verifier as inner — defense-in-depth on the legacy
  AND wire-bytes paths.

What Blue missed:
- The audit gate (the new infrastructure shipped in this batch) is fooled
  by string literals (V2). This is the single most important finding.
  The gate Blue added to prevent regressions of R4V7 cannot detect the
  very regression that ships in this same commit (V1).
- TransferChainOwnershipTx.Verify (V1) — Blue's own R6V8 gate is undone
  and the package test suite already fails. Blue did not run
  `go test ./vms/platformvm/txs/zap_native/...` before tagging v3.7.
- Legacy `*Owner()` consumers in `proposal_tx_executor.go` and `service.go`
  (V3) are out-of-scope for the audit gate but are real Owner consumers.
- BLS pubkey/PoP allocation amplification (V4) — Blue self-audited and
  shipped no fix.
- Per-block cap absence (V5) — Blue self-audited and shipped no fix.

Fix priority for Blue:
1. **V1**: Restore `TransferChainOwnershipTx.Verify` body, strip the
   ATTACK PROBE comment. **Run `go test ./vms/platformvm/txs/zap_native/`
   to green BEFORE tagging anything.**
2. **V2**: Replace textual audit gate with AST-based, OR replace with
   positive-test matrix in `r6_verify_test.go`. One and only one way.
3. **V3**: Broaden audit scope to `fx.Owner`-returning accessors, OR
   document the legacy boundary explicitly with a positive test matrix.
4. **V4**: `ValidatorRecord.BLSPubKey/BLSPoP` no-copy path for MustVerify
   (or cache on record). 144 KB → near-zero.
5. **V5**: Block-level validator/chain budget in standard_tx_executor.
6. **V7**: Delete the ATTACK PROBE comment from production source.
7. **V6**: Single-pass kind discriminator read in admission gate.

Re-review scope: V1, V2, V3 mandatory before re-review. V4, V5, V6, V7
can land in a follow-up batch but Blue should report the plan.

---

RED COMPLETE. Findings ready for Blue.
Total: 2 critical, 2 high, 2 medium, 1 low, 1 info.
Top 3 for Blue to fix:
1. V1 — TransferChainOwnershipTx.Verify regression (R6V8 undone)
2. V2 — Audit gate fooled by string-literal token (meta-attack proven)
3. V3 — Legacy `*Owner()` consumers bypass audit gate scope
Re-review needed: yes — V1, V2, V3 must close before re-review.
Recommendation: **do-not-ship**.
