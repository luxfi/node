# Postmortem: C-Chain accepted-head state GC eviction (mainnet freeze)

**Status:** Resolved. Mainnet recovered and accepted at 5/5 validators, C-Chain
height 1085412, hash `0xd957eae6cb0bbef37174…`, all validators in agreement,
explorer at tip, treasury and Genesis NFT state verified.

**Severity:** Critical (mainnet C-Chain unable to build blocks; no state loss).

## Accepted permanent invariant

> The accepted C-Chain head's state root must never be GC/pruning eligible —
> across idle windows, duplicate empty-block state roots, cold snapshot/cache
> layers, small state history, and restarts.
>
> accepted head ⇒ accepted head state root is pinned ⇒ GC/pruning cannot evict
> the execution base for H+1.

The specific accepted-head GC eviction failure is **structurally prevented** by
the head-state pin, and production evidence confirms the fleet no longer
exhibits the prior failure signature. (A formal long-idle ritual was not
completed to termination during the incident window; the sign-off rests on the
structural invariant plus production evidence: a head idle for 3h04m was built
on cleanly, multiple 5–15 minute zero-traffic windows passed with tip state
readable, and zero eviction/materialize canaries appeared fleet-wide after the
fix.)

## Failure mode (exact)

The C-Chain EVM (coreth-lineage, `luxfi/evm`) in pruning mode manages trie
memory with `cappedMemoryTrieWriter` (`core/state_manager.go`):

- Accepted state roots are held in a `tipBuffer` (`BoundedBuffer`) of depth
  `state-history` (default **32**); as roots age out of the buffer they are
  `Dereference`d.
- Dirty trie nodes are only committed to disk at `commit-interval` boundaries
  (default **4096** blocks), with optimistic `Cap` flushes near the boundary.
- The insert-time `triedb.Reference(root, {})` in `writeBlockAndSetHead` is
  **refcount-balanced**: it is consumed as the block ages through the
  tipBuffer (or via `RejectTrie`). It therefore does not protect an idle head.

On an idle chain, consecutive empty blocks share identical state roots. The
tipBuffer's aging `Dereference` for an old entry then lands on the *live head
root* (duplicate key), dropping its reference count to zero. At any height that
is not a commit boundary the head root has never been persisted, so the next
`Cap`/flush evicts it from the dirty cache. The subsequent `BuildBlock` cannot
open the parent (head) state:

```
failed to materialize parent state for build: … StateAt: missing trie node
<head state root> … is not available, not found
```

and the chain wedges. RPC reads at `latest` fail with the same error (any
`StateAt(root)` caller). Consensus is unaffected — all validators agree on the
head *block*; only the local execution base for H+1 is gone. State is always
deterministically re-derivable from durable blocks, so a restart re-executes
and recovers — **restart is recovery evidence, not a fix**: the head could
still be evicted again in the next idle window.

### Preconditions (all defaults on affected mainnet validators)

- `pruning-enabled: true`
- `state-history: 32`
- `commit-interval: 4096`
- idle or bursty-then-idle traffic (heartbeat pause, low organic flow)
- duplicate empty-block state roots at the tip
- current height not at a commit boundary

Any C-Chain deployment matching these preconditions is exposed on affected
versions — this bug class will recur wherever the EVM runs with pruning, small
state history, long commit intervals, and idle traffic.

### Observed occurrences

1. Mainnet froze at height 1085200 after a ~10 minute heartbeat pause
   (2026-07-07). All five validators had the block, none could serve or build
   on its state.
2. An earlier fleet-wide variant contributed to the 1082879→1085012 incident
   window (mixed with a separate proposervm/consensus issue documented in the
   consensus fault-recovery audit).

## Affected / fixed versions

| Component | Affected | Fixed |
|---|---|---|
| `luxfi/evm` (C-Chain plugin) | ≤ v1.104.6 (all pruning-mode deployments; the balanced insert-time Reference in v1.104.3-hotfix was insufficient) | **v1.104.7** |
| `luxfi/node` image | v1.34.14 – v1.34.23 (carry affected EVM plugins) | **v1.34.24** (interim), **v1.34.25** (canonical: identical fix, proper semver, clean go.mod) |

Fix commits (`luxfi/evm`, branch `evm-main-bugb`): `9bab20f7a` (pin),
`58b90490c` (non-fatal degradation), `e3781c35c` (vm v1.2.6 parity).

## The fix (structural)

`core/blockchain.go`: a dedicated **unbalanced** GC reference held on the
accepted head's state root — `headStatePinRoot` + `pinAcceptedHead(root)`:

- Transferred head-to-head: `Reference` the new head root first, then
  `Dereference` the previous pinned root; exactly one live head pin exists at
  all times, on `lastAccepted.Root()`.
- Established in `Accept`, in `SetLastAcceptedBlockDirect`, and on the loaded
  head at startup (`loadLastState`), so the invariant holds across restarts
  and the restart-then-idle path.
- Skips when the root is unchanged (duplicate empty-block roots keep exactly
  one reference) and when state is not yet materialized (bootstrapping /
  state-sync; the first `Accept` then establishes it).
- Deliberately **not** balanced against `InsertTrie`/`AcceptTrie`/`RejectTrie`
  — its lifetime is "is the accepted head", nothing else.
- Non-fatal on backend error (pathdb `Reference`/`Dereference` are no-ops /
  "not supported"): a failed pin degrades to pre-fix behavior with a WARN
  rather than wedging Accept or startup.

No archive-mode workaround and no state-sync hack. Disk growth is unchanged
(one extra referenced root).

## Recovery recipe (what actually worked)

1. **Wedged-at-tip (state evicted, DB otherwise consistent):** restart the
   node. Boot re-executes from the last committed root and re-materializes the
   head state deterministically. Valid as *recovery*; deploy the fixed version
   so it cannot recur.
2. **proposervm/EVM height split** (`proposervm finality index … is BEHIND the
   inner VM tip`; produced here by crash-churn on affected versions — the
   fail-closed guard then correctly refuses to mount): restarts cannot heal a
   split. Restore the node's PVC from a `VolumeSnapshot` of a currently
   healthy peer. Per-ordinal staking keys are installed by `startup.sh` from
   the `luxd-staking` secret, so cross-node volume clones are safe (distinct
   NodeIDs).
3. **PVC swaps must happen at StatefulSet `replicas=0`.** A live single-pod
   PVC delete/recreate always loses the race to the StatefulSet controller,
   which recreates a blank PVC first.
4. If a fleet-consistent EVM rewind is needed instead (no healthy peer):
   `evm/cmd/repair-cchain` rewinds the standalone EVM `lastAccepted` to the
   proposervm floor; on boot the heightAhead branch self-heals (used in the
   1084996 recovery). Zero re-execution; never a re-genesis.
5. Retain evidence snapshots before every destructive step.

## Residual follow-ups (non-blocking; restart/churn liveness, not consensus or state-loss)

1. **Proposer-preference restart loop:** after heavy sibling churn, a node's
   proposervm preference can reference a never-persisted outer block; every
   `BuildBlock` then fails `not found` in a tight loop and the node's voter
   goes mute (observed ~170 err/s). Restart clears it. Fix: fall back to
   last-accepted when the preferred parent is not fetchable.
2. **Ancestor-fetch liveness:** the finality guard refuses certs with
   "ancestor … is not tracked (behind; fetch and retry)" but the fetch never
   fires, so the node loops instead of catching up. Restart clears it. Fix:
   actually schedule the ancestor fetch on this path.

## Monitoring (keep active)

- Eviction/materialize canaries: `STATE-MATERIALIZE`, `missing trie node`,
  `ACCEPT-BACKSTOP` log lines — expect zero.
- Accepted height/hash equality across all validators.
- Explorer tip parity with chain head.
- `is BEHIND the inner` (heightBehind) occurrences — expect zero.
- Do not treat the heartbeat as a safety mechanism; it is a liveness nicety.
