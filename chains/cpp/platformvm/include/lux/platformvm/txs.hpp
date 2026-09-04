// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs.hpp — every transaction the P-chain accepts.
//
// Rendered from Go vms/platformvm/txs. THE STRUCT IS THE WIRE: a transaction
// holds its ZAP buffer and reads its fields by offset. There is no codec, no
// marshal step and no second representation that could disagree with the first.
// Construction (new_*) is the ONE place a field becomes bytes; every accessor
// reads them back.
//
// Dispatch is one byte at offset 0 of the root object — the kind. Slot 0 names
// nothing, so a zeroed buffer decodes to no type; slot 1 named a time-advance
// transaction no executor would run and stays a hole, because a number that
// stops naming a type must not let the ones after it shift down.
//
// A signed transaction is `unsigned ‖ credentials`, both self-delimiting ZAP
// messages, so the unsigned bytes are a genuine byte-prefix of the signed bytes
// and the id is sha256 of the whole thing. Nothing is re-encoded to be hashed,
// which is what removes every "which spelling did we sign" question.

#pragma once

#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/priority.hpp"
#include "lux/platformvm/security.hpp"
#include "lux/platformvm/signer.hpp"
#include "lux/platformvm/zap.hpp"

#include <array>
#include <cstdint>
#include <memory>
#include <optional>
#include <span>
#include <string>
#include <vector>

namespace lux::platformvm::txs {

// ── the discriminator, one byte at object offset 0

enum class Kind : std::uint8_t {
    RewardValidator = 2,
    Base = 3,
    Import = 4,
    Export = 5,
    CreateNetwork = 6,
    CreateChain = 7,
    TransferChainOwnership = 8,
    RemoveChainValidator = 9,
    TransformChain = 10,
    AddValidator = 11,
    AddChainValidator = 12,
    AddDelegator = 13,
    AddPermissionlessValidator = 14,
    AddPermissionlessDelegator = 15,
    RegisterL1Validator = 16,
    SetL1ValidatorWeight = 17,
    IncreaseL1ValidatorBalance = 18,
    DisableL1Validator = 19,
    ConvertNetwork = 20,
};

// ── the shared envelope, offsets fixed by the wire

inline constexpr std::int64_t kOffKind = 0;
inline constexpr std::int64_t kOffNetworkId = 1;
inline constexpr std::int64_t kOffBlockchainId = 5;
inline constexpr std::int64_t kOffOuts = 37;
inline constexpr std::int64_t kOffOwnerAddrs = 45;
inline constexpr std::int64_t kOffIns = 53;
inline constexpr std::int64_t kOffSigIndices = 61;
inline constexpr std::int64_t kOffMemo = 69;
inline constexpr std::int64_t kSpendSize = 77;

inline constexpr std::int64_t kValidatorSize = 44;
inline constexpr std::int64_t kBlsPubLen = 48;
inline constexpr std::int64_t kBlsSigLen = 96;
inline constexpr std::int64_t kSignerSize = 1 + kBlsPubLen + kBlsSigLen;  // 145
inline constexpr std::size_t kSigLen = 65;                                // secp256k1 signature

// Names on the blockchain a CreateChainTx spawns.
inline constexpr std::size_t kMaxNameLen = 128;
inline constexpr std::size_t kMaxGenesisLen = 1024 * 1024;
inline constexpr std::size_t kMaxChainAddressLength = 4096;

// ── delta-field value types

// The staking claim a transaction makes: which node, for how long, at what weight.
struct Validator {
    NodeId node_id{};
    std::uint64_t start = 0;
    std::uint64_t end = 0;
    std::uint64_t weight = 0;

    friend bool operator==(const Validator&, const Validator&) = default;

    Status verify() const {
        if (weight == 0) return fail(Err::WeightTooSmallSyntactic);
        return ok();
    }
};

// A staker's window is inside a bound when it starts no earlier and ends no
// later, and is not inverted. Rendered from txs.BoundedBy.
inline bool bounded_by(std::uint64_t staker_start, std::uint64_t staker_end, std::uint64_t lower,
                       std::uint64_t upper) {
    return staker_start >= lower && staker_end <= upper && staker_end >= staker_start;
}

// An authorization is a set of signature indices into the credential that
// accompanies it — the shape Go spells *secp256k1fx.Input.
using Auth = std::vector<std::uint32_t>;

inline Status verify_auth(const Auth& a) {
    for (std::size_t i = 0; i + 1 < a.size(); ++i)
        if (a[i] >= a[i + 1]) return fail(Err::InputIndicesNotSortedUnique);
    return ok();
}

// An owner is the same group an output carries; the P-chain reuses the shape
// wherever a right (to a reward, to a network) needs an owner.
using Owner = OutputOwners;

// A group that may act on an L1 validator's balance or deactivation. Locktime
// is not part of it — that is the whole difference from an output's owner.
struct PChainOwner {
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
    friend bool operator==(const PChainOwner&, const PChainOwner&) = default;

    Status verify() const {
        OutputOwners o{0, threshold, addresses};
        return o.verify();
    }
};

// A genesis validator of a network being created or promoted.
struct NetworkValidator {
    std::vector<std::uint8_t> node_id;
    std::uint64_t weight = 0;
    std::uint64_t balance = 0;
    signer::ProofOfPossession pop{};
    PChainOwner remaining_balance_owner{};
    PChainOwner deactivation_owner{};

    friend bool operator==(const NetworkValidator&, const NetworkValidator&) = default;

    int compare(const NetworkValidator& o) const;
    Status verify() const;
};

// A credential: the signatures authorizing one input or one authorization.
struct Credential {
    std::vector<std::array<std::uint8_t, kSigLen>> sigs;
    friend bool operator==(const Credential&, const Credential&) = default;
};

// ── the transaction types

class Visitor;

// The buffer a transaction is. Shared so an accessor can hand out views without
// the bytes moving underneath them.
using Buffer = std::shared_ptr<const std::vector<std::uint8_t>>;

class UnsignedTx {
  public:
    virtual ~UnsignedTx() = default;

    virtual Kind kind() const = 0;
    virtual Status syntactic_verify(const Runtime& rt) const = 0;
    virtual Status visit(Visitor& v) const = 0;

    std::span<const std::uint8_t> bytes() const {
        if (!buf_) return {};
        return {buf_->data(), buf_->size()};
    }

    // The UTXOs this transaction consumes, by name.
    virtual std::vector<Id> input_ids() const { return {}; }
    // The outputs it produces (the envelope's, not any staked or exported set).
    virtual std::vector<TransferableOutput> outputs() const { return {}; }

  protected:
    explicit UnsignedTx(Buffer b) : buf_(std::move(b)) {}
    zap::Object root() const {
        if (!buf_) return {};
        const auto m = zap::Message::parse({buf_->data(), buf_->size()});
        return m ? m->root() : zap::Object{};
    }
    Buffer buf_;
};

// Everything except the reward proposal carries the spending envelope.
class SpendingTx : public UnsignedTx {
  public:
    std::uint32_t network_id() const { return root().u32(kOffNetworkId); }
    Id blockchain_id() const;
    std::vector<TransferableOutput> outputs() const override;
    std::vector<TransferableInput> inputs() const;
    std::vector<std::uint8_t> memo() const;
    std::vector<Id> input_ids() const override;
    BaseTx base_tx() const;

  protected:
    using UnsignedTx::UnsignedTx;
};

// The bare spending envelope: value moves, nothing else.
class BaseTxUnsigned final : public SpendingTx {
  public:
    static Result<std::shared_ptr<BaseTxUnsigned>> create(const BaseTx& base);
    static std::shared_ptr<BaseTxUnsigned> wrap(Buffer b) {
        return std::shared_ptr<BaseTxUnsigned>(new BaseTxUnsigned(std::move(b)));
    }
    Kind kind() const override { return Kind::Base; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Consumes funds produced on another chain.
class ImportTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffSourceChain = kSpendSize;  // 77
    static constexpr std::int64_t kOffInputs = 109;
    static constexpr std::int64_t kOffSigIdx = 117;
    static constexpr std::int64_t kSize = 125;

    static Result<std::shared_ptr<ImportTx>> create(const BaseTx& base, const Id& source_chain,
                                                    const std::vector<TransferableInput>& imported);
    static std::shared_ptr<ImportTx> wrap(Buffer b) {
        return std::shared_ptr<ImportTx>(new ImportTx(std::move(b)));
    }
    Id source_chain() const;
    std::vector<TransferableInput> imported_inputs() const;
    std::vector<Id> input_utxos() const;
    std::vector<Id> input_ids() const override;

    Kind kind() const override { return Kind::Import; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Sends funds to another chain.
class ExportTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffDestChain = kSpendSize;  // 77
    static constexpr std::int64_t kOffOutputs = 109;
    static constexpr std::int64_t kOffAddrs = 117;
    static constexpr std::int64_t kSize = 125;

    static Result<std::shared_ptr<ExportTx>> create(const BaseTx& base, const Id& destination_chain,
                                                    const std::vector<TransferableOutput>& exported);
    static std::shared_ptr<ExportTx> wrap(Buffer b) {
        return std::shared_ptr<ExportTx>(new ExportTx(std::move(b)));
    }
    Id destination_chain() const;
    std::vector<TransferableOutput> exported_outputs() const;

    Kind kind() const override { return Kind::Export; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The sole network constructor: the ∅→Network birth, at any level of the
// hierarchy. Only Parent differs between an L1, an L2 and an L3.
class CreateNetworkTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffParent = kSpendSize;
    static constexpr std::int64_t kOffOwnerThreshold = kSpendSize + 32;
    static constexpr std::int64_t kOffOwnerLocktime = kSpendSize + 36;
    static constexpr std::int64_t kOffOwnerAddrPtr = kSpendSize + 44;
    static constexpr std::int64_t kOffRestakeParent = kSpendSize + 52;
    static constexpr std::int64_t kOffAdmission = kSpendSize + 53;
    static constexpr std::int64_t kOffManager = kSpendSize + 54;
    static constexpr std::int64_t kOffThreshold = kSpendSize + 55;
    static constexpr std::int64_t kOffValidators = kSpendSize + 63;
    static constexpr std::int64_t kOffValNodeIdPool = kSpendSize + 71;
    static constexpr std::int64_t kOffValAddrPool = kSpendSize + 79;
    static constexpr std::int64_t kOffManagerChainId = kSpendSize + 87;
    static constexpr std::int64_t kOffManagerAddress = kSpendSize + 119;
    static constexpr std::int64_t kSize = kSpendSize + 127;

    static Result<std::shared_ptr<CreateNetworkTx>> create(const BaseTx& base, const Id& parent,
                                                           const Owner& owner, const security::Mode& sec,
                                                           const std::vector<NetworkValidator>& validators,
                                                           const Id& manager_chain_id,
                                                           std::span<const std::uint8_t> manager_address);
    static std::shared_ptr<CreateNetworkTx> wrap(Buffer b) {
        return std::shared_ptr<CreateNetworkTx>(new CreateNetworkTx(std::move(b)));
    }
    Id parent() const;
    Owner owner() const;
    security::Mode security_mode() const;
    bool sovereign() const { return security_mode().sovereign(); }
    std::vector<NetworkValidator> validators() const;
    Id manager_chain_id() const;
    std::vector<std::uint8_t> manager_address() const;

    Kind kind() const override { return Kind::CreateNetwork; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Promotes an existing network: inherited security → sovereign, re-anchoring
// its parent. The endomorphism Network → Network.
class ConvertNetworkTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffNetwork = kSpendSize;
    static constexpr std::int64_t kOffParent = kSpendSize + 32;
    static constexpr std::int64_t kOffManagerChainId = kSpendSize + 64;
    static constexpr std::int64_t kOffManagerAddress = kSpendSize + 96;
    static constexpr std::int64_t kOffValidators = kSpendSize + 104;
    static constexpr std::int64_t kOffValNodeIdPool = kSpendSize + 112;
    static constexpr std::int64_t kOffValAddrPool = kSpendSize + 120;
    static constexpr std::int64_t kOffAuthPtr = kSpendSize + 128;
    static constexpr std::int64_t kOffRestakeParent = kSpendSize + 136;
    static constexpr std::int64_t kOffAdmission = kSpendSize + 137;
    static constexpr std::int64_t kOffManager = kSpendSize + 138;
    static constexpr std::int64_t kOffThreshold = kSpendSize + 139;
    static constexpr std::int64_t kSize = kSpendSize + 147;

    static Result<std::shared_ptr<ConvertNetworkTx>> create(
        const BaseTx& base, const Id& network, const Id& parent, const Id& manager_chain_id,
        const security::Mode& sec, std::span<const std::uint8_t> manager_address,
        const std::vector<NetworkValidator>& validators, const Auth& auth);
    static std::shared_ptr<ConvertNetworkTx> wrap(Buffer b) {
        return std::shared_ptr<ConvertNetworkTx>(new ConvertNetworkTx(std::move(b)));
    }
    Id network() const;
    Id parent() const;
    Id manager_chain_id() const;
    std::vector<std::uint8_t> manager_address() const;
    std::vector<NetworkValidator> validators() const;
    Auth auth() const;
    security::Mode security_mode() const;
    bool sovereign() const { return security_mode().sovereign(); }

    Kind kind() const override { return Kind::ConvertNetwork; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Creates a blockchain on a network. The sole chain constructor.
class CreateChainTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffChainId = kSpendSize;  // 77
    static constexpr std::int64_t kOffVmId = 109;
    static constexpr std::int64_t kOffName = 141;
    static constexpr std::int64_t kOffFxIds = 149;
    static constexpr std::int64_t kOffGenesis = 157;
    static constexpr std::int64_t kOffAuth = 165;
    static constexpr std::int64_t kSize = 173;

    static Result<std::shared_ptr<CreateChainTx>> create(const BaseTx& base, const Id& chain_id,
                                                          const std::string& blockchain_name, const Id& vm_id,
                                                          const std::vector<Id>& fx_ids,
                                                          std::span<const std::uint8_t> genesis_data,
                                                          const Auth& chain_auth);
    static std::shared_ptr<CreateChainTx> wrap(Buffer b) {
        return std::shared_ptr<CreateChainTx>(new CreateChainTx(std::move(b)));
    }
    Id chain_id() const;
    Id vm_id() const;
    std::string blockchain_name() const;
    std::vector<Id> fx_ids() const;
    std::vector<std::uint8_t> genesis_data() const;
    Auth chain_auth() const;

    Kind kind() const override { return Kind::CreateChain; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Re-owns a network.
class TransferChainOwnershipTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffChain = kSpendSize;  // 77
    static constexpr std::int64_t kOffChainAuth = 109;
    static constexpr std::int64_t kOffOwnerThreshold = 117;
    static constexpr std::int64_t kOffOwnerLocktime = 121;
    static constexpr std::int64_t kOffOwnerAddrs = 129;
    static constexpr std::int64_t kSize = 137;

    static Result<std::shared_ptr<TransferChainOwnershipTx>> create(const BaseTx& base, const Id& chain,
                                                                     const Auth& chain_auth,
                                                                     const Owner& owner);
    static std::shared_ptr<TransferChainOwnershipTx> wrap(Buffer b) {
        return std::shared_ptr<TransferChainOwnershipTx>(new TransferChainOwnershipTx(std::move(b)));
    }
    Id chain() const;
    Auth chain_auth() const;
    Owner owner() const;

    Kind kind() const override { return Kind::TransferChainOwnership; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Removes a permissioned validator from a network.
class RemoveChainValidatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffNodeId = kSpendSize;  // 77
    static constexpr std::int64_t kOffChain = 97;
    static constexpr std::int64_t kOffChainAuth = 129;
    static constexpr std::int64_t kSize = 137;

    static Result<std::shared_ptr<RemoveChainValidatorTx>> create(const BaseTx& base, const NodeId& node_id,
                                                                   const Id& chain, const Auth& chain_auth);
    static std::shared_ptr<RemoveChainValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<RemoveChainValidatorTx>(new RemoveChainValidatorTx(std::move(b)));
    }
    NodeId node_id() const;
    Id chain() const;
    Auth chain_auth() const;

    Kind kind() const override { return Kind::RemoveChainValidator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Transforms a network into a permissionless one.
class TransformChainTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffChain = kSpendSize;  // 77
    static constexpr std::int64_t kOffAssetId = 109;
    static constexpr std::int64_t kOffInitialSupply = 141;
    static constexpr std::int64_t kOffMaximumSupply = 149;
    static constexpr std::int64_t kOffMinConsumptionRate = 157;
    static constexpr std::int64_t kOffMaxConsumptionRate = 165;
    static constexpr std::int64_t kOffMinValidatorStake = 173;
    static constexpr std::int64_t kOffMaxValidatorStake = 181;
    static constexpr std::int64_t kOffMinStakeDuration = 189;
    static constexpr std::int64_t kOffMaxStakeDuration = 193;
    static constexpr std::int64_t kOffMinDelegationFee = 197;
    static constexpr std::int64_t kOffMinDelegatorStake = 201;
    static constexpr std::int64_t kOffMaxValidatorWeightFactor = 209;
    static constexpr std::int64_t kOffUptimeRequirement = 210;
    static constexpr std::int64_t kOffChainAuth = 214;
    static constexpr std::int64_t kSize = 222;

    struct Params {
        Id chain{};
        Id asset_id{};
        std::uint64_t initial_supply = 0;
        std::uint64_t maximum_supply = 0;
        std::uint64_t min_consumption_rate = 0;
        std::uint64_t max_consumption_rate = 0;
        std::uint64_t min_validator_stake = 0;
        std::uint64_t max_validator_stake = 0;
        std::uint32_t min_stake_duration = 0;
        std::uint32_t max_stake_duration = 0;
        std::uint32_t min_delegation_fee = 0;
        std::uint64_t min_delegator_stake = 0;
        std::uint8_t max_validator_weight_factor = 0;
        std::uint32_t uptime_requirement = 0;
    };

    static Result<std::shared_ptr<TransformChainTx>> create(const BaseTx& base, const Params& p,
                                                             const Auth& chain_auth);
    static std::shared_ptr<TransformChainTx> wrap(Buffer b) {
        return std::shared_ptr<TransformChainTx>(new TransformChainTx(std::move(b)));
    }
    Id chain() const;
    Id asset_id() const;
    std::uint64_t initial_supply() const;
    std::uint64_t maximum_supply() const;
    std::uint64_t min_consumption_rate() const;
    std::uint64_t max_consumption_rate() const;
    std::uint64_t min_validator_stake() const;
    std::uint64_t max_validator_stake() const;
    std::uint32_t min_stake_duration() const;
    std::uint32_t max_stake_duration() const;
    std::uint32_t min_delegation_fee() const;
    std::uint64_t min_delegator_stake() const;
    std::uint8_t max_validator_weight_factor() const;
    std::uint32_t uptime_requirement() const;
    Auth chain_auth() const;

    Kind kind() const override { return Kind::TransformChain; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The legacy primary-network validator entry. Retained so pre-LP-018 history
// still decodes and replays; the executor refuses to run it.
class AddValidatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidator = kSpendSize;                    // 77
    static constexpr std::int64_t kOffStakeOuts = kOffValidator + kValidatorSize; // 121
    static constexpr std::int64_t kOffStakeAddrs = kOffStakeOuts + 8;             // 129
    static constexpr std::int64_t kOffRewardsThreshold = kOffStakeAddrs + 8;      // 137
    static constexpr std::int64_t kOffRewardsLocktime = kOffRewardsThreshold + 4; // 141
    static constexpr std::int64_t kOffRewardsAddrs = kOffRewardsLocktime + 8;     // 149
    static constexpr std::int64_t kOffDelegationShares = kOffRewardsAddrs + 8;    // 157
    static constexpr std::int64_t kSize = kOffDelegationShares + 4;               // 161

    static Result<std::shared_ptr<AddValidatorTx>> create(const BaseTx& base, const Validator& validator,
                                                           const std::vector<TransferableOutput>& stake_outs,
                                                           const Owner& rewards_owner,
                                                           std::uint32_t delegation_shares);
    static std::shared_ptr<AddValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<AddValidatorTx>(new AddValidatorTx(std::move(b)));
    }
    Validator validator() const;
    std::vector<TransferableOutput> stake_outs() const;
    Owner rewards_owner() const;
    std::uint32_t delegation_shares() const;

    Id chain_id() const { return kPrimaryNetworkId; }
    NodeId node_id() const { return validator().node_id; }
    std::uint64_t start_time() const { return validator().start; }
    std::uint64_t end_time() const { return validator().end; }
    std::uint64_t weight() const { return validator().weight; }
    Priority pending_priority() const { return Priority::PrimaryNetworkValidatorPending; }
    Priority current_priority() const { return Priority::PrimaryNetworkValidatorCurrent; }

    Kind kind() const override { return Kind::AddValidator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The legacy primary-network delegation entry. Same status as AddValidatorTx.
class AddDelegatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidator = kSpendSize;                    // 77
    static constexpr std::int64_t kOffStakeOuts = kOffValidator + kValidatorSize; // 121
    static constexpr std::int64_t kOffStakeAddrs = kOffStakeOuts + 8;             // 129
    static constexpr std::int64_t kOffRewardsThreshold = kOffStakeAddrs + 8;      // 137
    static constexpr std::int64_t kOffRewardsLocktime = kOffRewardsThreshold + 4; // 141
    static constexpr std::int64_t kOffRewardsAddrs = kOffRewardsLocktime + 8;     // 149
    static constexpr std::int64_t kSize = kOffRewardsAddrs + 8;                   // 157

    static Result<std::shared_ptr<AddDelegatorTx>> create(const BaseTx& base, const Validator& validator,
                                                           const std::vector<TransferableOutput>& stake_outs,
                                                           const Owner& rewards_owner);
    static std::shared_ptr<AddDelegatorTx> wrap(Buffer b) {
        return std::shared_ptr<AddDelegatorTx>(new AddDelegatorTx(std::move(b)));
    }
    Validator validator() const;
    std::vector<TransferableOutput> stake_outs() const;
    Owner delegation_rewards_owner() const;

    Id chain_id() const { return kPrimaryNetworkId; }
    NodeId node_id() const { return validator().node_id; }
    std::uint64_t start_time() const { return validator().start; }
    std::uint64_t end_time() const { return validator().end; }
    std::uint64_t weight() const { return validator().weight; }
    Priority pending_priority() const { return Priority::PrimaryNetworkDelegatorLegacyPending; }
    Priority current_priority() const { return Priority::PrimaryNetworkDelegatorCurrent; }

    Kind kind() const override { return Kind::AddDelegator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The legacy per-chain (permissioned) validator registration.
class AddChainValidatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidator = kSpendSize;                     // 77
    static constexpr std::int64_t kOffChain = kOffValidator + kValidatorSize;      // 121
    static constexpr std::int64_t kOffChainAuth = kOffChain + 32;                  // 153
    static constexpr std::int64_t kSize = kOffChainAuth + 8;                       // 161

    static Result<std::shared_ptr<AddChainValidatorTx>> create(const BaseTx& base, const Validator& validator,
                                                                const Id& chain, const Auth& chain_auth);
    static std::shared_ptr<AddChainValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<AddChainValidatorTx>(new AddChainValidatorTx(std::move(b)));
    }
    Validator validator() const;
    Id chain() const;
    Auth chain_auth() const;

    Id chain_id() const { return chain(); }
    NodeId node_id() const { return validator().node_id; }
    std::uint64_t start_time() const { return validator().start; }
    std::uint64_t end_time() const { return validator().end; }
    std::uint64_t weight() const { return validator().weight; }
    Priority pending_priority() const { return Priority::ChainPermissionedValidatorPending; }
    Priority current_priority() const { return Priority::ChainPermissionedValidatorCurrent; }

    Kind kind() const override { return Kind::AddChainValidator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The permissionless validator entry — the transaction that makes this a public
// good. No allowlist, no admin key: a node joins by staking.
class AddPermissionlessValidatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidator = kSpendSize;                          // 77
    static constexpr std::int64_t kOffChain = kOffValidator + kValidatorSize;           // 121
    static constexpr std::int64_t kOffSigner = kOffChain + 32;                          // 153
    static constexpr std::int64_t kOffStakeOuts = kOffSigner + kSignerSize;             // 298
    static constexpr std::int64_t kOffStakeAddrs = kOffStakeOuts + 8;                   // 306
    static constexpr std::int64_t kOffValRewardsThreshold = kOffStakeAddrs + 8;         // 314
    static constexpr std::int64_t kOffValRewardsLocktime = kOffValRewardsThreshold + 4; // 318
    static constexpr std::int64_t kOffValRewardsAddrs = kOffValRewardsLocktime + 8;     // 326
    static constexpr std::int64_t kOffDelRewardsThreshold = kOffValRewardsAddrs + 8;    // 334
    static constexpr std::int64_t kOffDelRewardsLocktime = kOffDelRewardsThreshold + 4; // 338
    static constexpr std::int64_t kOffDelRewardsAddrs = kOffDelRewardsLocktime + 8;     // 346
    static constexpr std::int64_t kOffDelegationShares = kOffDelRewardsAddrs + 8;       // 354
    static constexpr std::int64_t kSize = kOffDelegationShares + 4;                     // 358

    static Result<std::shared_ptr<AddPermissionlessValidatorTx>> create(
        const BaseTx& base, const Validator& validator, const Id& chain, const signer::Signer& sig,
        const std::vector<TransferableOutput>& stake_outs, const Owner& validator_rewards_owner,
        const Owner& delegator_rewards_owner, std::uint32_t delegation_shares);
    static std::shared_ptr<AddPermissionlessValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<AddPermissionlessValidatorTx>(new AddPermissionlessValidatorTx(std::move(b)));
    }
    Validator validator() const;
    Id chain() const;
    signer::Signer signer_value() const;
    std::vector<TransferableOutput> stake_outs() const;
    Owner validator_rewards_owner() const;
    Owner delegator_rewards_owner() const;
    std::uint32_t delegation_shares() const;

    Id chain_id() const { return chain(); }
    NodeId node_id() const { return validator().node_id; }
    std::uint64_t start_time() const { return validator().start; }
    std::uint64_t end_time() const { return validator().end; }
    std::uint64_t weight() const { return validator().weight; }
    // The registered key, once the proof of possession has been verified.
    Result<std::optional<signer::PublicKeyBytes>> public_key() const;
    Priority pending_priority() const;
    Priority current_priority() const;

    Kind kind() const override { return Kind::AddPermissionlessValidator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The permissionless delegation entry.
class AddPermissionlessDelegatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidator = kSpendSize;                     // 77
    static constexpr std::int64_t kOffChain = kOffValidator + kValidatorSize;      // 121
    static constexpr std::int64_t kOffStakeOuts = kOffChain + 32;                  // 153
    static constexpr std::int64_t kOffStakeAddrs = kOffStakeOuts + 8;              // 161
    static constexpr std::int64_t kOffRewardsThreshold = kOffStakeAddrs + 8;       // 169
    static constexpr std::int64_t kOffRewardsLocktime = kOffRewardsThreshold + 4;  // 173
    static constexpr std::int64_t kOffRewardsAddrs = kOffRewardsLocktime + 8;      // 181
    static constexpr std::int64_t kSize = kOffRewardsAddrs + 8;                    // 189

    static Result<std::shared_ptr<AddPermissionlessDelegatorTx>> create(
        const BaseTx& base, const Validator& validator, const Id& chain,
        const std::vector<TransferableOutput>& stake_outs, const Owner& delegation_rewards_owner);
    static std::shared_ptr<AddPermissionlessDelegatorTx> wrap(Buffer b) {
        return std::shared_ptr<AddPermissionlessDelegatorTx>(new AddPermissionlessDelegatorTx(std::move(b)));
    }
    Validator validator() const;
    Id chain() const;
    std::vector<TransferableOutput> stake_outs() const;
    Owner delegation_rewards_owner() const;

    Id chain_id() const { return chain(); }
    NodeId node_id() const { return validator().node_id; }
    std::uint64_t start_time() const { return validator().start; }
    std::uint64_t end_time() const { return validator().end; }
    std::uint64_t weight() const { return validator().weight; }
    Priority pending_priority() const;
    Priority current_priority() const;

    Kind kind() const override { return Kind::AddPermissionlessDelegator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Registers an L1 validator from a signed cross-chain message.
class RegisterL1ValidatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffBalance = kSpendSize;  // 77
    static constexpr std::int64_t kOffPop = 85;
    static constexpr std::int64_t kOffMessage = 181;
    static constexpr std::int64_t kSize = 189;

    static Result<std::shared_ptr<RegisterL1ValidatorTx>> create(
        const BaseTx& base, std::uint64_t balance, const signer::SignatureBytes& proof_of_possession,
        std::span<const std::uint8_t> message);
    static std::shared_ptr<RegisterL1ValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<RegisterL1ValidatorTx>(new RegisterL1ValidatorTx(std::move(b)));
    }
    std::uint64_t balance() const;
    signer::SignatureBytes proof_of_possession() const;
    std::vector<std::uint8_t> message() const;

    Kind kind() const override { return Kind::RegisterL1Validator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Sets an L1 validator's weight from a signed cross-chain message.
class SetL1ValidatorWeightTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffMessage = kSpendSize;  // 77
    static constexpr std::int64_t kSize = 85;

    static Result<std::shared_ptr<SetL1ValidatorWeightTx>> create(const BaseTx& base,
                                                                   std::span<const std::uint8_t> message);
    static std::shared_ptr<SetL1ValidatorWeightTx> wrap(Buffer b) {
        return std::shared_ptr<SetL1ValidatorWeightTx>(new SetL1ValidatorWeightTx(std::move(b)));
    }
    std::vector<std::uint8_t> message() const;

    Kind kind() const override { return Kind::SetL1ValidatorWeight; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Tops up an L1 validator's continuous-fee balance.
class IncreaseL1ValidatorBalanceTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidationId = kSpendSize;  // 77
    static constexpr std::int64_t kOffBalance = 109;
    static constexpr std::int64_t kSize = 117;

    static Result<std::shared_ptr<IncreaseL1ValidatorBalanceTx>> create(const BaseTx& base,
                                                                         const Id& validation_id,
                                                                         std::uint64_t balance);
    static std::shared_ptr<IncreaseL1ValidatorBalanceTx> wrap(Buffer b) {
        return std::shared_ptr<IncreaseL1ValidatorBalanceTx>(new IncreaseL1ValidatorBalanceTx(std::move(b)));
    }
    Id validation_id() const;
    std::uint64_t balance() const;

    Kind kind() const override { return Kind::IncreaseL1ValidatorBalance; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// Disables an L1 validator, proving authorization with a credential.
class DisableL1ValidatorTx final : public SpendingTx {
  public:
    static constexpr std::int64_t kOffValidationId = kSpendSize;  // 77
    static constexpr std::int64_t kOffAuth = 109;
    static constexpr std::int64_t kSize = 117;

    static Result<std::shared_ptr<DisableL1ValidatorTx>> create(const BaseTx& base, const Id& validation_id,
                                                                 const Auth& disable_auth);
    static std::shared_ptr<DisableL1ValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<DisableL1ValidatorTx>(new DisableL1ValidatorTx(std::move(b)));
    }
    Id validation_id() const;
    Auth disable_auth() const;

    Kind kind() const override { return Kind::DisableL1Validator; }
    Status syntactic_verify(const Runtime& rt) const override;
    Status visit(Visitor& v) const override;

  private:
    using SpendingTx::SpendingTx;
};

// The proposal to remove a validator that has finished, and pay it. Accepted
// with a commit block the staker is rewarded; with an abort block it is not.
class RewardValidatorTx final : public UnsignedTx {
  public:
    static constexpr std::int64_t kOffTxId = 1;  // 32B, after kind@0
    static constexpr std::int64_t kSize = 33;

    static std::shared_ptr<RewardValidatorTx> create(const Id& tx_id);
    static std::shared_ptr<RewardValidatorTx> wrap(Buffer b) {
        return std::shared_ptr<RewardValidatorTx>(new RewardValidatorTx(std::move(b)));
    }
    Id tx_id() const;

    Kind kind() const override { return Kind::RewardValidator; }
    Status syntactic_verify(const Runtime&) const override { return ok(); }
    Status visit(Visitor& v) const override;

  private:
    using UnsignedTx::UnsignedTx;
};

// ── the visitor: one arm per kind, so a new transaction cannot be forgotten

class Visitor {
  public:
    virtual ~Visitor() = default;
    virtual Status base_tx(const BaseTxUnsigned&) = 0;
    virtual Status import_tx(const ImportTx&) = 0;
    virtual Status export_tx(const ExportTx&) = 0;
    virtual Status create_network_tx(const CreateNetworkTx&) = 0;
    virtual Status convert_network_tx(const ConvertNetworkTx&) = 0;
    virtual Status create_chain_tx(const CreateChainTx&) = 0;
    virtual Status transfer_chain_ownership_tx(const TransferChainOwnershipTx&) = 0;
    virtual Status remove_chain_validator_tx(const RemoveChainValidatorTx&) = 0;
    virtual Status transform_chain_tx(const TransformChainTx&) = 0;
    virtual Status add_validator_tx(const AddValidatorTx&) = 0;
    virtual Status add_delegator_tx(const AddDelegatorTx&) = 0;
    virtual Status add_chain_validator_tx(const AddChainValidatorTx&) = 0;
    virtual Status add_permissionless_validator_tx(const AddPermissionlessValidatorTx&) = 0;
    virtual Status add_permissionless_delegator_tx(const AddPermissionlessDelegatorTx&) = 0;
    virtual Status register_l1_validator_tx(const RegisterL1ValidatorTx&) = 0;
    virtual Status set_l1_validator_weight_tx(const SetL1ValidatorWeightTx&) = 0;
    virtual Status increase_l1_validator_balance_tx(const IncreaseL1ValidatorBalanceTx&) = 0;
    virtual Status disable_l1_validator_tx(const DisableL1ValidatorTx&) = 0;
    virtual Status reward_validator_tx(const RewardValidatorTx&) = 0;
};

// ── the staker views, for the transactions that put weight on a set

// A staker: the shape state records when a transaction admits one.
struct StakerView {
    Id chain_id{};
    NodeId node_id{};
    std::optional<signer::PublicKeyBytes> public_key;
    std::uint64_t start = 0;
    std::uint64_t end = 0;
    std::uint64_t weight = 0;
    Priority current_priority = Priority::PrimaryNetworkValidatorCurrent;
    Priority pending_priority = Priority::PrimaryNetworkValidatorPending;
    bool scheduled = false;  // has a start time of its own
};

// The staker a transaction describes, if it describes one. This is the ONE
// place "is this transaction a staker" is answered.
Result<std::optional<StakerView>> staker_of(const UnsignedTx& tx);

// The stake a transaction locks, if it locks any.
std::vector<TransferableOutput> stake_of(const UnsignedTx& tx);

// ── the signed transaction

struct Tx {
    std::shared_ptr<UnsignedTx> unsigned_tx;
    std::vector<Credential> creds;
    Id tx_id{};
    std::vector<std::uint8_t> bytes;

    // Bind the cached bytes and id from the unsigned buffer plus the
    // credentials. Used for transactions built in process.
    Status initialize();
    void set_bytes(std::vector<std::uint8_t> signed_bytes);
    std::size_t size() const { return bytes.size(); }
    Id id() const { return tx_id; }

    // The UTXOs this transaction produces.
    std::vector<UTXO> utxos() const;
    std::vector<Id> input_ids() const;
    Status syntactic_verify(const Runtime& rt) const;
};

// Wrap signed bytes: the leading self-delimiting message is the unsigned body,
// any remainder is the credential buffer. The id is sha256 of the whole buffer,
// with nothing re-encoded.
Result<Tx> parse(std::span<const std::uint8_t> signed_bytes);

// The credential buffer, encoded and decoded. Exposed because a signer builds
// one and the parser reads one, and both must agree exactly.
Result<std::vector<std::uint8_t>> write_creds(const std::vector<Credential>& creds);
Result<std::vector<Credential>> parse_creds(std::span<const std::uint8_t> b);

// The ONE canonical byte encoding of an owner, lifted out of a transaction into
// a standalone buffer. Used wherever a stable owner identity is needed off the
// tx wire — the L1 balance and deactivation owners state records, for one.
std::vector<std::uint8_t> marshal_owner(const Owner& o);
Result<Owner> unmarshal_owner(std::span<const std::uint8_t> b);

}  // namespace lux::platformvm::txs
