# SPDX-License-Identifier: BSD-3-Clause-Eco
# The warp wire — what one chain says to another, and the payloads it says.
#
# The offsets are the format. They are stated here once and every language's
# accessors come out of this file, so a warp message a Rust node signs is the
# message a Go node verifies.
#
# THREE LAYERS, EACH ITS OWN MESSAGE, and the nesting is what makes the
# signature mean something. A signed message carries the UNSIGNED message's
# bytes and a signature over exactly those bytes; the unsigned message carries
# a payload's bytes; the payload carries an addressed call's bytes. Each layer
# is self-delimiting, so a verifier finds the bytes a signature covers by
# reading a length rather than by re-encoding what it just read — and a
# re-encoding that came out different would be a different message under the
# same signature.
#
# The kind byte at 0 of a payload and of an addressed call says which struct
# to read it as, the same way a transaction's does.

package warp

type id32 = bytes_fixed[32]
type addr = bytes_fixed[20]
type key = bytes_fixed[48]
type sig = bytes_fixed[96]

# ---- the envelope ---------------------------------------------------------

# What a source chain signs: which network, which chain, and what it says.
struct Unsigned {
    NetworkID u32   @0
    Source    id32  @4
    Payload   bytes @36
}

# The unsigned message's bytes and the signature over them.
struct Message {
    Unsigned  bytes @0
    Signature bytes @8
}

# An aggregate signature and the bit set naming who is in it. The signers are
# a bit set rather than a list because the verifier already holds the ordered
# validator set: what it needs is which of them, not who they are.
struct BitSetSignature {
    Kind      u8    @0
    Signature sig   @1
    Signers   bytes @97
}

# ---- what an unsigned message carries -------------------------------------

# A payload that is a hash and nothing else: the chain asserts a value, and
# what it means is the reader's business.
struct Hash {
    Kind u8   @0
    Hash id32 @1
}

# A payload addressed to a contract on the destination chain.
struct AddressedCall {
    Kind    u8    @0
    Source  bytes @1
    Payload bytes @9
}

# ---- what an addressed call carries ---------------------------------------

# A network became an L1, under this manager and this validator set.
struct Conversion {
    Kind u8   @0
    ID   id32 @1
}

# One validator asking to be registered on an L1.
struct RegisterValidator {
    Kind             u8         @0
    Chain            id32       @1
    BLSKey           key        @33
    Expiry           u64        @81
    Weight           u64        @89
    NodeID           bytes      @97
    RemoveThreshold  u32        @105
    RemoveAddrs      list<addr> @109
    DisableThreshold u32        @117
    DisableAddrs     list<addr> @121
}

# The L1 saying a registration landed, or did not.
struct Registration {
    Kind         u8   @0
    ValidationID id32 @1
    Registered   u8   @33
}

# A validator's weight, set by the L1's manager. The nonce is what stops an
# older weight from being replayed over a newer one.
struct ValidatorWeight {
    Kind         u8   @0
    ValidationID id32 @1
    Nonce        u64  @33
    Weight       u64  @41
}

# ---- what a conversion is over --------------------------------------------

# One genesis validator of a converted network, inline at a fixed stride. Its
# node id is a run in a message-wide blob, which is what NodeIDStart and
# NodeIDLen name.
struct ConversionValidator {
    NodeIDStart u32 @0
    NodeIDLen   u32 @4
    BLSKey      key @8
    Weight      u64 @56
}

# The bytes a conversion id is the hash of.
struct ConversionData {
    Chain      id32                      @0
    ManagerID  id32                      @32
    ManagerAdr bytes                     @64
    Validators list<ConversionValidator> @72
    NodeIDPool bytes                     @80
}
