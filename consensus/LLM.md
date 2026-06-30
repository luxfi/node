# Consensus Package - Session Notes

## Terminology Update (2026-01-04)

Updated documentation to use "Vote" terminology consistently:

- **Vote**: Semantic name for validator responses to block proposals
- **Chits**: Wire protocol format (preserved for backwards compatibility)

Note: "Vote (wire format: Chits)" should be used where clarification is needed.

## Package Contents

### acceptor.go

Node-side `Acceptor` interface for block acceptance callbacks:
- `Accept()` called before container committed as accepted
- `AcceptorGroup` manages chain-ID-keyed acceptors (used by indexer + warp)
- Thread-safe with RWMutex

The canonical consensus `Acceptor` lives in `luxfi/consensus/core`. This
package's variant differs in that it takes a `*runtime.Runtime` (node-side
runtime context) rather than a `context.Context`, and supports multi-chain
registration via `AcceptorGroup`.

### quasar/

Node-side wiring around `github.com/luxfi/consensus/protocol/quasar`:
- `Quasar` - Wraps the canonical engine with P-Chain provider + finality channel
- `CoronaCoordinator` - Stub for threshold signing (real keys loaded later)
- `CoronaSignature`, `BLSSignature`, `QuasarSignature` - Node-side signature wrappers

Imports `github.com/luxfi/consensus/protocol/quasar` for the actual protocol.

## Architecture Notes

### Dual-Path Finality

```
Block arrives
    |
    +-- BLS PATH (fast) --------+-- CORONA PATH (quantum-safe) --+
    |   All validators sign     |   Round 1: commitments           |
    |   with BLS keys           |   Round 2: partial signatures    |
    |   Aggregate (96 bytes)    |   Combine threshold signature    |
    |                           |                                  |
    +---------------------------+----------------------------------+
                                |
                         HYBRID PROOF
                    (BLS + Corona combined)
                                |
                        QUANTUM FINALITY
```

### Vote Flow

1. Block proposed via gossip
2. Validators vote (wire: Chits message)
3. Votes collected and aggregated
4. Quorum check (2/3+ weight)
5. Finality achieved when both BLS and Corona complete

## Test Coverage

- `quasar/config_test.go` - Configuration tests
- `quasar/integration_test.go` - Integration tests

## Recent Changes

- 2026-01-04: Created documentation files with Vote terminology

## consensus/quasar — PQ-finality VERIFY gate (2026-06-28)

Supersedes the stale "Quasar wrapper / CoronaCoordinator" notes above (that
subpackage did not exist in-tree). The current `consensus/quasar` package is the
node-side integration of `luxfi/consensus@v1.29.0`'s typed compact-cert finality
layer (`protocol/quasar.VerifyConsensusCert`). It wires the VERIFY half only.

Model: luxd finalizes on classical Snow every block; at CHECKPOINTS (height %
interval) a sampled committee's QuasarCert over the finalized digest is VERIFIED.
Default posture HYBRID_PQ = Beam(BLS) ∧ Pulsar (ML-DSA-65); STRICT_DUAL_PQ
(+Corona) / POLARIS (+Magnetar) configurable.

THE SAFETY CONTRACT — forward-dated, DORMANT by default:
- `Gate.VerifyAccepted` is the accept-path boundary. nil gate / `Activation.Height
  == 0` / below-height / non-checkpoint => no-op; classical finality UNCHANGED.
- Activated at a checkpoint => REQUIRE a valid cert bound to the finalized block;
  FAIL CLOSED (missing/mismatch/invalid => error from Accept, halts without
  persisting). Activation is HEIGHT-ONLY (deterministic — no wall clock).
- Hooked in `vms/proposervm/post_fork_block.go Accept()` via
  `vm.verifyQuasarFinality(b)`; the VM's `quasarGate` is nil in production today
  (set via `SetQuasarGate`). Nothing wires it yet — that is the activation step.

Files: gate.go (Gate/ActivationConfig/bindCheck), policy.go (HYBRID_PQ default,
cert can't pick its own policy), validators.go (ConsensusValidatorSet: BLS+Pulsar
keys), store.go (MemCertStore), producer.go (committee-signer interface =
scaffolding; nil = verify-only), errors.go. Tests: gate_test.go (13, -race green:
dormant no-op, fail-closed, epoch/round/chain/height/block anti-replay, real-
verifier delegation, misconfigured-fails-closed).

REMAINING WORK to reach a live PQ-finality network (all owner-gated):
1. Producer service (pulsard): the per-validator committee cert signer. Needs
   pulsar v1.7.1 (no-reconstruct hyperball signer) AND consensus to EXPORT the
   currently package-private cert/payload ENCODERS (an external producer cannot
   assemble a ConsensusCert envelope without them; this also unblocks an end-to-
   end positive verify test).
2. Cert gossip/ingest -> MemCertStore (verify-before-store); MemCertStore needs
   eviction below last-finalized height before this lands.
3. Production per-epoch `ValidatorSetProvider` from the P-Chain validator manager
   + KeyEra registry (BLS aggregate + Pulsar/Corona/Magnetar group keys per era).
4. Config-flag -> SetQuasarGate wiring (construct a non-nil gate from node config;
   choose ChainID = sovereign/EVM chain id).
5. Cert-unavailability runbook + a bounded grace window (await cert N rounds)
   before a checkpoint halts — fail-closed-after-decision can brick a chain if
   gossip is down. REQUIRED before any forward-dated activation.
6. A proposervm Accept-path integration test (nil/dormant/activated).

Mainnet activation order (owner): deploy producer -> verify cert-gossip coverage
at checkpoints -> set Activation.Height to a forward-dated height with margin ->
roll via `kubectl patch sts luxd` OnDelete, 1 pod at a time. NEVER wipe /data/db,
NEVER pkill, NEVER blind-restart.
