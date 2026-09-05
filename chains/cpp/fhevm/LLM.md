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
    cmake -B build/san -S . -DFHEVM_SANITIZE=address,undefined -DCMAKE_BUILD_TYPE=Debug
    cmake --build build/san -j && (cd build/san && ctest)

    # And the one measurement a green suite does not make: the JSON path at
    # Go's full depth cap, on a stack the size a thread gets rather than the
    # size main gets. It passes at every size down to 256 KB; before parse and
    # teardown were made flat it died below 4 MB.
    for s in 8192 4096 2048 1024 512 256; do
      (ulimit -s $s; ./build/san/json_test >/dev/null 2>&1; echo "$s -> $?")
    done

Fourteen suites, ~1150 assertions. `differential` is the load-bearing one: every
constant in `test/golden.hpp` came out of the GO F-Chain, and that suite
reproduces each in C++ — the wire bytes, the ids, the effects, the gas for every
(operation, scheme, payload-length) triple, the four persisted records, the
genesis document and the genesis block id. It also verifies GO-PRODUCED ML-DSA-65
signatures, which is how the two implementations are known to agree on FIPS 204's
pure variant with an empty context rather than assumed to.

`fhevm_conformance` is the other half, and it is not a test: it reads the shared
`conformance/corpus/vectors.tsv` and PRINTS this chain's answer per vector, which
`make chains` compares against the Go F-chain's. F contributes 264 rows there —
the six operations, the well-formedness and authorisation edges, the wire damage,
and a `F_JSON_*` group covering exactly the decoder rules named below, each one a
correct transaction with one thing done to its payload. Those are the vectors
that would catch any of these rules coming back:

    F_JSON_TRAILING_BRACE/BRACKET   Decoder.More is not "are there bytes left"
    F_JSON_UPPERCASE/LONG_S/KELVIN  names fold by SimpleFold, not by ASCII case
    F_JSON_DUPLICATE_KEY            two keys, one field: the later one wins
    F_JSON_ARRAY_TAIL_DISCARDED     a discarded element is never type-checked
    F_JSON_BASE64_NEWLINE           base64 ignores \r and \n
    F_JSON_NULL_*                   null is the zero struct, per operation
    F_JSON_EMPTY/WORDED_NULL_GRANTEE  the two words an address takes as zero

The corpus is the committed gate, and it carries the cases that were FOUND. What
found them was a throwaway differential over the decode discipline itself: a Go
program emitting one line per document — the bytes, and the verdict
`Decode`+`DisallowUnknownFields`+`More` reaches — and a C++ program required to
reach the same verdict on every line. Over 269,730 documents for the register
form (every one- and two-token document from a 49-token alphabet of closers,
commas, folded names, boundary numbers and cut-short escapes, plus random longer
ones and random raw bytes) and 165,348 for the revoke form (which compares the
decoded STRING byte for byte, so escapes, surrogate pairs and every ill-formed
UTF-8 sequence Go rewrites to U+FFFD are in scope): zero disagreements, and the
same zero under ASan. That search is not in the tree — it is a net, not a gate,
and keeping it would mean maintaining a second differential beside the corpus —
but it is a hundred lines to rebuild against `conformance/gen`'s module if the
decoder is ever touched again, and it is what the `F_JSON_*` vectors came out of.

Regenerate the goldens by copying `test/golden_gen_test.go` into
`~/work/lux/chains/fhevm` as `cppgolden_test.go` and running

    FHEVM_CPP_GOLDEN=<abs path to test/golden.hpp> go test ./fhevm -run TestGenerateCppGolden

## What this port decided, and why

**JSON is a consensus question here, not a formatting one.** A payload is opaque
bytes the chain keeps verbatim, and whether it DECODES decides whether the
transaction is valid — so `json.hpp` reproduces Go's `encoding/json` exactly:
integer literals handed to a strconv-strict parser, Go's array rule (extra
elements discarded WITHOUT a type check, missing ones zero), `null` as the zero
value for a member AND for a whole struct, `[]byte` as base64 (which ignores
`\r` and `\n`), `ids.ShortID` as cb58, `ids.NodeID` as `NodeID-<cb58>`, `ids.ID`
as its native-chain name or its one-letter alias where it has one. The writer
matches `json.Marshal` down to the HTML escaping, the sorted map keys, and
`\ufffd` for every byte that starts no well-formed rune.

Two of those rules are subtler than they look, and each was measured against the
reference rather than read off:

**The chain reads JSON two ways, and they are different acceptance sets.**
Payloads go through a `Decoder` with `DisallowUnknownFields` (`transaction.go`
decode); records and genesis go through plain `json.Unmarshal` (`vm.go` loadInto,
Initialize). So a member the schema does not describe is REFUSED in a payload and
IGNORED in a record — and `Decoder.More`, which asks whether another ELEMENT
follows, answers false at `]` and `}`, while `Unmarshal` scans the whole document
and refuses any trailing byte. `json::more` and `json::trailing` are those two
questions, and `json::Unknown` is which side of the line a reader is on. Reading
records under the payload rules SKIPPED rows the reference loads, and refused a
genesis it starts a chain on.

**Field names fold by `unicode.SimpleFold`, not by ASCII case.** Every schema
name here is ASCII, and exactly two runes outside ASCII fold onto an ASCII
letter — U+017F onto s, U+212A onto k — so `{"ſize":…}` names Size and
`{"public<U+212A>ey":…}` names PublicKey, in Go and now here. And with two keys
naming one field the LATER one wins whichever way each matched, because Go
resolves each key on its own and then writes it; preferring the exact match read
a different value out of the same bytes.

**A decoder that recurses is a decoder the payer controls.** Both the parser and
`~Value` are iterative, and it took both. Capping the open containers at Go's
10,000 was not enough: at that depth a Debug build with the sanitizers on still
walked off the stack, so the same 128 KiB of brackets was a refusal on one build
and fatal on another — a consensus property decided by the compiler's frame size.
Making the parser flat moved the fatality to the teardown, where it was worse: a
destructor cannot refuse anything, only abort, and it died below a 4 MB stack.
Now the depth a payload reaches costs heap and the whole path holds at 256 KB.
The cap stays at Go's number, because that is which documents the two agree to
refuse — it was never the safety argument.

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

- **The JSON nesting cap is not in the corpus, deliberately.** Both sides of
  Go's boundary — 10,000 open containers and 10,001 — are refused with the same
  compared verdict, and the runner weighs parse/kind/id/syntactic/exec rather
  than the sentence, so a vector could not tell a wrong cap from a right one. It
  would cost 1.3 MB of brackets to say nothing. The cap is pinned where it can
  be seen instead: `test/json_test.cpp`, against the two answers Go was asked
  for directly, and run under the sanitizers.

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
