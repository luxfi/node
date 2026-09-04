# The P-Chain, in Rust

The chain that decides who validates: the validator sets, the stake behind
them, the clock those sets change on, and the networks they secure. Ported
from the Go reference at `~/work/lux/node/vms/platformvm`, against the VM seam
`~/work/lux-rs/node/src/vm.rs`.

A permissionless validator set with no admin key and no allowlist is what
makes this a public good rather than a product. Every check in `executor`
exists to make entry honest — enough stake, real stake, for long enough, once,
and signed for by whoever owns it. None exists to make entry selective.

```
cd chains/rust/platformvm
PATH=~/.cargo/bin:$PATH cargo test          # 293 tests
PATH=~/.cargo/bin:$PATH cargo clippy --all-targets
```

## The claim, and how it is checked

Two implementations of a chain agree about a transaction only if they agree
about its **bytes**, because the bytes are what a signature covers and what an
id is the hash of. So "same wire" is checked against Go, not against itself.

`tests/vectors/gen.go` builds each value with the **Go P-Chain's own
constructors** and prints the hex; `tests/wire_vectors.rs` holds those lines
verbatim and asserts both directions — the bytes this port writes are
byte-identical, and Go's bytes read back as the same value. Regenerate with:

```
mkdir -p /tmp/pvmvec && cp tests/vectors/gen.go /tmp/pvmvec/main.go
cd /tmp/pvmvec && cat > go.mod <<'EOF'
module pvmvec
go 1.26.5
require github.com/luxfi/node v0.0.0
replace github.com/luxfi/node => /home/z/work/lux/node
EOF
go mod tidy && go run .
```

33 vectors are checked this way: every transaction kind that carries an
envelope, the signed form with three credentials, all four block kinds
(including the golden abort block Go pins byte for byte), the unspent-output
envelope locked and unlocked, a whole genesis blob, a signature Go made —
which this port recovers to the same address Go derives from the key that made
it — and a proof of possession Go made, which verifies here under the same
domain tag.

Beyond the bytes, these Go test tables are ported case for case, and each one
is named in the Rust test that carries it: `TestVerifySpendUTXOs` (what value
may be created, locked and burned), `TestStakerDiffIterator` and
`TestMutableStakerIterator` (the order weight changes are read in),
`TestStakerLess` and the `TestBaseStakers*` set,
`TestAddPermissionlessValidatorTxSyntacticVerify` and its delegator sibling,
`TestUnsignedCreateChainTxVerify`, `TestTransformChainTxSyntacticVerify`, the
six `TestPriorityIs*`, `TestParseCredsBuf*`, `TestRewards`/`TestSplit`, the
whole `stakingparams` suite, `TestGoldenAbortBlock`, and
`TestHIGH2_SlashAmount*`.

`tests/hostile_bytes.rs` is the other half of the wire claim: every entry
point that takes bytes takes them from someone who chose them, so every honest
buffer is truncated at every length, mutated a byte at a time, given a length
nothing backs and a root pointing anywhere, and thrown at every reader. A
reader that can be made to panic on a chosen buffer is a way to stop a node
from a distance. The sweep reaches past the front door — most mutations still
parse, so the field accessors are exercised on hostile content rather than
refused at the header.

## What is here

| module | what it is |
| --- | --- |
| `zap` | the arena wire — a fixed payload plus relative pointers, read by indexing. Port of `luxfi/zap` v1.2.7. |
| `ids`, `components` | names, and the values an unspent output holds, plus the envelope one travels in. |
| `txs` | all twenty transaction kinds: their bytes, and the checks that need no state. |
| `sign` | who authorised a spend — secp256k1 recovery and the P-Chain address. |
| `signer` | how a validator proves the BLS key it signs with is its own. |
| `security` | on what terms a network's set is admitted. |
| `stakingparams` | the terms the set is admitted on, and the keyless rule that decides whether a change to them is admissible. |
| `reward` | what a staker is paid. |
| `flow` | value is not created, and the spend is authorised. |
| `state` | what the chain believes. |
| `uptime` | how much of its term a validator was reachable for — the node's measurement, not the chain's. |
| `executor` | what a transaction does to that belief. |
| `block` | the four things a block can be. |
| `genesis` | how the chain is born. |
| `vm` | the chain, as the node holds it. |

## The seam

`PlatformVm` has one inherent method per method of `lux-rs/node`'s `Vm` trait,
with the same names, the same `Id` (`lux_consensus::finality::Id`) and the same
error shape, so the host satisfies its trait by naming this type. The trait is
not re-declared here: a second declaration would be a second seam, and the two
could drift. `vm::Vm` and `vm::Block` in this crate state the same shape for
this crate's own use.

`PlatformVm::from_genesis` is the whole birth of a network in one call: hand it
the bytes a network was published as and it holds the first block, the first
validator set and the first balance, having trusted nobody. The first block is
a commit block naming the genesis bytes as its parent at `GENESIS_TIME`
(2020-12-05T05:00:00Z) — the shape Go's `state.init` builds, so both
implementations name the first block identically.

## What is deliberately absent

Nothing here is a stub. Where a thing is not held, the path that would need it
refuses **by name** with its own error, rather than succeeding against nothing.

- **The L1-validator plane.** Registrations, their fee balances, the expiry of
  a registration nobody paid for, and the validator-fee state that drains
  them. `RegisterL1Validator`, `SetL1ValidatorWeight`,
  `IncreaseL1ValidatorBalance` and `DisableL1Validator` parse and verify
  syntactically — their bytes are byte-identical to Go — and the executor
  refuses them with `L1ValidatorPlaneNotHeld` / `WrongTxType`. A
  `CreateNetwork` that asks for a set of its own, and every `ConvertNetwork`,
  refuse with `OwnSetNotHeld`; a `CreateNetwork` that restakes its parent
  executes for real.
- **Warp.** Signed cross-chain messages and their aggregate verification. The
  L1 transactions carry a message this port stores and does not interpret.
- **Atomic import.** `ImportTx` needs the proof that the source chain produced
  the output; without the shared-memory half there is nothing to check, so it
  is refused rather than trusted.
- **`TransformChainTx`.** Refused — which is *fidelity*, not a gap: Go refuses
  it permanently too (`errTransformChainTxNotPermitted`).
- **A network's own staking terms.** A network states them in the
  transformation that made it staked, and since that transaction is refused,
  no transformation is ever recorded. So the only network whose terms can be
  answered is the primary one, and everything that would need another's says
  `NetworkTermsNotHeld`: admitting a validator, admitting a delegator,
  promoting one into a set (which would mint on the wrong schedule), and the
  reward gate. Substituting the primary network's terms would judge a
  network's validators on rules nobody agreed to, which is worse than
  answering nobody.
- **Dynamic fees.** `FlatFees` is Go's static schedule. The gas-metered
  alternative — complexity times weights times a price that moves with demand —
  is not ported.
- **Persisting a node's measurements.** `uptime::Tracker` is ported whole,
  with Go's whole test suite, and `uptime::Ledger` keeps its measurements in
  memory. A node that wants them to survive a restart writes its own
  `uptime::Record` over whatever it already persists — which is right, because
  these numbers are not agreed and must not be in the state that is.
- **Persistence.** `State` is in memory. There is no database, no versioned
  diff layer, and no height-indexed validator sets (Go's weight diffs, which
  answer "who was validating at height N").
- **The node's edges.** Mempool policy, block-building heuristics, gossip, the
  metrics surface, and the full JSON-RPC service. `call` answers four
  read-only methods (`getHeight`, `getTimestamp`, `getCurrentSupply`,
  `getCurrentValidators`).
- **Legacy scheduled stakers.** `AddValidatorTx` and `AddDelegatorTx` are read
  off the wire and refused by the executor — again matching Go, where the
  scheduled-staker flow has no role now that stakers enter immediately.

## Things worth knowing before changing anything

- **A number in a wire layout is consensus.** The offsets in `txs`, `block`,
  `genesis` and `components` are the wire. Changing one silently reinterprets
  every transaction already on disk. A slot that stops naming a kind stays a
  hole rather than letting the ones after it slide down.
- **The unsigned bytes are a byte-prefix of the signed bytes.** Both halves are
  self-delimiting zap messages, so the split is found by reading the first
  message's length rather than by re-encoding anything.
- **An id is the hash of the bytes that travelled.** Genesis stores embedded
  transactions as their own signed bytes for exactly this reason.
- **`verify_credentials` is not optional.** It used to take the recovery
  function as an argument and nothing in the executor supplied it, so a
  transaction could execute with no signature checked at all. It now calls
  `sign::recover` directly: a check that can be handed a different answer is a
  check that can be handed one that always agrees.
- **Reward arithmetic is exact.** Go divides with `big.Int` at the end;
  `reward` reproduces that with the identity `floor(floor(x/a)/b) ==
  floor(x/(a·b))` so every divisor fits a `u64`, and the Go test table is
  reproduced value for value.
- **Money is not votable.** `stakingparams` governs admission thresholds,
  durations, the delegation-fee floor and the uptime requirement — and never
  the consumption rates, minting period, supply cap or fee split. A test reads
  this file's own source to hold that line.

## The one dependency worth explaining

`lux-consensus` is named for `finality::Id` alone — the host's own id type, so
a chain here plugs into `lux-rs/node`'s `Vm` with no conversion at the join.
`blst`, `k256`, `sha2` and `ripemd` are the curves and hashes the Go node uses;
a second implementation of any of them would be a second answer to who signed.
`serde_json` is the shape the seam's `call` speaks in — the node's RPC
boundary, not a serialization of chain state. ZAP is the only thing chain
values are written in.
