# Block Composition and Formation Logic

Lux's P-Chain always builds and verifies the latest block format. The
post-upgrade block model is the only model that ever runs on a Lux network:
all legacy upgrade gates resolve to "active" at chain birth, so block
formation has exactly one shape.

## Block Types

- **Standard Blocks** may contain multiple transactions of the following
  types: `CreateChainTx`, `CreateNetTx`, `ImportTx`, `ExportTx`,
  `AddValidatorTx`, `AddDelegatorTx`, `AddNetValidatorTx`,
  `RemoveNetValidatorTx`, `TransformNetTx`, `AddPermissionlessValidatorTx`,
  `AddPermissionlessDelegatorTx`.
- **Proposal Blocks** may contain a single transaction of type
  `RewardValidatorTx`.
- **Option Blocks** (`Commit Block`, `Abort Block`) carry no transactions.

Every block header contains `ParentID`, `Height`, and `Time`.

## Block Formation Logic

The P-Chain picks transactions to include in the next block in this order:

1. **Advance chain time.** Try to move chain time forward to the current
   local time or the earliest staker-set change event, issuing a Standard
   or Proposal block as needed.
2. **Reward expiring stakers.** Walk any stakers whose staking period has
   ended at the new chain time and issue a `RewardValidatorTx` into a
   Proposal Block per staker.
3. **Pack mempool decision transactions.** Fill a Standard Block with
   mempool decision transactions up to the default block size.

Block formation terminates as soon as any step produces a block. If no
step finds work the builder schedules a fresh attempt and returns.

## Notes

[^1]: Proposal transactions whose start time is too close to local time are
dropped first and never included in any block.

[^2]: Advance-time transactions are proposal transactions that DO change
chain time, but they are generated just in time and never stored in the
mempool. Mempool proposal transactions are `AddValidator`, `AddDelegator`,
and `AddNetValidator`. `RewardValidator` is a proposal transaction that
does not change chain time and is also never in the mempool — it is
generated just in time.
