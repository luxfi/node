# fhevm — the Lux F-Chain in C++

The coordination plane for confidential compute (LP-8200, LP-167), rendered from
the Go implementation at `~/work/lux/chains/fhevm` and running behind the node's
VM seam (`~/work/lux-cpp/node/include/lux/node/vm.hpp`).

F records the PUBLIC coordinates of encrypted values — a handle, the digest of
the off-chain ciphertext body, its owner, the capabilities granted over it, and
the threshold decryptions asked for and answered — and it records nothing else.
It holds no ciphertext body, no FHE secret key and no decryption share.

## Layout

    include/lux/fhevm/    src/
      id            identifiers, sha256, and the cb58/base64/hex renderings
      json          JSON as Go's encoding/json does it
      error         the refusals, one enumerated value each
      records       what the chain persists, and the derivations that name it
      fee           metered gas, a balance ledger, and the burn
      gas           what an operation costs, by operation and by FHE scheme
      auth          ML-DSA-65 payer authentication — VERIFY ONLY
      transaction   the six operations and every rule each has to satisfy
      wire          struct-is-wire over ZAP, canonical in both directions
      store         where the state rests, and the one durability point
      block         verify, accept, reject
      vm            the seam implementation, the mempool, and the batch
      service       the read-only public views

Reused, not vendored: `luxcpp/pqclean` + `luxcpp/crypto/mldsa` (FIPS 204),
`lux-cpp/node` (the seam), `lux-cpp/consensus` (the `Id` type), and
`chains/cpp/zap` (the codec).

## Building and testing

    cmake -B build -S . && cmake --build build -j && (cd build && ctest)

    # ASan + UBSan, which the suite is clean under:
    cmake -B build -S . -DFHEVM_SANITIZE=address,undefined -DCMAKE_BUILD_TYPE=Debug
    cmake --build build -j && (cd build && ctest)

Fourteen suites, ~1150 assertions. `differential` is the load-bearing one: every
constant in `test/golden.hpp` came out of the GO F-Chain, and that suite
reproduces each in C++ — the wire bytes, the ids, the effects, the gas for every
(operation, scheme, payload-length) triple, the four persisted records, the
genesis document and the genesis block id. It also verifies GO-PRODUCED ML-DSA-65
signatures, which is how the two implementations are known to agree on FIPS 204's
pure variant with an empty context rather than assumed to.

Regenerate the goldens by copying `test/golden_gen_test.go` into
`~/work/lux/chains/fhevm` as `cppgolden_test.go` and running

    FHEVM_CPP_GOLDEN=<abs path to test/golden.hpp> go test ./fhevm -run TestGenerateCppGolden

## What this port decided, and why

**JSON is a consensus question here, not a formatting one.** A payload is opaque
bytes the chain keeps verbatim, and whether it DECODES decides whether the
transaction is valid — so `json.hpp` reproduces Go's `encoding/json` exactly:
unknown members refused, trailing content refused, field names matched exactly
then case-insensitively, `null` as the zero value, integer literals handed to a
strconv-strict parser, Go's array rule (extra elements discarded, missing ones
zero), `[]byte` as base64, `ids.ShortID` as cb58, `ids.NodeID` as `NodeID-<cb58>`,
`ids.ID` as its native-chain name where it has one. The writer matches
`json.Marshal` down to the HTML escaping and the sorted map keys.

**Verification is the whole of the auth surface.** `auth.hpp` offers no signing
and no key generation: a payer signs offline with a key F never sees, and a
library that COULD sign would have to be handed something to sign with. The
tests reach FIPS 204 directly through `test/signer.hpp`, on the test side of the
line where the invariant scan can see it.

**F commits no state root, and `Block::root()` says so** by returning the empty
id. The Go chain implements no execution-root surface either — putting a root in
consensus needs a state layer per in-flight block, which this VM does not have.
Reporting a root Go does not report would be worse than reporting none: two
implementations of one chain would hand consensus different values for the same
block. What stands in its place is `replay_test`, which drives one chain onto
nodes whose clocks differ by years and requires their databases to come out
byte-identical; `VM::records_root()` is the digest it compares, and it is NOT a
consensus commitment.

**Chain time is integer seconds throughout.** The wire carries Unix seconds, so
that is the resolution every comparison uses. Go keeps sub-second precision in a
proposer's in-memory block and drops it at the wire, which is a difference
without a consequence at this resolution but is written down here rather than
left to be rediscovered.

**One unreadable ROW does not stop a boot; one unreadable INDEX does.** That is
Go's rule and the reason is stated there: the first says one record cannot be
trusted, the second says nothing can. `VM::skipped()` counts the rows that did
not decode, so a node knows what it is serving.

**The codec has one home.** `chains/cpp/zap` is the chain-neutral port of the Go
ZAP codec. The four in-tree copies under `chains/cpp/{platformvm,xvm,quantumvm,
zkvm}/include` predate it and differ from it only by namespace (verified
byte-identical modulo the namespace line); they should fold into this one as each
of those ports next moves. This port added `set_i64`/`i64`, which the F-Chain's
block timestamp needs and which are Go's `SetInt64`/`Int64` exactly.

## Open, and named rather than hidden

- **The FHE runtime has no C++ port.** `Service::public_params` reports the
  runtime's parameters (LogN 14, LogQP 435, LogScale 45 — lattigo's
  `ExampleParameters128BitLogN14LogQP438` at the default threshold config) as
  pinned values, because the Go chain reads them from a live runtime this side
  does not have. When `mpcvm/fhe` is ported, those read from it. The values are
  pinned in the test too, so a change to the runtime's parameters shows up as a
  failure rather than as two networks encrypting under different moduli.

- **The differential harness does not yet cover F.** `conformance/harness_runner.py`
  evaluates `corpus/chain_differential.json` across platformvm, xvm, quantumvm
  and zkvm; there are no F vectors, so this port is checked against Go by its own
  golden header instead. Adding F to the shared corpus needs Go-side generation
  in `conformance/gen`, which is that harness's change and not this one.

- **Concurrency is the host's.** This VM takes no locks, matching the house
  shape (`chains/cpp/xvm` does the same) and the seam's single-threaded drive.
  Go's VM is concurrent and carries a lock order for it; if the C++ host ever
  drives one chain from several threads, that ordering has to come across with
  it — `stateLock` then `mempoolLock`, never the other way.

- **A child of a verified-but-unaccepted block from the SAME payer cannot be
  verified**, because verify reads committed state and the parent's nonce is not
  committed yet. This is Go's limit too, recorded there for the same reason: it
  needs the per-block state overlay that a state root would also need. A child
  from a DIFFERENT payer verifies fine, and there is a test for it.
