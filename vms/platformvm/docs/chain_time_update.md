# Chain time update mechanism

The P-Chain tracks `ChainTime` as the network-agreed timestamp used to
score staker activity for rewards. This document describes how blocks
advance `ChainTime` on Lux. The legacy pre-upgrade gate is always active
on Lux; only the modern model is documented here.

## About `ChainTime`

The P-Chain records staking periods for every staker (validator or
delegator) on every net so that rewards can be computed at end-of-staking.

`ChainTime` is the network-agreed timestamp used to decide when a staker
started and stopped staking. It is the basic input to the per-staker
uptime fraction that decides whether a reward is paid.

Note: `ChainTime` is unrelated to the `Linear++` timestamp. `Linear++`
timestamps are local times used to reduce network congestion and play no
role in staker rewards.

## How blocks advance `ChainTime`

`AdvanceTimeTx` transactions are not used. Every P-Chain block explicitly
serialises a timestamp; on acceptance, `ChainTime` is set to that
timestamp.

Validation rules vary slightly by block type:

- `CommitBlock` and `AbortBlock` timestamps must equal the timestamp of
  the `ProposalBlock` they depend upon.
- `StandardBlock` and `ProposalBlock` timestamps obey:
  1. **Monotonicity** — block timestamp must be greater than or equal to
     the current `ChainTime` (which is also the parent's timestamp if the
     parent was accepted).
  2. **Synchronicity** — block timestamp must not exceed the node's local
     clock plus a 10-second synchrony bound.
  3. **No skipping** — block timestamp must not exceed the next staker
     start- or stop-event time.
