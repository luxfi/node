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
PATH=~/.cargo/bin:$PATH cargo test          # 407 tests
PATH=~/.cargo/bin:$PATH cargo clippy --all-targets
```

**One build hazard, and it is the machine's, not the code's.** This crate's
dependency graph reaches `lux-rs/node`, which is large, and `~/.cargo/config.toml`
routes every rustc through a shared compile cache. Under load that cache has
handed back artifacts that do not match their crate — the symptom is
`E0463: can't find crate for alloy_rlp` with the rlib sitting right there on
disk. `CARGO_BUILD_RUSTC_WRAPPER="" cargo …` bypasses it and builds
deterministically; a stale-looking failure is worth one retry that way before
it is believed.

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
| `store` | where that is written down: a byte map whose batch is all there or none of it is. |
| `persist` | what is written down, as records, and how it is read back. |
| `validators` | who validates, now and at any height already passed. |
| `uptime` | how much of its term a validator was reachable for — the node's measurement, not the chain's. |
| `executor` | what a transaction does to that belief. |
| `block` | the four things a block can be. |
| `genesis` | how the chain is born. |
| `vm` | the chain, as the node holds it. |

## The seam

`PlatformVm` implements `lux-rs/node`'s own `Vm` — `lux_node::vm::Vm`, reached
through a path dependency on that crate and re-exported from `vm` for
convenience. Nothing of the seam is declared here. A restated trait of the same
shape compiles and satisfies nothing, because the node's chain map holds
`Box<dyn lux_node::vm::Vm>` and only the host's own trait coerces into it;
`the_seam_is_the_hosts_own_declaration` builds that box by its full path so the
claim is checked rather than asserted. `Id` is `lux_consensus::finality::Id` on
both sides of the join, so there is one id type in a node's chain map.

The dependency runs chain → node, not node → chain: a chain naming the seam it
plugs into cannot also be named by it. Registration therefore belongs to
whatever holds both, which is this repo.

`PlatformVm::open` is the one way to start a chain that writes itself down: it
resumes what the store holds, or writes the chain's birth if it holds nothing.
Two entry points would be two answers to which chain this is, and the wrong one
is a node that silently rejoins at height zero. What comes back is checked, not
trusted.

`PlatformVm::from_genesis` is the whole birth of a network in one call: hand it
the bytes a network was published as and it holds the first block, the first
validator set and the first balance, having trusted nobody. The first block is
a commit block naming the genesis bytes as its parent at `GENESIS_TIME`
(2020-12-05T05:00:00Z) — the shape Go's `state.init` builds, so both
implementations name the first block identically.

## The differential

`conformance/chain-differential` hands one corpus of Go-built bytes to the Go,
Rust and C++ P-chains and compares every answer with every other. `src/bin/
conformance.rs` is this chain's voice in it.

On **all 39 P-chain vectors this chain now answers exactly what Go answers** —
parse, kind, id, syntactic verdict and execution verdict. The two P-chain rows
the run still reports are C++'s, and Go sides with this chain on both:
`P_EDGE_EMPTY_NODE_ID`, where C++ refuses `AddValidator` by kind before it looks
at the validator, and `P_SEAM_BLOCK_REJECT`, where the C++ host seam has no
`reject` at all.

That is what closed the fork. It used to be the other way round: C++ executed
the sovereign-L1 plane and this chain refused it by name, and the run named six
vectors for it.

## What is deliberately absent

Nothing here is a stub. Where a thing is not held, the path that would need it
refuses **by name** with its own error, rather than succeeding against nothing.

- **Warp aggregate verification, on the execution path.** The signed envelope
  and the addressed call are opened and read, and the message's source chain
  and address are checked against the conversion the L1 recorded. The aggregate
  BLS proof over the source chain's validator set is `verify_warp_messages`,
  which Go likewise runs as its own pass at the height the block is verified
  against — the set that signed has to be the set as it stood then, which is
  what `validators::History` now answers. Wiring that pass into the block
  executor is the remaining half.
- **`TransformChainTx`.** Refused — which is *fidelity*, not a gap: Go refuses
  it permanently too (`errTransformChainTxNotPermitted`). A network's terms are
  therefore whatever it was born with, and a network born without them refuses
  by name rather than borrowing the primary network's.
- **Dynamic fees.** `FlatFees` is Go's static schedule. The gas-metered
  alternative — complexity times weights times a price that moves with demand —
  is not ported. `gas` holds the arithmetic; nothing calls it.
- **Persisting a node's measurements.** `uptime::Tracker` is ported whole,
  with Go's whole test suite, and `uptime::Ledger` keeps its measurements in
  memory. A node that wants them to survive a restart writes its own
  `uptime::Record` over whatever it already persists — which is right, because
  these numbers are not agreed and must not be in the state that is.
- **The node's edges.** Mempool policy, block-building heuristics, gossip, the
  metrics surface, and the full JSON-RPC service. `call` answers five
  read-only methods (`getHeight`, `getTimestamp`, `getCurrentSupply`,
  `getCurrentValidators`, `getValidatorsAt`).
- **Legacy scheduled stakers.** `AddValidatorTx` and `AddDelegatorTx` are read
  off the wire and refused by the executor — again matching Go, where the
  scheduled-staker flow has no role now that stakers enter immediately.

## The two halves a node fills

Neither is a stub and neither has a default that pretends. Both are traits the
node implements, because both are things the chain cannot know by itself.

- `executor::Uptime` — how much of its term a validator was reachable for. Two
  honest nodes measure differently, which is why the reward is a *proposal*.
- `executor::Atomic` — what another chain has already handed over. An import
  spends value made somewhere else, so only the shared half both chains write
  to can say the export happened. `NoImports` is the honest answer for a node
  with no such half: every import is refused for want of what it names, exactly
  as Go answers from an empty shared memory. Removing what an import consumed
  from the shared half is the node's, on accept — Go splits it the same way,
  returning `AtomicRequests` for the node to apply.

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
  check that can be handed one that always agrees. Both it and
  `verify_permission` — the one-owner check the authorisation paths call, and
  where the recovery actually happens — have their types pinned by a `const _`
  in `flow`, so adding a parameter of any kind stops the build rather than
  waiting for someone to notice.
- **Where a transaction is addressed is part of the bytes being well formed.**
  `syntactic_verify` takes a `txs::Chain` — network, blockchain, native asset —
  and refuses a mismatch before it looks at anything else, because a
  transaction that is well formed on two networks is one signature that spends
  on both.
- **The set at a past height is not the set now.** A signature made at height
  H is checked long after H. `validators::History` records what each accepted
  height changed and rewinds; `PlatformVm::validator_set_at` answers, and
  refuses a height the chain has not reached rather than extrapolating one.
- **Reward arithmetic reproduces a defect on purpose.** `cap - supply` wraps
  past the cap and the live chain is past it. See `reward`.
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
