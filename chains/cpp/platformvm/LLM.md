# platformvm — the P-chain, in C++

The Lux platform chain: the validator set, the staking rules that admit and pay
it, and the blocks that change it. Ported from the Go reference at
`~/work/lux/node/vms/platformvm` (42,398 LoC across 303 files, 115 of them
tests), against the VM seam at `~/work/lux-cpp/node/include/lux/node/vm.hpp`.

A node joins this chain's validator set by staking. There is no allowlist, no
admin key and no argument anywhere in the tree to pass one — that property is
what makes the chain a public good, and it is asserted by a test
(`APermissionlessValidatorJoins`) rather than assumed.

## Building

```
cmake -S . -B build && cmake --build build -j && (cd build && ctest)
```

Three checkouts are REQUIRED and are found where they actually sit; each is
matched on a FILE rather than a directory name, so a same-named sibling cannot
be mistaken for one. Override with `-DLUXCPP_ROOT`, `-DNODE_DIR`,
`-DCONSENSUS_DIR`, `-DCRYPTO_DIR` when building from an unusual path.

| what | where | why it is required |
| --- | --- | --- |
| `luxcpp/blst` | `~/work/luxcpp/blst` | a proof of possession is a real pairing |
| `luxcpp/crypto` | `~/work/luxcpp/crypto` | secp256k1 recovery + ripemd160 + keccak + sha256 |
| `lux-cpp/node` | `~/work/lux-cpp/node` | the VM seam this chain registers through |
| `lux-cpp/consensus` | `~/work/lux-cpp/consensus` | the id type that seam is written in |

There is no build in which the crypto is skipped. A validator set that admits a
key nobody holds is not a validator set, and a chain that cannot check who
signed has no spending rule, so a build that cannot do either does not produce a
P-chain at all.

`-DPLATFORMVM_SANITIZE=address,undefined` builds the chain's own code under
sanitizers (blst's assembly stays clean). The suite passes under them.

## Layout

```
include/lux/platformvm/
  zap.hpp          the ZAP v1.2.7 zero-copy codec — builder and reader
  ids.hpp          Id (32B), ShortId (20B), NodeId — three distinct types
  sha256.hpp       the hash every id and every signature is over
  safemath.hpp     u64 arithmetic that refuses to wrap, + the 512-bit integer
  components.hpp   what a transaction spends and produces
  security.hpp     a network's security mode, as two orthogonal axes
  signer.hpp       the BLS key a validator registers, and the proof it owns it
  priority.hpp     how stakers scheduled for one instant are ordered
  txs.hpp          every transaction the chain accepts
  block.hpp        the four block kinds and their wire
  state.hpp        the validator set, the diff layers, the state root
  fx.hpp           who may spend an output, and the proof of it
  flow.hpp         no value is created, and locked value stays locked
  gas.hpp          what a transaction costs the chain, in four dimensions
  complexity.hpp   the LP-103 fee schedule
  reward.hpp       the emission curve
  executor.hpp     what a transaction DOES to the validator set
  atomic.hpp       money crossing to and from another chain
  uptime.hpp       how much of its term a validator was there for
  validators.hpp   who validates a network, and the set commitment
  warp.hpp         a message another chain signed, and the aggregated proof
  warpmsg.hpp      what that message SAYS
  l1.hpp           a validator of a sovereign network, and the fee it pays
  staking.hpp      the terms of validating, and what may change them
  adopt.hpp        the register of networks Lux did not create
  mempool.hpp      what is waiting to go into a block
  genesis.hpp      the state the network starts in
  vm.hpp           the chain, as the node's VM seam sees it
```

## The three ideas worth knowing

**The struct IS the wire.** A transaction holds its ZAP buffer and reads its
fields by offset. There is no codec, no marshal step and no second
representation that could disagree with the first — so the bytes that were
signed are the bytes that were stored, and "which spelling did we sign" is not a
question anyone can ask. A signed transaction is `unsigned ‖ credentials`, both
self-delimiting, so the unsigned bytes are a genuine byte-prefix of the signed
ones.

**Order is consensus.** A staker is ordered by when it next moves, then by
priority, then by the id of the transaction that created it. The priority groups
exist because permissioned stakers leave the set by the clock and permissionless
ones leave by being paid; interleaving them would let the clock take a
validator's stake without paying for it. Two nodes that walk the set differently
disagree about who validates — a fork with no bytes to blame.

**Execution is not optional.** `verify()` builds the state a block WOULD produce
on a layer over its parent's and keeps it; `accept()` applies that layer. A
block's `root()` is sha256 over what its execution produced — the clock, the fee
position, the supply of every network, both staker sets in order, every unspent
output, and every network with its owner — computed by running the block, never
copied from a proposer. A validator that signed a block it had not executed
would be certifying a name rather than a result.

## Proof, not self-agreement

A round-trip test proves an implementation agrees with itself, which is also
what a fork does. So:

- `test/golden.hpp` pins bytes and ids printed by the Go package itself: all
  nineteen transaction kinds, the four block kinds, the credential buffer, the
  owner encoding, and a signed transaction with its id. Regenerate them with a
  Go module that imports `github.com/luxfi/node/vms/platformvm` and prints hex;
  a diff there is a wire change, and a wire change is a hard fork.
- `flow_test.cpp` recovers the Go reference's own address from the Go
  reference's own signature over the Go reference's own transaction.
- `validators_test.cpp` checks this chain's set commitment against the NODE's
  implementation of the same hash, not against its own.
- `warp_test.cpp` verifies the Go reference's own aggregated signature, over the
  Go reference's own message, by the Go reference's own three validators.
- `reward_test.cpp` recovers a real mainnet reward from real mainnet inputs.
- Every ported refusal asserts the SAME sentinel the Go original asserts. A rule
  that rejects for the wrong reason will one day accept for the wrong reason.

## What is here, and what is not

234 cases across 19 suites, all green, clean under address+undefined sanitizers.

**Ported and tested.** All nineteen transaction kinds and all four block kinds,
byte-identical, with their full syntactic verification. Every execution path the
reference runs: admission, delegation, removal, networks, chains, ownership,
import and export, the clock and everything that follows from advancing it, the
reward proposal computed on both outcomes before the vote, and the reward gate
that decides which one this node prefers. The staker set with its ordering, base
and diff layers, the mutable walk and the staker-diff walk. The fx signature
check over real secp256k1 recovery and the value-conservation flow check. The
LP-103 gas dimensions, fee schedule and price curve, with the chain reading its
own price off its own excess. The emission curve and the reward split. Warp: the
message wire, the canonical validator set, the aggregated BitSet signature, the
envelope and the four things an L1 says. The whole L1 subsystem: the validator
record, the expiry set that refuses a replay, the LP-77 continuous fee, the four
transactions that register, reweight, top up and switch off an L1 validator, and
the two that establish a sovereign network's own set. The validator set consensus
samples and its commitment, and the set at any height that has already passed —
which is what makes a signature from back then checkable now. The staking
constitution: what the validator set may vote about its own terms, the envelope
no vote leaves, the brake on exclusionary change, and the history that makes a
validator judged on the terms it agreed to. The LP-1021 register of networks
Lux did not create — what is believed about each one, on what basis, and the
single question a bridge asks before it releases anything. What is waiting to go
into a block, and everything a node refuses to spend on a stranger. The genesis
blob — the money, the
first validators and the first chains a network starts with — encoded, parsed
and validated, against the bytes the Go package wrote. And the VM itself — build, parse, get,
prefer, verify, accept — through the node's seam.

**Absent, and why.** Each of these returns the reason it cannot run rather than
a success it has not earned:

- **The post-quantum warp signature** (`CoronaSignature`) and the teleport
  payloads — threshold BLS over ML-KEM, a separate construction. Their wire kinds
  parse to a named refusal rather than to a signature that verifies trivially.
- **`TransformChainTx` execution** — refused here, as in the reference, which
  refuses it permanently.
- **State persistence** — `state::MemState` is the accepted state in memory. The
  reference's on-disk layout (~2,000 lines of key encodings, height diffs and
  batched commits) is not ported. Nothing above it assumes memory: `Chain` is an
  interface and a disk-backed implementation is a sibling of `MemState`. What
  each height CHANGED about the validator sets is here — recorded as blocks are
  accepted, so the set at any past height is the set now with everything since
  undone — but it lives in memory with the state, and a node that restarts
  starts that record again.
- **The JSON-RPC service and client** (~3,200 lines) — the node's seam does not
  ask for them.
- **Gossip, metrics, and the uptime tracker** — the first two are node
  integration; uptime is a MEASUREMENT the node makes, so it enters through
  `uptime::Calculator` and no other way. A VM that could compute its own uptime
  could decide its own reward. Shared memory and the source chain's validator set
  are interfaces for the same reason: an import that could invent the other
  chain's outputs, or a warp check that could invent the set that signed, would
  be a chain deciding what other chains said.

**One deliberate divergence from the reference's shape.** Go models a
stake-locked output as a wrapper TYPE and unwraps it everywhere it matters; the
wire has always carried a stake-lock FIELD beside the output's own fields. So it
is a value here, not a second type: zero means unlocked. Same bytes, same
verification, one shape instead of two.

**One thing the reference does that looks like a bug and is kept.** The reward
curve's `supplyCap - existingSupply` is unguarded and wraps once supply passes
the cap — and Lux mainnet has passed it, so the live chain's rewards are computed
from the wrapped value. A clamp here would be a monetary change, and this is a
port. It is reproduced and pinned by a test.
