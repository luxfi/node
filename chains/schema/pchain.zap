# SPDX-License-Identifier: BSD-3-Clause-Eco
# The P-chain wire.
#
# The offsets are the format. They are stated here once and every language's
# accessors come out of this file, so a P transaction built in Rust is the
# bytes a Go peer builds.
#
# Every transaction opens with the same eight fields — which network, which
# chain, what is spent, what is made — and then says its own thing after them.
# Each kind repeats those eight rather than naming a shared one, because this
# file is a table of consensus offsets: what a kind is, read straight down,
# with nothing to resolve first. The kind byte at 0 says which table to read.
#
# Fields are written in the order they are declared, which is the order of
# their offsets, which is the order the bytes are in.

package pchain

type id32 = bytes_fixed[32]
type addr = bytes_fixed[20]
type key = bytes_fixed[48]
type proof = bytes_fixed[96]
type sig = bytes_fixed[65]

# One output, inline in an output list. Its owners are a run in the
# transaction-wide address array, which is what AddrStart and AddrCount name.
# The last four bytes are the pad that makes the stride eight-aligned; naming
# them is what makes the emitted size the stride the wire uses.
struct Out {
    Asset     id32           @0
    StakeLock u64            @32
    Amount    u64            @40
    Threshold u32            @48
    OwnerLock u64            @52
    AddrStart u32            @60
    AddrCount u32            @64
    Pad       bytes_fixed[4] @68
}

# One input, inline in an input list. Its signatures are a run in the
# transaction-wide index array.
struct In {
    TxID      id32           @0
    Index     u32            @32
    Asset     id32           @36
    StakeLock u64            @68
    Amount    u64            @76
    SigStart  u32            @84
    SigCount  u32            @88
    Pad       bytes_fixed[4] @92
}

# A plain transfer: the envelope and nothing after it.
struct Base {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
}

# The one kind with no envelope: it spends nothing and names the staker whose
# time is up.
struct RewardValidator {
    Kind        u8   @0
    StakerTxID  id32 @1
}

# Bringing value in from another chain.
struct Import {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Source      id32       @77
    Ins         list<In>   @109
    SigIndices  list<u32>  @117
}

# Sending value out to another chain.
struct Export {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Destination id32       @77
    Outs        list<Out>  @109
    OwnerAddrs  list<addr> @117
}

# A new chain on a network this transaction is authorized over.
struct CreateChain {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Chain    id32        @77
    VmID     id32        @109
    Name     bytes       @141
    FxIds    list<id32>  @149
    Genesis  bytes       @157
    Auth     list<u32>   @165
}

# Handing a chain's control to a new owner set.
struct TransferChainOwnership {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Chain     id32       @77
    Auth      list<u32>  @109
    OwnerThreshold u32        @117
    OwnerLocktime  u64        @121
    OwnerAddrs     list<addr> @129
}

# Taking a validator off a permissioned chain.
struct RemoveChainValidator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    NodeID    addr       @77
    Chain     id32       @97
    Auth      list<u32>  @129
}

# A validator joining the primary network, with what it stakes and where
# its reward goes.
struct AddValidator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    NodeID       addr       @77
    Start        u64        @97
    End          u64        @105
    Weight       u64        @113
    StakeOuts  list<Out>  @121
    StakeAddrs list<addr> @129
    RewardsThreshold u32        @137
    RewardsLocktime  u64        @141
    RewardsAddrs     list<addr> @149
    DelegationShares u32  @157
}

# Weight lent to someone else's validator.
struct AddDelegator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    NodeID       addr       @77
    Start        u64        @97
    End          u64        @105
    Weight       u64        @113
    StakeOuts  list<Out>  @121
    StakeAddrs list<addr> @129
    RewardsThreshold u32        @137
    RewardsLocktime  u64        @141
    RewardsAddrs     list<addr> @149
}

# A validator joining a permissioned chain.
struct AddChainValidator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    NodeID       addr       @77
    Start        u64        @97
    End          u64        @105
    Weight       u64        @113
    Chain      id32       @121
    Auth       list<u32>  @153
}

# A validator joining a chain that anyone may join, with the key it will be
# aggregated under and two reward owners.
struct AddPermissionlessValidator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    NodeID       addr       @77
    Start        u64        @97
    End          u64        @105
    Weight       u64        @113
    Chain        id32   @121
    SignerKind   u8     @153
    SignerKey    key    @154
    SignerProof  proof  @202
    StakeOuts    list<Out>  @298
    StakeAddrs   list<addr> @306
    ValidatorRewardsThreshold u32        @314
    ValidatorRewardsLocktime  u64        @318
    ValidatorRewardsAddrs     list<addr> @326
    DelegatorRewardsThreshold u32        @334
    DelegatorRewardsLocktime  u64        @338
    DelegatorRewardsAddrs     list<addr> @346
    DelegationShares u32 @354
}

# Weight lent on a chain that anyone may join.
struct AddPermissionlessDelegator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    NodeID       addr       @77
    Start        u64        @97
    End          u64        @105
    Weight       u64        @113
    Chain      id32       @121
    StakeOuts  list<Out>  @153
    StakeAddrs list<addr> @161
    RewardsThreshold u32        @169
    RewardsLocktime  u64        @173
    RewardsAddrs     list<addr> @181
}

# Paying for an L1 validator's continued registration.
struct IncreaseL1ValidatorBalance {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    ValidationID id32 @77
    Balance      u64  @109
}

# Standing an L1 validator down.
struct DisableL1Validator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    ValidationID id32      @77
    Auth         list<u32> @109
}

# A validator joining an L1, carrying the signed message the L1 said it with
# and the balance that pays for it.
struct RegisterL1Validator {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Balance  u64   @77
    Proof    proof @85
    Message  bytes @181
}

# An L1 restating one of its validators' weights.
struct SetL1ValidatorWeight {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Message bytes @77
}

# Turning a permissioned chain into a staked one: the asset stake is
# denominated in and every threshold measured against it.
struct TransformChain {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Chain                    id32      @77
    Asset                    id32      @109
    InitialSupply            u64       @141
    MaximumSupply            u64       @149
    MinConsumptionRate       u64       @157
    MaxConsumptionRate       u64       @165
    MinValidatorStake        u64       @173
    MaxValidatorStake        u64       @181
    MinStakeDuration         u32       @189
    MaxStakeDuration         u32       @193
    MinDelegationFee         u32       @197
    MinDelegatorStake        u64       @201
    MaxValidatorWeightFactor u8        @209
    UptimeRequirement        u32       @210
    Auth                     list<u32> @214
}

# One validator in a genesis set, inline at a fixed stride. A node id and two
# owner address runs do not fit a fixed slot, so each entry names a run into
# one transaction-wide blob and one transaction-wide address array — the same
# shape the envelope uses for its outputs' owners.
struct NetworkValidator {
    Weight            u64   @0
    Balance           u64   @8
    SignerKey         key   @16
    SignerProof       proof @64
    NodeIDStart       u32   @160
    NodeIDLen         u32   @164
    RemoveThreshold   u32   @168
    RemoveAddrStart   u32   @172
    RemoveAddrCount   u32   @176
    DisableThreshold  u32   @180
    DisableAddrStart  u32   @184
    DisableAddrCount  u32   @188
}

# A new network, its owner set, how it admits validators, and the genesis set
# it starts with.
struct CreateNetwork {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Parent    id32 @77
    OwnerThreshold u32        @109
    OwnerLocktime  u64        @113
    OwnerAddrs     list<addr> @121
    RestakeParent  u8                     @129
    Admission      u8                     @130
    Manager        u8                     @131
    Threshold      u64                    @132
    Validators     list<NetworkValidator> @140
    NodeIDPool     bytes                  @148
    AddrPool       list<addr>             @156
    ManagerChainID id32                   @164
    ManagerAddress bytes                  @196
}

# A network becoming an L1: it names its manager and the validator set that
# runs it from here on.
struct ConvertNetwork {
    Kind         u8         @0
    NetworkID    u32        @1
    BlockchainID id32       @5
    Outs         list<Out>  @37
    OwnerAddrs   list<addr> @45
    Ins          list<In>   @53
    SigIndices   list<u32>  @61
    Memo         bytes      @69
    Network        id32                   @77
    Parent         id32                   @109
    ManagerChainID id32                   @141
    ManagerAddress bytes                  @173
    Validators     list<NetworkValidator> @181
    NodeIDPool     bytes                  @189
    AddrPool       list<addr>             @197
    Auth           list<u32>              @205
    RestakeParent  u8                     @213
    Admission      u8                     @214
    Manager        u8                     @215
    Threshold      u64                    @216
}

# ---- blocks -------------------------------------------------------------
#
# Three shapes, one prefix. A decided block stops at its timestamp, a standard
# block carries the transactions it applies, and a proposal block carries one
# transaction of its own after them. The kind byte says which was written, and
# writing the shorter one is what makes the shorter bytes.

struct Decided {
    Kind   u8   @0
    Parent id32 @1
    Height u64  @33
    Time   u64  @41
}

struct Standard {
    Kind      u8        @0
    Parent    id32      @1
    Height    u64       @33
    Time      u64       @41
    TxLengths list<u32> @49
    TxBlob    bytes     @57
}

struct Proposal {
    Kind       u8        @0
    Parent     id32      @1
    Height     u64       @33
    Time       u64       @41
    TxLengths  list<u32> @49
    TxBlob     bytes     @57
    ProposalTx bytes     @65
}

# ---- credentials --------------------------------------------------------
#
# The signatures a transaction carries, as their own message: one entry per
# credential naming a run in one shared signature array.

struct CredentialRun {
    Start u32 @0
    Count u32 @4
}

struct Credentials {
    Runs       list<CredentialRun> @0
    Signatures list<sig>           @8
}
