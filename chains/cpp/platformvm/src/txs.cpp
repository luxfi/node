// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs.cpp — construction and validation for every P-chain transaction.
//
// Rendered from Go vms/platformvm/txs, one function per file there. The order of
// the refusals inside each syntactic_verify is itself part of the port: a
// transaction that is wrong in two ways must be reported wrong for the same
// reason on both implementations, or the two disagree about which of two
// invalid transactions is which.

#include "lux/platformvm/txs.hpp"

#include "lux/platformvm/reward.hpp"
#include "lux/platformvm/txs_wire.hpp"

#include <algorithm>
#include <cstring>

namespace lux::platformvm::txs {
namespace {

Buffer own(std::vector<std::uint8_t> v) {
    return std::make_shared<const std::vector<std::uint8_t>>(std::move(v));
}

bool parses(const Buffer& buf) {
    return zap::Message::parse({buf->data(), buf->size()}).has_value();
}

// The eight fields every spending transaction opens with, filled once. They
// are the same fields at the same offsets in every kind, which is why the
// envelope of any of them reads through wire::Base.
template <class T>
void envelope(T& in, Kind k, const BaseTx& base, wire::Spend s) {
    in.Kind = static_cast<std::uint8_t>(k);
    in.NetworkID = base.network_id;
    in.BlockchainID = base.blockchain_id.b;
    in.Outs = std::move(s.outs);
    in.OwnerAddrs = std::move(s.owner_addrs);
    in.Ins = std::move(s.ins);
    in.SigIndices = std::move(s.sig_indices);
    in.Memo = {base.memo.data(), base.memo.size()};
}

// The shared envelope checks every spending transaction composes over its own
// fields: metadata, per-output and per-input verification, canonical ordering.
Status verify_base_tx(const BaseTx& base, const Runtime& rt) {
    if (auto s = base.verify(rt); !s) return s;
    for (const auto& out : base.outs)
        if (auto s = out.verify(); !s) return s;
    for (const auto& in : base.ins)
        if (auto s = in.verify(); !s) return s;
    if (!outputs_sorted(base.outs)) return fail(Err::OutputsNotSorted);
    if (!inputs_sorted_unique(base.ins)) return fail(Err::InputsNotSortedUnique);
    return ok();
}

bool ascii_name_char(std::uint8_t c) {
    if (c >= 0x80) return false;  // a rune past ASCII, or invalid UTF-8
    return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ';
}

}  // namespace

// ── NetworkValidator

int NetworkValidator::compare(const NetworkValidator& o) const {
    const std::size_t n = std::min(node_id.size(), o.node_id.size());
    if (n > 0) {
        const int r = std::memcmp(node_id.data(), o.node_id.data(), n);
        if (r != 0) return r < 0 ? -1 : 1;
    }
    if (node_id.size() == o.node_id.size()) return 0;
    return node_id.size() < o.node_id.size() ? -1 : 1;
}

Status NetworkValidator::verify() const {
    if (weight == 0) return fail(Err::ZeroWeight);
    if (node_id.size() != kNodeIdLen) return fail(Err::InvalidNodeIDLength);
    if (NodeId::from(node_id) == kEmptyNodeId) return fail(Err::EmptyNodeID);
    if (auto s = pop.verify(); !s) return s;
    if (auto s = remaining_balance_owner.verify(); !s) return s;
    return deactivation_owner.verify();
}

// ── SpendingTx: the shared envelope surface

Id SpendingTx::blockchain_id() const { return Id::from(wire::Base(root()).BlockchainID()); }

std::vector<TransferableOutput> SpendingTx::outputs() const {
    const wire::Base e(root());
    return wire::outs(e.Outs(), e.OwnerAddrs());
}

std::vector<TransferableInput> SpendingTx::inputs() const {
    const wire::Base e(root());
    return wire::ins(e.Ins(), e.SigIndices());
}

std::vector<std::uint8_t> SpendingTx::memo() const {
    const auto m = wire::Base(root()).Memo();
    return std::vector<std::uint8_t>(m.begin(), m.end());
}

std::vector<Id> SpendingTx::input_ids() const {
    std::vector<Id> out;
    for (const auto& in : inputs()) out.push_back(in.input_id());
    return out;
}

BaseTx SpendingTx::base_tx() const {
    const wire::Base e(root());
    BaseTx b;
    b.network_id = e.NetworkID();
    b.blockchain_id = Id::from(e.BlockchainID());
    b.outs = wire::outs(e.Outs(), e.OwnerAddrs());
    b.ins = wire::ins(e.Ins(), e.SigIndices());
    b.memo = memo();
    return b;
}

// ── BaseTx

Result<std::shared_ptr<BaseTxUnsigned>> BaseTxUnsigned::create(const BaseTx& base) {
    wire::BaseInput in;
    envelope(in, Kind::Base, base, wire::spend(base));
    auto buf = own(wire::NewBase(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Status BaseTxUnsigned::syntactic_verify(const Runtime& rt) const { return verify_base_tx(base_tx(), rt); }
Status BaseTxUnsigned::visit(Visitor& v) const { return v.base_tx(*this); }

// ── ImportTx

Result<std::shared_ptr<ImportTx>> ImportTx::create(const BaseTx& base, const Id& source_chain,
                                                   const std::vector<TransferableInput>& imported) {
    wire::ImportInput in;
    envelope(in, Kind::Import, base, wire::spend(base));
    in.Source = source_chain.b;
    auto extra = wire::ins(imported);
    in.Imported = std::move(extra.list);
    in.ImportedSigs = std::move(extra.sigs);
    auto buf = own(wire::NewImport(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id ImportTx::source_chain() const { return Id::from(wire::Import(root()).Source()); }

std::vector<TransferableInput> ImportTx::imported_inputs() const {
    const wire::Import t(root());
    return wire::ins(t.Imported(), t.ImportedSigs());
}

std::vector<Id> ImportTx::input_utxos() const {
    std::vector<Id> out;
    for (const auto& in : imported_inputs()) out.push_back(in.input_id());
    return out;
}

std::vector<Id> ImportTx::input_ids() const {
    auto out = SpendingTx::input_ids();
    for (const auto& id : input_utxos())
        if (std::find(out.begin(), out.end(), id) == out.end()) out.push_back(id);
    return out;
}

Status ImportTx::syntactic_verify(const Runtime& rt) const {
    const auto ins = imported_inputs();
    if (ins.empty()) return fail(Err::NoImportInputs);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    for (const auto& in : ins)
        if (auto s = in.verify(); !s) return s;
    if (!inputs_sorted_unique(ins)) return fail(Err::InputsNotSortedUnique);
    return ok();
}

Status ImportTx::visit(Visitor& v) const { return v.import_tx(*this); }

// ── ExportTx

Result<std::shared_ptr<ExportTx>> ExportTx::create(const BaseTx& base, const Id& destination_chain,
                                                   const std::vector<TransferableOutput>& exported) {
    wire::ExportInput in;
    envelope(in, Kind::Export, base, wire::spend(base));
    in.Destination = destination_chain.b;
    auto extra = wire::outs(exported);
    in.Exported = std::move(extra.list);
    in.ExportedAddrs = std::move(extra.addrs);
    auto buf = own(wire::NewExport(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id ExportTx::destination_chain() const { return Id::from(wire::Export(root()).Destination()); }

std::vector<TransferableOutput> ExportTx::exported_outputs() const {
    const wire::Export t(root());
    return wire::outs(t.Exported(), t.ExportedAddrs());
}

Status ExportTx::syntactic_verify(const Runtime& rt) const {
    const auto outs = exported_outputs();
    if (outs.empty()) return fail(Err::NoExportOutputs);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    for (const auto& out : outs) {
        if (auto s = out.verify(); !s) return s;
        // An exported output may not carry a stake lock: the destination chain
        // has no notion of this chain's staking time.
        if (out.locked()) return fail(Err::WrongLocktime);
    }
    if (!outputs_sorted(outs)) return fail(Err::OutputsNotSorted);
    return ok();
}

Status ExportTx::visit(Visitor& v) const { return v.export_tx(*this); }

// ── CreateNetworkTx

Result<std::shared_ptr<CreateNetworkTx>> CreateNetworkTx::create(
    const BaseTx& base, const Id& parent, const Owner& owner, const security::Mode& sec,
    const std::vector<NetworkValidator>& validators, const Id& manager_chain_id,
    std::span<const std::uint8_t> manager_address) {
    wire::CreateNetworkInput in;
    envelope(in, Kind::CreateNetwork, base, wire::spend(base));
    in.Parent = parent.b;
    in.OwnerThreshold = owner.threshold;
    in.OwnerLocktime = owner.locktime;
    in.NewOwnerAddrs = wire::addrs(owner.addrs);
    in.RestakeParent = sec.restake_parent ? 1 : 0;
    in.Admission = static_cast<std::uint8_t>(sec.admission);
    in.Manager = static_cast<std::uint8_t>(sec.manager);
    in.Threshold = sec.threshold;
    auto vp = wire::validators(validators);
    in.Validators = std::move(vp.list);
    in.NodeIDPool = {vp.node_ids.data(), vp.node_ids.size()};
    in.AddrPool = std::move(vp.addrs);
    in.ManagerChainID = manager_chain_id.b;
    in.ManagerAddress = manager_address;
    auto buf = own(wire::NewCreateNetwork(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id CreateNetworkTx::parent() const { return Id::from(wire::CreateNetwork(root()).Parent()); }
Owner CreateNetworkTx::owner() const {
    const wire::CreateNetwork t(root());
    return wire::owner(t.OwnerThreshold(), t.OwnerLocktime(), t.NewOwnerAddrs());
}
security::Mode CreateNetworkTx::security_mode() const {
    const wire::CreateNetwork t(root());
    security::Mode m;
    m.restake_parent = t.RestakeParent() != 0;
    m.admission = static_cast<security::Admission>(t.Admission());
    m.manager = static_cast<security::Manager>(t.Manager());
    m.threshold = t.Threshold();
    return m;
}
std::vector<NetworkValidator> CreateNetworkTx::validators() const {
    const wire::CreateNetwork t(root());
    return wire::validators(t.Validators(), t.NodeIDPool(), t.AddrPool());
}
Id CreateNetworkTx::manager_chain_id() const { return Id::from(wire::CreateNetwork(root()).ManagerChainID()); }
std::vector<std::uint8_t> CreateNetworkTx::manager_address() const {
    const auto a = wire::CreateNetwork(root()).ManagerAddress();
    return std::vector<std::uint8_t>(a.begin(), a.end());
}

Status CreateNetworkTx::syntactic_verify(const Runtime& rt) const {
    const auto vdrs = validators();
    const auto sec = security_mode();
    if (auto s = sec.valid(); !s) return s;
    const auto addr = manager_address();
    if (!sec.sovereign() && !vdrs.empty()) return fail(Err::NoOwnSetButHasValidators);
    if (!sec.restake_parent && vdrs.empty()) return fail(Err::OwnSetMustIncludeValidator);
    if (sec.sovereign() && sec.manager == security::Manager::Contract && addr.empty())
        return fail(Err::ContractManagerNeedsAddress);
    for (std::size_t i = 0; i + 1 < vdrs.size(); ++i)
        if (vdrs[i].compare(vdrs[i + 1]) >= 0) return fail(Err::ValidatorsNotSortedAndUnique);
    if (addr.size() > kMaxChainAddressLength) return fail(Err::AddressTooLong);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    if (auto s = owner().verify(); !s) return s;
    for (const auto& v : vdrs)
        if (auto s = v.verify(); !s) return s;
    return ok();
}

Status CreateNetworkTx::visit(Visitor& v) const { return v.create_network_tx(*this); }

// ── ConvertNetworkTx

Result<std::shared_ptr<ConvertNetworkTx>> ConvertNetworkTx::create(
    const BaseTx& base, const Id& network, const Id& parent, const Id& manager_chain_id,
    const security::Mode& sec, std::span<const std::uint8_t> manager_address,
    const std::vector<NetworkValidator>& validators, const Auth& auth) {
    wire::ConvertNetworkInput in;
    envelope(in, Kind::ConvertNetwork, base, wire::spend(base));
    in.Network = network.b;
    in.Parent = parent.b;
    in.ManagerChainID = manager_chain_id.b;
    in.ManagerAddress = manager_address;
    auto vp = wire::validators(validators);
    in.Validators = std::move(vp.list);
    in.NodeIDPool = {vp.node_ids.data(), vp.node_ids.size()};
    in.AddrPool = std::move(vp.addrs);
    in.Auth = auth;
    in.RestakeParent = sec.restake_parent ? 1 : 0;
    in.Admission = static_cast<std::uint8_t>(sec.admission);
    in.Manager = static_cast<std::uint8_t>(sec.manager);
    in.Threshold = sec.threshold;
    auto buf = own(wire::NewConvertNetwork(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id ConvertNetworkTx::network() const { return Id::from(wire::ConvertNetwork(root()).Network()); }
Id ConvertNetworkTx::parent() const { return Id::from(wire::ConvertNetwork(root()).Parent()); }
Id ConvertNetworkTx::manager_chain_id() const {
    return Id::from(wire::ConvertNetwork(root()).ManagerChainID());
}
std::vector<std::uint8_t> ConvertNetworkTx::manager_address() const {
    const auto a = wire::ConvertNetwork(root()).ManagerAddress();
    return std::vector<std::uint8_t>(a.begin(), a.end());
}
std::vector<NetworkValidator> ConvertNetworkTx::validators() const {
    const wire::ConvertNetwork t(root());
    return wire::validators(t.Validators(), t.NodeIDPool(), t.AddrPool());
}
Auth ConvertNetworkTx::auth() const { return wire::auth(wire::ConvertNetwork(root()).Auth()); }
security::Mode ConvertNetworkTx::security_mode() const {
    const wire::ConvertNetwork t(root());
    security::Mode m;
    m.restake_parent = t.RestakeParent() != 0;
    m.admission = static_cast<security::Admission>(t.Admission());
    m.manager = static_cast<security::Manager>(t.Manager());
    m.threshold = t.Threshold();
    return m;
}

Status ConvertNetworkTx::syntactic_verify(const Runtime& rt) const {
    const auto vdrs = validators();
    const auto sec = security_mode();
    const auto addr = manager_address();
    if (network() == kPrimaryNetworkId) return fail(Err::ConvertPrimaryNetwork);
    if (vdrs.empty()) return fail(Err::ConvertMustHaveValidators);
    for (std::size_t i = 0; i + 1 < vdrs.size(); ++i)
        if (vdrs[i].compare(vdrs[i + 1]) >= 0) return fail(Err::ValidatorsNotSortedAndUnique);
    if (addr.size() > kMaxChainAddressLength) return fail(Err::AddressTooLong);
    if (auto s = sec.valid(); !s) return s;
    if (!sec.sovereign()) return fail(Err::ConvertMustEstablishOwnSet);
    if (sec.manager == security::Manager::Contract && addr.empty())
        return fail(Err::ContractManagerNeedsAddress);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    for (const auto& v : vdrs)
        if (auto s = v.verify(); !s) return s;
    return verify_auth(auth());
}

Status ConvertNetworkTx::visit(Visitor& v) const { return v.convert_network_tx(*this); }

// ── CreateChainTx

Result<std::shared_ptr<CreateChainTx>> CreateChainTx::create(const BaseTx& base, const Id& chain_id,
                                                              const std::string& blockchain_name,
                                                              const Id& vm_id, const std::vector<Id>& fx_ids,
                                                              std::span<const std::uint8_t> genesis_data,
                                                              const Auth& chain_auth) {
    wire::CreateChainInput in;
    envelope(in, Kind::CreateChain, base, wire::spend(base));
    in.Chain = chain_id.b;
    in.VmID = vm_id.b;
    in.Name = blockchain_name;
    in.FxIds.reserve(fx_ids.size());
    for (const auto& id : fx_ids) in.FxIds.push_back(id.b);
    in.Genesis = genesis_data;
    in.Auth = chain_auth;
    auto buf = own(wire::NewCreateChain(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id CreateChainTx::chain_id() const { return Id::from(wire::CreateChain(root()).Chain()); }
Id CreateChainTx::vm_id() const { return Id::from(wire::CreateChain(root()).VmID()); }
std::string CreateChainTx::blockchain_name() const {
    return std::string(wire::CreateChain(root()).Name());
}
std::vector<Id> CreateChainTx::fx_ids() const {
    const wire::CreateChain t(root());
    const int n = t.FxIds().size();
    std::vector<Id> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) out[static_cast<std::size_t>(i)] = Id::from(t.FxIdsAt(i));
    return out;
}
std::vector<std::uint8_t> CreateChainTx::genesis_data() const {
    const auto g = wire::CreateChain(root()).Genesis();
    return std::vector<std::uint8_t>(g.begin(), g.end());
}
Auth CreateChainTx::chain_auth() const { return wire::auth(wire::CreateChain(root()).Auth()); }

Status CreateChainTx::syntactic_verify(const Runtime& rt) const {
    const auto name = blockchain_name();
    const auto fx = fx_ids();
    if (chain_id() == kPrimaryNetworkId) return fail(Err::CantValidatePrimaryNetwork);
    if (name.size() > kMaxNameLen) return fail(Err::NameTooLong);
    if (vm_id().empty()) return fail(Err::InvalidVMID);
    for (std::size_t i = 0; i + 1 < fx.size(); ++i)
        if (!(fx[i] < fx[i + 1])) return fail(Err::FxIDsNotSortedAndUnique);
    if (genesis_data().size() > kMaxGenesisLen) return fail(Err::GenesisTooLong);
    for (const char c : name)
        if (!ascii_name_char(static_cast<std::uint8_t>(c))) return fail(Err::IllegalNameCharacter);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    return verify_auth(chain_auth());
}

Status CreateChainTx::visit(Visitor& v) const { return v.create_chain_tx(*this); }

// ── TransferChainOwnershipTx

Result<std::shared_ptr<TransferChainOwnershipTx>> TransferChainOwnershipTx::create(const BaseTx& base,
                                                                                    const Id& chain,
                                                                                    const Auth& chain_auth,
                                                                                    const Owner& owner) {
    wire::TransferChainOwnershipInput in;
    envelope(in, Kind::TransferChainOwnership, base, wire::spend(base));
    in.Chain = chain.b;
    in.Auth = chain_auth;
    in.OwnerThreshold = owner.threshold;
    in.OwnerLocktime = owner.locktime;
    in.NewOwnerAddrs = wire::addrs(owner.addrs);
    auto buf = own(wire::NewTransferChainOwnership(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id TransferChainOwnershipTx::chain() const {
    return Id::from(wire::TransferChainOwnership(root()).Chain());
}
Auth TransferChainOwnershipTx::chain_auth() const {
    return wire::auth(wire::TransferChainOwnership(root()).Auth());
}
Owner TransferChainOwnershipTx::owner() const {
    const wire::TransferChainOwnership t(root());
    return wire::owner(t.OwnerThreshold(), t.OwnerLocktime(), t.NewOwnerAddrs());
}

Status TransferChainOwnershipTx::syntactic_verify(const Runtime& rt) const {
    if (chain() == kPrimaryNetworkId) return fail(Err::TransferPermissionlessChain);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    if (auto s = verify_auth(chain_auth()); !s) return s;
    return owner().verify();
}

Status TransferChainOwnershipTx::visit(Visitor& v) const { return v.transfer_chain_ownership_tx(*this); }

// ── RemoveChainValidatorTx

Result<std::shared_ptr<RemoveChainValidatorTx>> RemoveChainValidatorTx::create(const BaseTx& base,
                                                                                const NodeId& node_id,
                                                                                const Id& chain,
                                                                                const Auth& chain_auth) {
    wire::RemoveChainValidatorInput in;
    envelope(in, Kind::RemoveChainValidator, base, wire::spend(base));
    in.NodeID = node_id.b;
    in.Chain = chain.b;
    in.Auth = chain_auth;
    auto buf = own(wire::NewRemoveChainValidator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

NodeId RemoveChainValidatorTx::node_id() const {
    return NodeId::from(wire::RemoveChainValidator(root()).NodeID());
}
Id RemoveChainValidatorTx::chain() const { return Id::from(wire::RemoveChainValidator(root()).Chain()); }
Auth RemoveChainValidatorTx::chain_auth() const {
    return wire::auth(wire::RemoveChainValidator(root()).Auth());
}

Status RemoveChainValidatorTx::syntactic_verify(const Runtime& rt) const {
    if (chain() == kPrimaryNetworkId) return fail(Err::RemovePrimaryNetworkValidator);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    return verify_auth(chain_auth());
}

Status RemoveChainValidatorTx::visit(Visitor& v) const { return v.remove_chain_validator_tx(*this); }

// ── TransformChainTx

Result<std::shared_ptr<TransformChainTx>> TransformChainTx::create(const BaseTx& base, const Params& q,
                                                                    const Auth& chain_auth) {
    wire::TransformChainInput in;
    envelope(in, Kind::TransformChain, base, wire::spend(base));
    in.Chain = q.chain.b;
    in.Asset = q.asset_id.b;
    in.InitialSupply = q.initial_supply;
    in.MaximumSupply = q.maximum_supply;
    in.MinConsumptionRate = q.min_consumption_rate;
    in.MaxConsumptionRate = q.max_consumption_rate;
    in.MinValidatorStake = q.min_validator_stake;
    in.MaxValidatorStake = q.max_validator_stake;
    in.MinStakeDuration = q.min_stake_duration;
    in.MaxStakeDuration = q.max_stake_duration;
    in.MinDelegationFee = q.min_delegation_fee;
    in.MinDelegatorStake = q.min_delegator_stake;
    in.MaxValidatorWeightFactor = q.max_validator_weight_factor;
    in.UptimeRequirement = q.uptime_requirement;
    in.Auth = chain_auth;
    auto buf = own(wire::NewTransformChain(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id TransformChainTx::chain() const { return Id::from(wire::TransformChain(root()).Chain()); }
Id TransformChainTx::asset_id() const { return Id::from(wire::TransformChain(root()).Asset()); }
std::uint64_t TransformChainTx::initial_supply() const { return wire::TransformChain(root()).InitialSupply(); }
std::uint64_t TransformChainTx::maximum_supply() const { return wire::TransformChain(root()).MaximumSupply(); }
std::uint64_t TransformChainTx::min_consumption_rate() const {
    return wire::TransformChain(root()).MinConsumptionRate();
}
std::uint64_t TransformChainTx::max_consumption_rate() const {
    return wire::TransformChain(root()).MaxConsumptionRate();
}
std::uint64_t TransformChainTx::min_validator_stake() const {
    return wire::TransformChain(root()).MinValidatorStake();
}
std::uint64_t TransformChainTx::max_validator_stake() const {
    return wire::TransformChain(root()).MaxValidatorStake();
}
std::uint32_t TransformChainTx::min_stake_duration() const {
    return wire::TransformChain(root()).MinStakeDuration();
}
std::uint32_t TransformChainTx::max_stake_duration() const {
    return wire::TransformChain(root()).MaxStakeDuration();
}
std::uint32_t TransformChainTx::min_delegation_fee() const {
    return wire::TransformChain(root()).MinDelegationFee();
}
std::uint64_t TransformChainTx::min_delegator_stake() const {
    return wire::TransformChain(root()).MinDelegatorStake();
}
std::uint8_t TransformChainTx::max_validator_weight_factor() const {
    return wire::TransformChain(root()).MaxValidatorWeightFactor();
}
std::uint32_t TransformChainTx::uptime_requirement() const {
    return wire::TransformChain(root()).UptimeRequirement();
}
Auth TransformChainTx::chain_auth() const { return wire::auth(wire::TransformChain(root()).Auth()); }

Status TransformChainTx::syntactic_verify(const Runtime& rt) const {
    if (chain() == kPrimaryNetworkId) return fail(Err::CantTransformPrimaryNetwork);
    if (asset_id().empty()) return fail(Err::TransformEmptyAssetID);
    if (asset_id() == rt.utxo_asset_id) return fail(Err::AssetIDCantBeLUX);
    if (initial_supply() == 0) return fail(Err::InitialSupplyZero);
    if (initial_supply() > maximum_supply()) return fail(Err::InitialSupplyGreaterThanMaxSupply);
    if (min_consumption_rate() > max_consumption_rate()) return fail(Err::MinConsumptionRateTooLarge);
    if (max_consumption_rate() > reward::kPercentDenominator) return fail(Err::MaxConsumptionRateTooLarge);
    if (min_validator_stake() == 0) return fail(Err::MinValidatorStakeZero);
    if (min_validator_stake() > initial_supply()) return fail(Err::MinValidatorStakeAboveSupply);
    if (min_validator_stake() > max_validator_stake()) return fail(Err::MinValidatorStakeAboveMax);
    if (max_validator_stake() > maximum_supply()) return fail(Err::MaxValidatorStakeTooLarge);
    if (min_stake_duration() == 0) return fail(Err::MinStakeDurationZero);
    if (min_stake_duration() > max_stake_duration()) return fail(Err::MinStakeDurationTooLarge);
    if (min_delegation_fee() > reward::kPercentDenominator) return fail(Err::MinDelegationFeeTooLarge);
    if (min_delegator_stake() == 0) return fail(Err::MinDelegatorStakeZero);
    if (max_validator_weight_factor() == 0) return fail(Err::MaxValidatorWeightFactorZero);
    if (uptime_requirement() > reward::kPercentDenominator) return fail(Err::UptimeRequirementTooLarge);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    return verify_auth(chain_auth());
}

Status TransformChainTx::visit(Visitor& v) const { return v.transform_chain_tx(*this); }

// ── AddValidatorTx

Result<std::shared_ptr<AddValidatorTx>> AddValidatorTx::create(
    const BaseTx& base, const Validator& validator, const std::vector<TransferableOutput>& stake_outs,
    const Owner& rewards_owner, std::uint32_t delegation_shares) {
    wire::AddValidatorInput in;
    envelope(in, Kind::AddValidator, base, wire::spend(base));
    in.NodeID = validator.node_id.b;
    in.Start = validator.start;
    in.End = validator.end;
    in.Weight = validator.weight;
    auto so = wire::outs(stake_outs);
    in.StakeOuts = std::move(so.list);
    in.StakeAddrs = std::move(so.addrs);
    in.RewardsThreshold = rewards_owner.threshold;
    in.RewardsLocktime = rewards_owner.locktime;
    in.RewardsAddrs = wire::addrs(rewards_owner.addrs);
    in.DelegationShares = delegation_shares;
    auto buf = own(wire::NewAddValidator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddValidatorTx::validator() const {
    const wire::AddValidator t(root());
    return wire::validator(t.NodeID(), t.Start(), t.End(), t.Weight());
}
std::vector<TransferableOutput> AddValidatorTx::stake_outs() const {
    const wire::AddValidator t(root());
    return wire::outs(t.StakeOuts(), t.StakeAddrs());
}
Owner AddValidatorTx::rewards_owner() const {
    const wire::AddValidator t(root());
    return wire::owner(t.RewardsThreshold(), t.RewardsLocktime(), t.RewardsAddrs());
}
std::uint32_t AddValidatorTx::delegation_shares() const {
    return wire::AddValidator(root()).DelegationShares();
}

Status AddValidatorTx::syntactic_verify(const Runtime& rt) const {
    if (delegation_shares() > reward::kPercentDenominator) return fail(Err::TooManyShares);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    const auto v = validator();
    if (auto s = v.verify(); !s) return s;
    if (auto s = rewards_owner().verify(); !s) return s;

    const auto outs = stake_outs();
    std::uint64_t total = 0;
    for (const auto& out : outs) {
        if (auto s = out.verify(); !s) return s;
        const auto sum = add64(total, out.amount());
        if (!sum) return std::unexpected(sum.error());
        total = *sum;
        if (!(out.asset_id() == rt.utxo_asset_id)) return fail(Err::StakeMustBeLUX);
    }
    if (!outputs_sorted(outs)) return fail(Err::OutputsNotSorted);
    if (total != v.weight) return fail(Err::ValidatorWeightMismatch);
    return ok();
}

Status AddValidatorTx::visit(Visitor& v) const { return v.add_validator_tx(*this); }

// ── AddDelegatorTx

Result<std::shared_ptr<AddDelegatorTx>> AddDelegatorTx::create(
    const BaseTx& base, const Validator& validator, const std::vector<TransferableOutput>& stake_outs,
    const Owner& rewards_owner) {
    wire::AddDelegatorInput in;
    envelope(in, Kind::AddDelegator, base, wire::spend(base));
    in.NodeID = validator.node_id.b;
    in.Start = validator.start;
    in.End = validator.end;
    in.Weight = validator.weight;
    auto so = wire::outs(stake_outs);
    in.StakeOuts = std::move(so.list);
    in.StakeAddrs = std::move(so.addrs);
    in.RewardsThreshold = rewards_owner.threshold;
    in.RewardsLocktime = rewards_owner.locktime;
    in.RewardsAddrs = wire::addrs(rewards_owner.addrs);
    auto buf = own(wire::NewAddDelegator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddDelegatorTx::validator() const {
    const wire::AddDelegator t(root());
    return wire::validator(t.NodeID(), t.Start(), t.End(), t.Weight());
}
std::vector<TransferableOutput> AddDelegatorTx::stake_outs() const {
    const wire::AddDelegator t(root());
    return wire::outs(t.StakeOuts(), t.StakeAddrs());
}
Owner AddDelegatorTx::delegation_rewards_owner() const {
    const wire::AddDelegator t(root());
    return wire::owner(t.RewardsThreshold(), t.RewardsLocktime(), t.RewardsAddrs());
}

Status AddDelegatorTx::syntactic_verify(const Runtime& rt) const {
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    const auto v = validator();
    if (auto s = v.verify(); !s) return s;
    if (auto s = delegation_rewards_owner().verify(); !s) return s;

    const auto outs = stake_outs();
    std::uint64_t total = 0;
    for (const auto& out : outs) {
        if (auto s = out.verify(); !s) return s;
        const auto sum = add64(total, out.amount());
        if (!sum) return std::unexpected(sum.error());
        total = *sum;
        if (!(out.asset_id() == rt.utxo_asset_id)) return fail(Err::StakeMustBeLUX);
    }
    if (!outputs_sorted(outs)) return fail(Err::OutputsNotSorted);
    if (total != v.weight) return fail(Err::DelegatorWeightMismatch);
    return ok();
}

Status AddDelegatorTx::visit(Visitor& v) const { return v.add_delegator_tx(*this); }

// ── AddChainValidatorTx

Result<std::shared_ptr<AddChainValidatorTx>> AddChainValidatorTx::create(const BaseTx& base,
                                                                          const Validator& validator,
                                                                          const Id& chain,
                                                                          const Auth& chain_auth) {
    wire::AddChainValidatorInput in;
    envelope(in, Kind::AddChainValidator, base, wire::spend(base));
    in.NodeID = validator.node_id.b;
    in.Start = validator.start;
    in.End = validator.end;
    in.Weight = validator.weight;
    in.Chain = chain.b;
    in.Auth = chain_auth;
    auto buf = own(wire::NewAddChainValidator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddChainValidatorTx::validator() const {
    const wire::AddChainValidator t(root());
    return wire::validator(t.NodeID(), t.Start(), t.End(), t.Weight());
}
Id AddChainValidatorTx::chain() const { return Id::from(wire::AddChainValidator(root()).Chain()); }
Auth AddChainValidatorTx::chain_auth() const {
    return wire::auth(wire::AddChainValidator(root()).Auth());
}

Status AddChainValidatorTx::syntactic_verify(const Runtime& rt) const {
    if (chain() == kPrimaryNetworkId) return fail(Err::AddPrimaryNetworkValidator);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    if (auto s = validator().verify(); !s) return s;
    return verify_auth(chain_auth());
}

Status AddChainValidatorTx::visit(Visitor& v) const { return v.add_chain_validator_tx(*this); }

// ── AddPermissionlessValidatorTx

Result<std::shared_ptr<AddPermissionlessValidatorTx>> AddPermissionlessValidatorTx::create(
    const BaseTx& base, const Validator& validator, const Id& chain, const signer::Signer& sig,
    const std::vector<TransferableOutput>& stake_outs, const Owner& validator_rewards_owner,
    const Owner& delegator_rewards_owner, std::uint32_t delegation_shares) {
    wire::AddPermissionlessValidatorInput in;
    envelope(in, Kind::AddPermissionlessValidator, base, wire::spend(base));
    in.NodeID = validator.node_id.b;
    in.Start = validator.start;
    in.End = validator.end;
    in.Weight = validator.weight;
    in.Chain = chain.b;
    if (const auto* pop = std::get_if<signer::ProofOfPossession>(&sig)) {
        in.SignerKind = 1;
        in.SignerKey = pop->public_key;
        in.SignerProof = pop->proof;
    }
    auto so = wire::outs(stake_outs);
    in.StakeOuts = std::move(so.list);
    in.StakeAddrs = std::move(so.addrs);
    in.ValidatorRewardsThreshold = validator_rewards_owner.threshold;
    in.ValidatorRewardsLocktime = validator_rewards_owner.locktime;
    in.ValidatorRewardsAddrs = wire::addrs(validator_rewards_owner.addrs);
    in.DelegatorRewardsThreshold = delegator_rewards_owner.threshold;
    in.DelegatorRewardsLocktime = delegator_rewards_owner.locktime;
    in.DelegatorRewardsAddrs = wire::addrs(delegator_rewards_owner.addrs);
    in.DelegationShares = delegation_shares;
    auto buf = own(wire::NewAddPermissionlessValidator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddPermissionlessValidatorTx::validator() const {
    const wire::AddPermissionlessValidator t(root());
    return wire::validator(t.NodeID(), t.Start(), t.End(), t.Weight());
}
Id AddPermissionlessValidatorTx::chain() const {
    return Id::from(wire::AddPermissionlessValidator(root()).Chain());
}
signer::Signer AddPermissionlessValidatorTx::signer_value() const {
    const wire::AddPermissionlessValidator t(root());
    if (t.SignerKind() == 0) return signer::Empty{};
    signer::ProofOfPossession p;
    const auto key = t.SignerKey();
    std::memcpy(p.public_key.data(), key.data(), p.public_key.size());
    const auto proof = t.SignerProof();
    std::memcpy(p.proof.data(), proof.data(), p.proof.size());
    return p;
}
std::vector<TransferableOutput> AddPermissionlessValidatorTx::stake_outs() const {
    const wire::AddPermissionlessValidator t(root());
    return wire::outs(t.StakeOuts(), t.StakeAddrs());
}
Owner AddPermissionlessValidatorTx::validator_rewards_owner() const {
    const wire::AddPermissionlessValidator t(root());
    return wire::owner(t.ValidatorRewardsThreshold(), t.ValidatorRewardsLocktime(),
                       t.ValidatorRewardsAddrs());
}
Owner AddPermissionlessValidatorTx::delegator_rewards_owner() const {
    const wire::AddPermissionlessValidator t(root());
    return wire::owner(t.DelegatorRewardsThreshold(), t.DelegatorRewardsLocktime(),
                       t.DelegatorRewardsAddrs());
}
std::uint32_t AddPermissionlessValidatorTx::delegation_shares() const {
    return wire::AddPermissionlessValidator(root()).DelegationShares();
}

Result<std::optional<signer::PublicKeyBytes>> AddPermissionlessValidatorTx::public_key() const {
    const auto sig = signer_value();
    if (auto s = signer::verify(sig); !s) return std::unexpected(s.error());
    if (const auto* k = signer::key(sig)) return std::optional<signer::PublicKeyBytes>(*k);
    return std::optional<signer::PublicKeyBytes>();
}

Priority AddPermissionlessValidatorTx::pending_priority() const {
    return chain() == kPrimaryNetworkId ? Priority::PrimaryNetworkValidatorPending
                                        : Priority::ChainPermissionlessValidatorPending;
}
Priority AddPermissionlessValidatorTx::current_priority() const {
    return chain() == kPrimaryNetworkId ? Priority::PrimaryNetworkValidatorCurrent
                                        : Priority::ChainPermissionlessValidatorCurrent;
}

Status AddPermissionlessValidatorTx::syntactic_verify(const Runtime& rt) const {
    const auto v = validator();
    const auto outs = stake_outs();
    if (v.node_id == kEmptyNodeId) return fail(Err::EmptyNodeID);
    if (outs.empty()) return fail(Err::NoStake);
    if (delegation_shares() > reward::kPercentDenominator) return fail(Err::TooManyShares);

    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    const auto sig = signer_value();
    if (auto s = v.verify(); !s) return s;
    if (auto s = signer::verify(sig); !s) return s;
    if (auto s = validator_rewards_owner().verify(); !s) return s;
    if (auto s = delegator_rewards_owner().verify(); !s) return s;

    // A key is registered exactly when the validator joins the primary network:
    // that is the set whose aggregate signature has to mean something.
    const bool has_key = signer::has_key(sig);
    const bool is_primary = chain() == kPrimaryNetworkId;
    if (has_key != is_primary) return fail(Err::InvalidSigner);

    for (const auto& out : outs)
        if (auto s = out.verify(); !s) return s;

    const Id staked_asset = outs[0].asset_id();
    std::uint64_t total = outs[0].amount();
    for (std::size_t i = 1; i < outs.size(); ++i) {
        const auto sum = add64(total, outs[i].amount());
        if (!sum) return std::unexpected(sum.error());
        total = *sum;
        if (!(outs[i].asset_id() == staked_asset)) return fail(Err::MultipleStakedAssets);
    }
    if (!outputs_sorted(outs)) return fail(Err::OutputsNotSorted);
    if (total != v.weight) return fail(Err::ValidatorWeightMismatch);
    return ok();
}

Status AddPermissionlessValidatorTx::visit(Visitor& v) const {
    return v.add_permissionless_validator_tx(*this);
}

// ── AddPermissionlessDelegatorTx

Result<std::shared_ptr<AddPermissionlessDelegatorTx>> AddPermissionlessDelegatorTx::create(
    const BaseTx& base, const Validator& validator, const Id& chain,
    const std::vector<TransferableOutput>& stake_outs, const Owner& delegation_rewards_owner) {
    wire::AddPermissionlessDelegatorInput in;
    envelope(in, Kind::AddPermissionlessDelegator, base, wire::spend(base));
    in.NodeID = validator.node_id.b;
    in.Start = validator.start;
    in.End = validator.end;
    in.Weight = validator.weight;
    in.Chain = chain.b;
    auto so = wire::outs(stake_outs);
    in.StakeOuts = std::move(so.list);
    in.StakeAddrs = std::move(so.addrs);
    in.RewardsThreshold = delegation_rewards_owner.threshold;
    in.RewardsLocktime = delegation_rewards_owner.locktime;
    in.RewardsAddrs = wire::addrs(delegation_rewards_owner.addrs);
    auto buf = own(wire::NewAddPermissionlessDelegator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddPermissionlessDelegatorTx::validator() const {
    const wire::AddPermissionlessDelegator t(root());
    return wire::validator(t.NodeID(), t.Start(), t.End(), t.Weight());
}
Id AddPermissionlessDelegatorTx::chain() const {
    return Id::from(wire::AddPermissionlessDelegator(root()).Chain());
}
std::vector<TransferableOutput> AddPermissionlessDelegatorTx::stake_outs() const {
    const wire::AddPermissionlessDelegator t(root());
    return wire::outs(t.StakeOuts(), t.StakeAddrs());
}
Owner AddPermissionlessDelegatorTx::delegation_rewards_owner() const {
    const wire::AddPermissionlessDelegator t(root());
    return wire::owner(t.RewardsThreshold(), t.RewardsLocktime(), t.RewardsAddrs());
}

Priority AddPermissionlessDelegatorTx::pending_priority() const {
    return chain() == kPrimaryNetworkId ? Priority::PrimaryNetworkDelegatorPermissionlessPending
                                        : Priority::ChainPermissionlessDelegatorPending;
}
Priority AddPermissionlessDelegatorTx::current_priority() const {
    return chain() == kPrimaryNetworkId ? Priority::PrimaryNetworkDelegatorCurrent
                                        : Priority::ChainPermissionlessDelegatorCurrent;
}

Status AddPermissionlessDelegatorTx::syntactic_verify(const Runtime& rt) const {
    const auto outs = stake_outs();
    if (outs.empty()) return fail(Err::NoStake);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    const auto v = validator();
    if (auto s = v.verify(); !s) return s;
    if (auto s = delegation_rewards_owner().verify(); !s) return s;

    for (const auto& out : outs)
        if (auto s = out.verify(); !s) return s;

    const Id staked_asset = outs[0].asset_id();
    std::uint64_t total = outs[0].amount();
    for (std::size_t i = 1; i < outs.size(); ++i) {
        const auto sum = add64(total, outs[i].amount());
        if (!sum) return std::unexpected(sum.error());
        total = *sum;
        if (!(outs[i].asset_id() == staked_asset)) return fail(Err::MultipleStakedAssets);
    }
    if (!outputs_sorted(outs)) return fail(Err::OutputsNotSorted);
    if (total != v.weight) return fail(Err::DelegatorWeightMismatch);
    return ok();
}

Status AddPermissionlessDelegatorTx::visit(Visitor& v) const {
    return v.add_permissionless_delegator_tx(*this);
}

// ── RegisterL1ValidatorTx

Result<std::shared_ptr<RegisterL1ValidatorTx>> RegisterL1ValidatorTx::create(
    const BaseTx& base, std::uint64_t balance, const signer::SignatureBytes& proof_of_possession,
    std::span<const std::uint8_t> message) {
    wire::RegisterL1ValidatorInput in;
    envelope(in, Kind::RegisterL1Validator, base, wire::spend(base));
    in.Balance = balance;
    in.Proof = proof_of_possession;
    in.Message = message;
    auto buf = own(wire::NewRegisterL1Validator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

std::uint64_t RegisterL1ValidatorTx::balance() const {
    return wire::RegisterL1Validator(root()).Balance();
}
signer::SignatureBytes RegisterL1ValidatorTx::proof_of_possession() const {
    signer::SignatureBytes pop{};
    const auto b = wire::RegisterL1Validator(root()).Proof();
    std::memcpy(pop.data(), b.data(), pop.size());
    return pop;
}
std::vector<std::uint8_t> RegisterL1ValidatorTx::message() const {
    const auto m = wire::RegisterL1Validator(root()).Message();
    return std::vector<std::uint8_t>(m.begin(), m.end());
}

Status RegisterL1ValidatorTx::syntactic_verify(const Runtime& rt) const {
    return verify_base_tx(base_tx(), rt);
}
Status RegisterL1ValidatorTx::visit(Visitor& v) const { return v.register_l1_validator_tx(*this); }

// ── SetL1ValidatorWeightTx

Result<std::shared_ptr<SetL1ValidatorWeightTx>> SetL1ValidatorWeightTx::create(
    const BaseTx& base, std::span<const std::uint8_t> message) {
    wire::SetL1ValidatorWeightInput in;
    envelope(in, Kind::SetL1ValidatorWeight, base, wire::spend(base));
    in.Message = message;
    auto buf = own(wire::NewSetL1ValidatorWeight(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

std::vector<std::uint8_t> SetL1ValidatorWeightTx::message() const {
    const auto m = wire::SetL1ValidatorWeight(root()).Message();
    return std::vector<std::uint8_t>(m.begin(), m.end());
}

Status SetL1ValidatorWeightTx::syntactic_verify(const Runtime& rt) const {
    return verify_base_tx(base_tx(), rt);
}
Status SetL1ValidatorWeightTx::visit(Visitor& v) const { return v.set_l1_validator_weight_tx(*this); }

// ── IncreaseL1ValidatorBalanceTx

Result<std::shared_ptr<IncreaseL1ValidatorBalanceTx>> IncreaseL1ValidatorBalanceTx::create(
    const BaseTx& base, const Id& validation_id, std::uint64_t balance) {
    wire::IncreaseL1ValidatorBalanceInput in;
    envelope(in, Kind::IncreaseL1ValidatorBalance, base, wire::spend(base));
    in.ValidationID = validation_id.b;
    in.Balance = balance;
    auto buf = own(wire::NewIncreaseL1ValidatorBalance(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id IncreaseL1ValidatorBalanceTx::validation_id() const {
    return Id::from(wire::IncreaseL1ValidatorBalance(root()).ValidationID());
}
std::uint64_t IncreaseL1ValidatorBalanceTx::balance() const {
    return wire::IncreaseL1ValidatorBalance(root()).Balance();
}

Status IncreaseL1ValidatorBalanceTx::syntactic_verify(const Runtime& rt) const {
    if (balance() == 0) return fail(Err::ZeroBalance);
    return verify_base_tx(base_tx(), rt);
}
Status IncreaseL1ValidatorBalanceTx::visit(Visitor& v) const {
    return v.increase_l1_validator_balance_tx(*this);
}

// ── DisableL1ValidatorTx

Result<std::shared_ptr<DisableL1ValidatorTx>> DisableL1ValidatorTx::create(const BaseTx& base,
                                                                            const Id& validation_id,
                                                                            const Auth& disable_auth) {
    wire::DisableL1ValidatorInput in;
    envelope(in, Kind::DisableL1Validator, base, wire::spend(base));
    in.ValidationID = validation_id.b;
    in.Auth = disable_auth;
    auto buf = own(wire::NewDisableL1Validator(in));
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id DisableL1ValidatorTx::validation_id() const {
    return Id::from(wire::DisableL1Validator(root()).ValidationID());
}
Auth DisableL1ValidatorTx::disable_auth() const {
    return wire::auth(wire::DisableL1Validator(root()).Auth());
}

Status DisableL1ValidatorTx::syntactic_verify(const Runtime& rt) const {
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    return verify_auth(disable_auth());
}
Status DisableL1ValidatorTx::visit(Visitor& v) const { return v.disable_l1_validator_tx(*this); }

// ── RewardValidatorTx

std::shared_ptr<RewardValidatorTx> RewardValidatorTx::create(const Id& tx_id) {
    return wrap(own(wire::NewRewardValidator(wire::RewardValidatorInput{
        .Kind = static_cast<std::uint8_t>(Kind::RewardValidator),
        .StakerTxID = tx_id.b,
    })));
}

Id RewardValidatorTx::tx_id() const { return Id::from(wire::RewardValidator(root()).StakerTxID()); }
Status RewardValidatorTx::visit(Visitor& v) const { return v.reward_validator_tx(*this); }

// ── the staker view

Result<std::optional<StakerView>> staker_of(const UnsignedTx& tx) {
    StakerView s;
    switch (tx.kind()) {
        case Kind::AddValidator: {
            const auto& t = static_cast<const AddValidatorTx&>(tx);
            const auto v = t.validator();
            s = StakerView{t.chain_id(), v.node_id, std::nullopt, v.start, v.end, v.weight,
                           t.current_priority(), t.pending_priority(), true};
            return std::optional<StakerView>(s);
        }
        case Kind::AddDelegator: {
            const auto& t = static_cast<const AddDelegatorTx&>(tx);
            const auto v = t.validator();
            s = StakerView{t.chain_id(), v.node_id, std::nullopt, v.start, v.end, v.weight,
                           t.current_priority(), t.pending_priority(), true};
            return std::optional<StakerView>(s);
        }
        case Kind::AddChainValidator: {
            const auto& t = static_cast<const AddChainValidatorTx&>(tx);
            const auto v = t.validator();
            s = StakerView{t.chain_id(), v.node_id, std::nullopt, v.start, v.end, v.weight,
                           t.current_priority(), t.pending_priority(), true};
            return std::optional<StakerView>(s);
        }
        case Kind::AddPermissionlessValidator: {
            const auto& t = static_cast<const AddPermissionlessValidatorTx&>(tx);
            const auto v = t.validator();
            const auto pk = t.public_key();
            if (!pk) return std::unexpected(pk.error());
            s = StakerView{t.chain_id(), v.node_id, *pk, v.start, v.end, v.weight, t.current_priority(),
                           t.pending_priority(), true};
            return std::optional<StakerView>(s);
        }
        case Kind::AddPermissionlessDelegator: {
            const auto& t = static_cast<const AddPermissionlessDelegatorTx&>(tx);
            const auto v = t.validator();
            s = StakerView{t.chain_id(), v.node_id, std::nullopt, v.start, v.end, v.weight,
                           t.current_priority(), t.pending_priority(), true};
            return std::optional<StakerView>(s);
        }
        default:
            return std::optional<StakerView>();
    }
}

std::vector<TransferableOutput> stake_of(const UnsignedTx& tx) {
    switch (tx.kind()) {
        case Kind::AddValidator:
            return static_cast<const AddValidatorTx&>(tx).stake_outs();
        case Kind::AddDelegator:
            return static_cast<const AddDelegatorTx&>(tx).stake_outs();
        case Kind::AddPermissionlessValidator:
            return static_cast<const AddPermissionlessValidatorTx&>(tx).stake_outs();
        case Kind::AddPermissionlessDelegator:
            return static_cast<const AddPermissionlessDelegatorTx&>(tx).stake_outs();
        default:
            return {};
    }
}

// ── an owner set standing alone, the shape state keeps one in

std::vector<std::uint8_t> marshal_owner(const Owner& o) {
    return wire::NewOwner(wire::OwnerInput{
        .Threshold = o.threshold,
        .Locktime = o.locktime,
        .Addrs = wire::addrs(o.addrs),
    });
}

Result<Owner> unmarshal_owner(std::span<const std::uint8_t> b) {
    const auto w = wire::WrapOwner(b);
    if (!w) return fail(Err::BufferTooSmall);
    return wire::owner(w->Threshold(), w->Locktime(), w->Addrs());
}

}  // namespace lux::platformvm::txs
