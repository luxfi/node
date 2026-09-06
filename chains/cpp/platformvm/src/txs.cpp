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

Buffer finish(zap::Builder& b) { return std::make_shared<const std::vector<std::uint8_t>>(b.finish()); }

bool parses(const Buffer& buf) {
    return zap::Message::parse({buf->data(), buf->size()}).has_value();
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

Id SpendingTx::blockchain_id() const { return wire::read_id(root(), kOffBlockchainId); }

std::vector<TransferableOutput> SpendingTx::outputs() const {
    return wire::read_outputs(root(), kOffOuts, kOffOwnerAddrs);
}

std::vector<TransferableInput> SpendingTx::inputs() const {
    return wire::read_inputs(root(), kOffIns, kOffSigIndices);
}

std::vector<std::uint8_t> SpendingTx::memo() const {
    const auto m = root().bytes(kOffMemo);
    return std::vector<std::uint8_t>(m.begin(), m.end());
}

std::vector<Id> SpendingTx::input_ids() const {
    std::vector<Id> out;
    for (const auto& in : inputs()) out.push_back(in.input_id());
    return out;
}

BaseTx SpendingTx::base_tx() const {
    BaseTx b;
    const auto r = root();
    b.network_id = r.u32(kOffNetworkId);
    b.blockchain_id = wire::read_id(r, kOffBlockchainId);
    b.outs = wire::read_outputs(r, kOffOuts, kOffOwnerAddrs);
    b.ins = wire::read_inputs(r, kOffIns, kOffSigIndices);
    b.memo = memo();
    return b;
}

// ── BaseTx

Result<std::shared_ptr<BaseTxUnsigned>> BaseTxUnsigned::create(const BaseTx& base) {
    zap::Builder b(zap::kHeaderSize + 256 + kSpendSize);
    const auto p = wire::write_spending(b, base);
    auto ob = b.start_object(kSpendSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::Base), base, p);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Status BaseTxUnsigned::syntactic_verify(const Runtime& rt) const { return verify_base_tx(base_tx(), rt); }
Status BaseTxUnsigned::visit(Visitor& v) const { return v.base_tx(*this); }

// ── ImportTx

Result<std::shared_ptr<ImportTx>> ImportTx::create(const BaseTx& base, const Id& source_chain,
                                                   const std::vector<TransferableInput>& imported) {
    zap::Builder b(zap::kHeaderSize + 512 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto extra = wire::write_inputs(b, imported);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::Import), base, p);
    wire::set_id(ob, kOffSourceChain, source_chain);
    ob.set_list(kOffInputs, extra.list_off, extra.list_count);
    ob.set_list(kOffSigIdx, extra.sig_off, extra.sig_count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id ImportTx::source_chain() const { return wire::read_id(root(), kOffSourceChain); }

std::vector<TransferableInput> ImportTx::imported_inputs() const {
    return wire::read_inputs(root(), kOffInputs, kOffSigIdx);
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
    zap::Builder b(zap::kHeaderSize + 512 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto extra = wire::write_outputs(b, exported);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::Export), base, p);
    wire::set_id(ob, kOffDestChain, destination_chain);
    ob.set_list(kOffOutputs, extra.list_off, extra.list_count);
    ob.set_list(kOffAddrs, extra.addr_off, extra.addr_count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id ExportTx::destination_chain() const { return wire::read_id(root(), kOffDestChain); }

std::vector<TransferableOutput> ExportTx::exported_outputs() const {
    return wire::read_outputs(root(), kOffOutputs, kOffAddrs);
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
    zap::Builder b(zap::kHeaderSize + 1024 + kSize + static_cast<std::int64_t>(validators.size()) * wire::kNvStride);
    const auto p = wire::write_spending(b, base);
    const auto op = wire::write_owner(b, owner);
    std::vector<std::uint8_t> node_ids;
    std::vector<ShortId> addr_pool;
    const auto vp = wire::write_network_validators(b, validators, node_ids, addr_pool);
    std::int64_t val_addr_off = 0, val_addr_count = 0;
    if (!addr_pool.empty()) {
        auto alb = b.start_list(wire::kAddrStride);
        for (const auto& a : addr_pool) alb.add_bytes(a.span());
        const auto [alb_off, alb_count] = alb.finish();
        val_addr_off = alb_off;
        val_addr_count = static_cast<std::int64_t>(addr_pool.size());
    }

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::CreateNetwork), base, p);
    wire::set_id(ob, kOffParent, parent);
    wire::set_owner(ob, kOffOwnerThreshold, kOffOwnerLocktime, kOffOwnerAddrPtr, op);
    ob.set_u8(kOffRestakeParent, sec.restake_parent ? 1 : 0);
    ob.set_u8(kOffAdmission, static_cast<std::uint8_t>(sec.admission));
    ob.set_u8(kOffManager, static_cast<std::uint8_t>(sec.manager));
    ob.set_u64(kOffThreshold, sec.threshold);
    ob.set_list(kOffValidators, vp.list_off, vp.list_count);
    ob.set_bytes(kOffValNodeIdPool, {node_ids.data(), node_ids.size()});
    ob.set_list(kOffValAddrPool, val_addr_off, val_addr_count);
    wire::set_id(ob, kOffManagerChainId, manager_chain_id);
    ob.set_bytes(kOffManagerAddress, manager_address);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id CreateNetworkTx::parent() const { return wire::read_id(root(), kOffParent); }
Owner CreateNetworkTx::owner() const {
    return wire::read_owner(root(), kOffOwnerThreshold, kOffOwnerLocktime, kOffOwnerAddrPtr);
}
security::Mode CreateNetworkTx::security_mode() const {
    const auto r = root();
    security::Mode m;
    m.restake_parent = r.u8(kOffRestakeParent) != 0;
    m.admission = static_cast<security::Admission>(r.u8(kOffAdmission));
    m.manager = static_cast<security::Manager>(r.u8(kOffManager));
    m.threshold = r.u64(kOffThreshold);
    return m;
}
std::vector<NetworkValidator> CreateNetworkTx::validators() const {
    return wire::read_network_validators(root(), kOffValidators, kOffValNodeIdPool, kOffValAddrPool);
}
Id CreateNetworkTx::manager_chain_id() const { return wire::read_id(root(), kOffManagerChainId); }
std::vector<std::uint8_t> CreateNetworkTx::manager_address() const {
    const auto a = root().bytes(kOffManagerAddress);
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
    zap::Builder b(zap::kHeaderSize + 512 + kSize + static_cast<std::int64_t>(validators.size()) * wire::kNvStride);
    const auto p = wire::write_spending(b, base);
    std::vector<std::uint8_t> node_ids;
    std::vector<ShortId> addr_pool;
    const auto vp = wire::write_network_validators(b, validators, node_ids, addr_pool);
    std::int64_t val_addr_off = 0, val_addr_count = 0;
    if (!addr_pool.empty()) {
        auto alb = b.start_list(wire::kAddrStride);
        for (const auto& a : addr_pool) alb.add_bytes(a.span());
        const auto [alb_off, alb_count] = alb.finish();
        val_addr_off = alb_off;
        val_addr_count = static_cast<std::int64_t>(addr_pool.size());
    }
    const auto ap = wire::write_auth(b, auth);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::ConvertNetwork), base, p);
    wire::set_id(ob, kOffNetwork, network);
    wire::set_id(ob, kOffParent, parent);
    wire::set_id(ob, kOffManagerChainId, manager_chain_id);
    ob.set_bytes(kOffManagerAddress, manager_address);
    ob.set_list(kOffValidators, vp.list_off, vp.list_count);
    ob.set_bytes(kOffValNodeIdPool, {node_ids.data(), node_ids.size()});
    ob.set_list(kOffValAddrPool, val_addr_off, val_addr_count);
    ob.set_list(kOffAuthPtr, ap.off, ap.count);
    ob.set_u8(kOffRestakeParent, sec.restake_parent ? 1 : 0);
    ob.set_u8(kOffAdmission, static_cast<std::uint8_t>(sec.admission));
    ob.set_u8(kOffManager, static_cast<std::uint8_t>(sec.manager));
    ob.set_u64(kOffThreshold, sec.threshold);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id ConvertNetworkTx::network() const { return wire::read_id(root(), kOffNetwork); }
Id ConvertNetworkTx::parent() const { return wire::read_id(root(), kOffParent); }
Id ConvertNetworkTx::manager_chain_id() const { return wire::read_id(root(), kOffManagerChainId); }
std::vector<std::uint8_t> ConvertNetworkTx::manager_address() const {
    const auto a = root().bytes(kOffManagerAddress);
    return std::vector<std::uint8_t>(a.begin(), a.end());
}
std::vector<NetworkValidator> ConvertNetworkTx::validators() const {
    return wire::read_network_validators(root(), kOffValidators, kOffValNodeIdPool, kOffValAddrPool);
}
Auth ConvertNetworkTx::auth() const { return wire::read_auth(root(), kOffAuthPtr); }
security::Mode ConvertNetworkTx::security_mode() const {
    const auto r = root();
    security::Mode m;
    m.restake_parent = r.u8(kOffRestakeParent) != 0;
    m.admission = static_cast<security::Admission>(r.u8(kOffAdmission));
    m.manager = static_cast<security::Manager>(r.u8(kOffManager));
    m.threshold = r.u64(kOffThreshold);
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
    zap::Builder b(zap::kHeaderSize + 512 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto fx = wire::write_id_list(b, fx_ids);
    const auto ap = wire::write_auth(b, chain_auth);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::CreateChain), base, p);
    wire::set_id(ob, kOffChainId, chain_id);
    wire::set_id(ob, kOffVmId, vm_id);
    ob.set_text(kOffName, blockchain_name);
    ob.set_list(kOffFxIds, fx.off, fx.count);
    ob.set_bytes(kOffGenesis, genesis_data);
    ob.set_list(kOffAuth, ap.off, ap.count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id CreateChainTx::chain_id() const { return wire::read_id(root(), kOffChainId); }
Id CreateChainTx::vm_id() const { return wire::read_id(root(), kOffVmId); }
std::string CreateChainTx::blockchain_name() const { return std::string(root().text(kOffName)); }
std::vector<Id> CreateChainTx::fx_ids() const { return wire::read_id_list(root(), kOffFxIds); }
std::vector<std::uint8_t> CreateChainTx::genesis_data() const {
    const auto g = root().bytes(kOffGenesis);
    return std::vector<std::uint8_t>(g.begin(), g.end());
}
Auth CreateChainTx::chain_auth() const { return wire::read_auth(root(), kOffAuth); }

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
    zap::Builder b(zap::kHeaderSize + 512 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto ap = wire::write_auth(b, chain_auth);
    const auto op = wire::write_owner(b, owner);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::TransferChainOwnership), base, p);
    wire::set_id(ob, kOffChain, chain);
    ob.set_list(kOffChainAuth, ap.off, ap.count);
    wire::set_owner(ob, kOffOwnerThreshold, kOffOwnerLocktime, kOffOwnerAddrs, op);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id TransferChainOwnershipTx::chain() const { return wire::read_id(root(), kOffChain); }
Auth TransferChainOwnershipTx::chain_auth() const { return wire::read_auth(root(), kOffChainAuth); }
Owner TransferChainOwnershipTx::owner() const {
    return wire::read_owner(root(), kOffOwnerThreshold, kOffOwnerLocktime, kOffOwnerAddrs);
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
    zap::Builder b(zap::kHeaderSize + 256 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto ap = wire::write_auth(b, chain_auth);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::RemoveChainValidator), base, p);
    wire::set_node_id(ob, kOffNodeId, node_id);
    wire::set_id(ob, kOffChain, chain);
    ob.set_list(kOffChainAuth, ap.off, ap.count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

NodeId RemoveChainValidatorTx::node_id() const { return wire::read_node_id(root(), kOffNodeId); }
Id RemoveChainValidatorTx::chain() const { return wire::read_id(root(), kOffChain); }
Auth RemoveChainValidatorTx::chain_auth() const { return wire::read_auth(root(), kOffChainAuth); }

Status RemoveChainValidatorTx::syntactic_verify(const Runtime& rt) const {
    if (chain() == kPrimaryNetworkId) return fail(Err::RemovePrimaryNetworkValidator);
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    return verify_auth(chain_auth());
}

Status RemoveChainValidatorTx::visit(Visitor& v) const { return v.remove_chain_validator_tx(*this); }

// ── TransformChainTx

Result<std::shared_ptr<TransformChainTx>> TransformChainTx::create(const BaseTx& base, const Params& q,
                                                                    const Auth& chain_auth) {
    zap::Builder b(zap::kHeaderSize + 512 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto ap = wire::write_auth(b, chain_auth);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::TransformChain), base, p);
    wire::set_id(ob, kOffChain, q.chain);
    wire::set_id(ob, kOffAssetId, q.asset_id);
    ob.set_u64(kOffInitialSupply, q.initial_supply);
    ob.set_u64(kOffMaximumSupply, q.maximum_supply);
    ob.set_u64(kOffMinConsumptionRate, q.min_consumption_rate);
    ob.set_u64(kOffMaxConsumptionRate, q.max_consumption_rate);
    ob.set_u64(kOffMinValidatorStake, q.min_validator_stake);
    ob.set_u64(kOffMaxValidatorStake, q.max_validator_stake);
    ob.set_u32(kOffMinStakeDuration, q.min_stake_duration);
    ob.set_u32(kOffMaxStakeDuration, q.max_stake_duration);
    ob.set_u32(kOffMinDelegationFee, q.min_delegation_fee);
    ob.set_u64(kOffMinDelegatorStake, q.min_delegator_stake);
    ob.set_u8(kOffMaxValidatorWeightFactor, q.max_validator_weight_factor);
    ob.set_u32(kOffUptimeRequirement, q.uptime_requirement);
    ob.set_list(kOffChainAuth, ap.off, ap.count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id TransformChainTx::chain() const { return wire::read_id(root(), kOffChain); }
Id TransformChainTx::asset_id() const { return wire::read_id(root(), kOffAssetId); }
std::uint64_t TransformChainTx::initial_supply() const { return root().u64(kOffInitialSupply); }
std::uint64_t TransformChainTx::maximum_supply() const { return root().u64(kOffMaximumSupply); }
std::uint64_t TransformChainTx::min_consumption_rate() const { return root().u64(kOffMinConsumptionRate); }
std::uint64_t TransformChainTx::max_consumption_rate() const { return root().u64(kOffMaxConsumptionRate); }
std::uint64_t TransformChainTx::min_validator_stake() const { return root().u64(kOffMinValidatorStake); }
std::uint64_t TransformChainTx::max_validator_stake() const { return root().u64(kOffMaxValidatorStake); }
std::uint32_t TransformChainTx::min_stake_duration() const { return root().u32(kOffMinStakeDuration); }
std::uint32_t TransformChainTx::max_stake_duration() const { return root().u32(kOffMaxStakeDuration); }
std::uint32_t TransformChainTx::min_delegation_fee() const { return root().u32(kOffMinDelegationFee); }
std::uint64_t TransformChainTx::min_delegator_stake() const { return root().u64(kOffMinDelegatorStake); }
std::uint8_t TransformChainTx::max_validator_weight_factor() const {
    return root().u8(kOffMaxValidatorWeightFactor);
}
std::uint32_t TransformChainTx::uptime_requirement() const { return root().u32(kOffUptimeRequirement); }
Auth TransformChainTx::chain_auth() const { return wire::read_auth(root(), kOffChainAuth); }

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
    zap::Builder b(zap::kHeaderSize + 1024 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto so = wire::write_outputs(b, stake_outs);
    const auto op = wire::write_owner(b, rewards_owner);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::AddValidator), base, p);
    wire::set_validator(ob, kOffValidator, validator);
    ob.set_list(kOffStakeOuts, so.list_off, so.list_count);
    ob.set_list(kOffStakeAddrs, so.addr_off, so.addr_count);
    wire::set_owner(ob, kOffRewardsThreshold, kOffRewardsLocktime, kOffRewardsAddrs, op);
    ob.set_u32(kOffDelegationShares, delegation_shares);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddValidatorTx::validator() const { return wire::read_validator(root(), kOffValidator); }
std::vector<TransferableOutput> AddValidatorTx::stake_outs() const {
    return wire::read_outputs(root(), kOffStakeOuts, kOffStakeAddrs);
}
Owner AddValidatorTx::rewards_owner() const {
    return wire::read_owner(root(), kOffRewardsThreshold, kOffRewardsLocktime, kOffRewardsAddrs);
}
std::uint32_t AddValidatorTx::delegation_shares() const { return root().u32(kOffDelegationShares); }

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
    zap::Builder b(zap::kHeaderSize + 1024 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto so = wire::write_outputs(b, stake_outs);
    const auto op = wire::write_owner(b, rewards_owner);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::AddDelegator), base, p);
    wire::set_validator(ob, kOffValidator, validator);
    ob.set_list(kOffStakeOuts, so.list_off, so.list_count);
    ob.set_list(kOffStakeAddrs, so.addr_off, so.addr_count);
    wire::set_owner(ob, kOffRewardsThreshold, kOffRewardsLocktime, kOffRewardsAddrs, op);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddDelegatorTx::validator() const { return wire::read_validator(root(), kOffValidator); }
std::vector<TransferableOutput> AddDelegatorTx::stake_outs() const {
    return wire::read_outputs(root(), kOffStakeOuts, kOffStakeAddrs);
}
Owner AddDelegatorTx::delegation_rewards_owner() const {
    return wire::read_owner(root(), kOffRewardsThreshold, kOffRewardsLocktime, kOffRewardsAddrs);
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
    zap::Builder b(zap::kHeaderSize + 1024 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto ap = wire::write_auth(b, chain_auth);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::AddChainValidator), base, p);
    wire::set_validator(ob, kOffValidator, validator);
    wire::set_id(ob, kOffChain, chain);
    ob.set_list(kOffChainAuth, ap.off, ap.count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddChainValidatorTx::validator() const { return wire::read_validator(root(), kOffValidator); }
Id AddChainValidatorTx::chain() const { return wire::read_id(root(), kOffChain); }
Auth AddChainValidatorTx::chain_auth() const { return wire::read_auth(root(), kOffChainAuth); }

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
    zap::Builder b(zap::kHeaderSize + 1024 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto so = wire::write_outputs(b, stake_outs);
    const auto vop = wire::write_owner(b, validator_rewards_owner);
    const auto dop = wire::write_owner(b, delegator_rewards_owner);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::AddPermissionlessValidator), base, p);
    wire::set_validator(ob, kOffValidator, validator);
    wire::set_id(ob, kOffChain, chain);
    wire::set_signer(ob, kOffSigner, sig);
    ob.set_list(kOffStakeOuts, so.list_off, so.list_count);
    ob.set_list(kOffStakeAddrs, so.addr_off, so.addr_count);
    wire::set_owner(ob, kOffValRewardsThreshold, kOffValRewardsLocktime, kOffValRewardsAddrs, vop);
    wire::set_owner(ob, kOffDelRewardsThreshold, kOffDelRewardsLocktime, kOffDelRewardsAddrs, dop);
    ob.set_u32(kOffDelegationShares, delegation_shares);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddPermissionlessValidatorTx::validator() const {
    return wire::read_validator(root(), kOffValidator);
}
Id AddPermissionlessValidatorTx::chain() const { return wire::read_id(root(), kOffChain); }
signer::Signer AddPermissionlessValidatorTx::signer_value() const {
    return wire::read_signer(root(), kOffSigner);
}
std::vector<TransferableOutput> AddPermissionlessValidatorTx::stake_outs() const {
    return wire::read_outputs(root(), kOffStakeOuts, kOffStakeAddrs);
}
Owner AddPermissionlessValidatorTx::validator_rewards_owner() const {
    return wire::read_owner(root(), kOffValRewardsThreshold, kOffValRewardsLocktime, kOffValRewardsAddrs);
}
Owner AddPermissionlessValidatorTx::delegator_rewards_owner() const {
    return wire::read_owner(root(), kOffDelRewardsThreshold, kOffDelRewardsLocktime, kOffDelRewardsAddrs);
}
std::uint32_t AddPermissionlessValidatorTx::delegation_shares() const {
    return root().u32(kOffDelegationShares);
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
    zap::Builder b(zap::kHeaderSize + 1024 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto so = wire::write_outputs(b, stake_outs);
    const auto op = wire::write_owner(b, delegation_rewards_owner);

    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::AddPermissionlessDelegator), base, p);
    wire::set_validator(ob, kOffValidator, validator);
    wire::set_id(ob, kOffChain, chain);
    ob.set_list(kOffStakeOuts, so.list_off, so.list_count);
    ob.set_list(kOffStakeAddrs, so.addr_off, so.addr_count);
    wire::set_owner(ob, kOffRewardsThreshold, kOffRewardsLocktime, kOffRewardsAddrs, op);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Validator AddPermissionlessDelegatorTx::validator() const {
    return wire::read_validator(root(), kOffValidator);
}
Id AddPermissionlessDelegatorTx::chain() const { return wire::read_id(root(), kOffChain); }
std::vector<TransferableOutput> AddPermissionlessDelegatorTx::stake_outs() const {
    return wire::read_outputs(root(), kOffStakeOuts, kOffStakeAddrs);
}
Owner AddPermissionlessDelegatorTx::delegation_rewards_owner() const {
    return wire::read_owner(root(), kOffRewardsThreshold, kOffRewardsLocktime, kOffRewardsAddrs);
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
    zap::Builder b(zap::kHeaderSize + 512 + kSize);
    const auto p = wire::write_spending(b, base);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::RegisterL1Validator), base, p);
    ob.set_u64(kOffBalance, balance);
    ob.set_bytes_fixed(kOffPop, {proof_of_possession.data(), proof_of_possession.size()});
    ob.set_bytes(kOffMessage, message);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

std::uint64_t RegisterL1ValidatorTx::balance() const { return root().u64(kOffBalance); }
signer::SignatureBytes RegisterL1ValidatorTx::proof_of_possession() const {
    signer::SignatureBytes pop{};
    const auto b = root().bytes_fixed(kOffPop, kBlsSigLen);
    if (b.size() == pop.size()) std::memcpy(pop.data(), b.data(), b.size());
    return pop;
}
std::vector<std::uint8_t> RegisterL1ValidatorTx::message() const {
    const auto m = root().bytes(kOffMessage);
    return std::vector<std::uint8_t>(m.begin(), m.end());
}

Status RegisterL1ValidatorTx::syntactic_verify(const Runtime& rt) const {
    return verify_base_tx(base_tx(), rt);
}
Status RegisterL1ValidatorTx::visit(Visitor& v) const { return v.register_l1_validator_tx(*this); }

// ── SetL1ValidatorWeightTx

Result<std::shared_ptr<SetL1ValidatorWeightTx>> SetL1ValidatorWeightTx::create(
    const BaseTx& base, std::span<const std::uint8_t> message) {
    zap::Builder b(zap::kHeaderSize + 256 + kSize);
    const auto p = wire::write_spending(b, base);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::SetL1ValidatorWeight), base, p);
    ob.set_bytes(kOffMessage, message);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

std::vector<std::uint8_t> SetL1ValidatorWeightTx::message() const {
    const auto m = root().bytes(kOffMessage);
    return std::vector<std::uint8_t>(m.begin(), m.end());
}

Status SetL1ValidatorWeightTx::syntactic_verify(const Runtime& rt) const {
    return verify_base_tx(base_tx(), rt);
}
Status SetL1ValidatorWeightTx::visit(Visitor& v) const { return v.set_l1_validator_weight_tx(*this); }

// ── IncreaseL1ValidatorBalanceTx

Result<std::shared_ptr<IncreaseL1ValidatorBalanceTx>> IncreaseL1ValidatorBalanceTx::create(
    const BaseTx& base, const Id& validation_id, std::uint64_t balance) {
    zap::Builder b(zap::kHeaderSize + 256 + kSize);
    const auto p = wire::write_spending(b, base);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::IncreaseL1ValidatorBalance), base, p);
    wire::set_id(ob, kOffValidationId, validation_id);
    ob.set_u64(kOffBalance, balance);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id IncreaseL1ValidatorBalanceTx::validation_id() const { return wire::read_id(root(), kOffValidationId); }
std::uint64_t IncreaseL1ValidatorBalanceTx::balance() const { return root().u64(kOffBalance); }

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
    zap::Builder b(zap::kHeaderSize + 256 + kSize);
    const auto p = wire::write_spending(b, base);
    const auto ap = wire::write_auth(b, disable_auth);
    auto ob = b.start_object(kSize);
    wire::set_envelope(ob, static_cast<std::uint8_t>(Kind::DisableL1Validator), base, p);
    wire::set_id(ob, kOffValidationId, validation_id);
    ob.set_list(kOffAuth, ap.off, ap.count);
    ob.finish_as_root();
    auto buf = finish(b);
    if (!parses(buf)) return fail(Err::BufferTooSmall);
    return wrap(buf);
}

Id DisableL1ValidatorTx::validation_id() const { return wire::read_id(root(), kOffValidationId); }
Auth DisableL1ValidatorTx::disable_auth() const { return wire::read_auth(root(), kOffAuth); }

Status DisableL1ValidatorTx::syntactic_verify(const Runtime& rt) const {
    if (auto s = verify_base_tx(base_tx(), rt); !s) return s;
    return verify_auth(disable_auth());
}
Status DisableL1ValidatorTx::visit(Visitor& v) const { return v.disable_l1_validator_tx(*this); }

// ── RewardValidatorTx

std::shared_ptr<RewardValidatorTx> RewardValidatorTx::create(const Id& tx_id) {
    zap::Builder b(zap::kHeaderSize + 16 + kSize);
    auto ob = b.start_object(kSize);
    ob.set_u8(kOffKind, static_cast<std::uint8_t>(Kind::RewardValidator));
    ob.set_bytes_fixed(kOffTxId, tx_id.span());
    ob.finish_as_root();
    return wrap(finish(b));
}

Id RewardValidatorTx::tx_id() const { return wire::read_id(root(), kOffTxId); }
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

// ── the owner encoding, standalone

namespace {
constexpr std::int64_t kOwnerObjThreshold = 0;
constexpr std::int64_t kOwnerObjLocktime = 4;
constexpr std::int64_t kOwnerObjAddrPtr = 12;
constexpr std::int64_t kOwnerObjSize = 20;
}  // namespace

std::vector<std::uint8_t> marshal_owner(const Owner& o) {
    zap::Builder b(zap::kHeaderSize + 128);
    const auto p = wire::write_owner(b, o);
    auto ob = b.start_object(kOwnerObjSize);
    wire::set_owner(ob, kOwnerObjThreshold, kOwnerObjLocktime, kOwnerObjAddrPtr, p);
    ob.finish_as_root();
    return b.finish();
}

Result<Owner> unmarshal_owner(std::span<const std::uint8_t> b) {
    const auto m = zap::Message::parse(b);
    if (!m) return fail(Err::BufferTooSmall);
    return wire::read_owner(m->root(), kOwnerObjThreshold, kOwnerObjLocktime, kOwnerObjAddrPtr);
}

}  // namespace lux::platformvm::txs
