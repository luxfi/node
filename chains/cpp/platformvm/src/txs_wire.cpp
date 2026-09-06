// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_wire.cpp — the shared spending envelope and the reusable delta fields.
//
// Rendered from Go vms/platformvm/txs/spending.go and delta.go. Every
// non-proposal transaction is this envelope plus its own fields, so the
// envelope is written once and composed, never restated per type.
//
// Envelope fixed section (77 bytes):
//
//   kind          u8  @ 0
//   NetworkID     u32 @ 1
//   BlockchainID  32B @ 5
//   Outs          8B  @ 37   list ptr
//   OwnerAddrs    8B  @ 45   shared owner-address array
//   Ins           8B  @ 53   list ptr
//   SigIndices    8B  @ 61   shared input sig-index array
//   Memo          8B  @ 69   bytes ptr
//
// Outputs and inputs are FIXED-STRIDE records with their variable parts pooled
// into one shared array each. That is what makes a transaction readable without
// walking it: an output entry says where its owner addresses start and how many
// there are, and the slicer clamps both against the array it actually has.

#include "lux/platformvm/txs_wire.hpp"

namespace lux::platformvm::txs::wire {

namespace {
void put_u32(std::uint8_t* p, std::uint32_t v) { zap::store_u32(p, v); }
void put_u64(std::uint8_t* p, std::uint64_t v) { zap::store_u64(p, v); }
}  // namespace

Id read_id(const zap::Object& o, std::int64_t off) {
    return Id::from(o.bytes_fixed(off, static_cast<std::int64_t>(kIdLen)));
}

NodeId read_node_id(const zap::Object& o, std::int64_t off) {
    return NodeId::from(o.bytes_fixed(off, static_cast<std::int64_t>(kNodeIdLen)));
}

void set_id(zap::ObjectBuilder& ob, std::int64_t off, const Id& id) { ob.set_bytes_fixed(off, id.span()); }
void set_node_id(zap::ObjectBuilder& ob, std::int64_t off, const NodeId& id) {
    ob.set_bytes_fixed(off, id.span());
}

std::vector<ShortId> slice_addrs(const zap::List& arr, std::uint32_t start, std::uint32_t count) {
    const std::uint32_t total = static_cast<std::uint32_t>(arr.size());
    if (count == 0 || start > total || count > total - start) return {};
    std::vector<ShortId> out(count);
    for (std::uint32_t i = 0; i < count; ++i) {
        const auto o = arr.object(static_cast<int>(start + i), kAddrStride);
        for (int j = 0; j < kAddrStride; ++j) out[i].b[j] = o.u8(j);
    }
    return out;
}

std::vector<std::uint32_t> slice_sigs(const zap::List& arr, std::uint32_t start, std::uint32_t count) {
    const std::uint32_t total = static_cast<std::uint32_t>(arr.size());
    if (count == 0 || start > total || count > total - start) return {};
    std::vector<std::uint32_t> out(count);
    for (std::uint32_t i = 0; i < count; ++i) out[i] = arr.u32(static_cast<int>(start + i));
    return out;
}

OutListPtrs write_outputs(zap::Builder& b, const std::vector<TransferableOutput>& outs) {
    OutListPtrs p;
    if (outs.empty()) return p;
    std::vector<ShortId> addrs;
    auto lb = b.start_list(kOutStride);
    for (const auto& o : outs) {
        std::uint8_t e[kOutStride] = {};
        std::memcpy(e + kOutAssetId, o.asset.b.data(), kIdLen);
        put_u64(e + kOutStakeLock, o.stake_lock);
        put_u64(e + kOutAmount, o.out.amt);
        put_u32(e + kOutThreshold, o.out.owners.threshold);
        put_u64(e + kOutOwnerLock, o.out.owners.locktime);
        put_u32(e + kOutAddrStart, static_cast<std::uint32_t>(addrs.size()));
        put_u32(e + kOutAddrCount, static_cast<std::uint32_t>(o.out.owners.addrs.size()));
        lb.add_bytes({e, kOutStride});
        addrs.insert(addrs.end(), o.out.owners.addrs.begin(), o.out.owners.addrs.end());
    }
    // add_bytes counts BYTES; the list length on the wire is the element count.
    const auto [lb_off, lb_count] = lb.finish();
    p.list_off = lb_off;
    p.list_count = static_cast<std::int64_t>(outs.size());
    if (!addrs.empty()) {
        auto alb = b.start_list(kAddrStride);
        for (const auto& a : addrs) alb.add_bytes(a.span());
        const auto [alb_off, _] = alb.finish();
        p.addr_off = alb_off;
        p.addr_count = static_cast<std::int64_t>(addrs.size());
    }
    return p;
}

InListPtrs write_inputs(zap::Builder& b, const std::vector<TransferableInput>& ins) {
    InListPtrs p;
    if (ins.empty()) return p;
    std::vector<std::uint32_t> sigs;
    auto lb = b.start_list(kInStride);
    for (const auto& in : ins) {
        std::uint8_t e[kInStride] = {};
        std::memcpy(e + kInTxId, in.utxo.tx_id.b.data(), kIdLen);
        put_u32(e + kInOutputIndex, in.utxo.output_index);
        std::memcpy(e + kInAssetId, in.asset.b.data(), kIdLen);
        put_u64(e + kInStakeLock, in.stake_lock);
        put_u64(e + kInAmount, in.in.amt);
        put_u32(e + kInSigStart, static_cast<std::uint32_t>(sigs.size()));
        put_u32(e + kInSigCount, static_cast<std::uint32_t>(in.in.sig_indices.size()));
        lb.add_bytes({e, kInStride});
        sigs.insert(sigs.end(), in.in.sig_indices.begin(), in.in.sig_indices.end());
    }
    const auto [lb_off, lb_count] = lb.finish();
    p.list_off = lb_off;
    p.list_count = static_cast<std::int64_t>(ins.size());
    if (!sigs.empty()) {
        auto slb = b.start_list(kSigStride);
        for (auto s : sigs) slb.add_u32(s);
        const auto [slb_off, slb_count] = slb.finish();
        p.sig_off = slb_off;
        p.sig_count = slb_count;
    }
    return p;
}

SpendPtrs write_spending(zap::Builder& b, const BaseTx& base) {
    SpendPtrs p;
    const auto o = write_outputs(b, base.outs);
    p.outs_off = o.list_off;
    p.outs_count = o.list_count;
    p.addr_off = o.addr_off;
    p.addr_count = o.addr_count;
    const auto i = write_inputs(b, base.ins);
    p.ins_off = i.list_off;
    p.ins_count = i.list_count;
    p.sig_off = i.sig_off;
    p.sig_count = i.sig_count;
    return p;
}

void set_envelope(zap::ObjectBuilder& ob, std::uint8_t kind, const BaseTx& base, const SpendPtrs& p) {
    ob.set_u8(kOffKind, kind);
    ob.set_u32(kOffNetworkId, base.network_id);
    ob.set_bytes_fixed(kOffBlockchainId, base.blockchain_id.span());
    ob.set_list(kOffOuts, p.outs_off, p.outs_count);
    ob.set_list(kOffOwnerAddrs, p.addr_off, p.addr_count);
    ob.set_list(kOffIns, p.ins_off, p.ins_count);
    ob.set_list(kOffSigIndices, p.sig_off, p.sig_count);
    ob.set_bytes(kOffMemo, {base.memo.data(), base.memo.size()});
}

std::vector<TransferableOutput> read_outputs(const zap::Object& obj, std::int64_t list_off,
                                             std::int64_t addr_off) {
    const auto list = obj.list_stride(list_off, kOutStride);
    const auto addrs = obj.list_stride(addr_off, kAddrStride);
    const int n = list.size();
    if (n == 0) return {};
    std::vector<TransferableOutput> outs(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const auto e = list.object(i, kOutStride);
        auto& o = outs[static_cast<std::size_t>(i)];
        o.asset = read_id(e, kOutAssetId);
        o.stake_lock = e.u64(kOutStakeLock);
        o.out.amt = e.u64(kOutAmount);
        o.out.owners.threshold = e.u32(kOutThreshold);
        o.out.owners.locktime = e.u64(kOutOwnerLock);
        o.out.owners.addrs = slice_addrs(addrs, e.u32(kOutAddrStart), e.u32(kOutAddrCount));
    }
    return outs;
}

std::vector<TransferableInput> read_inputs(const zap::Object& obj, std::int64_t list_off,
                                           std::int64_t sig_off) {
    const auto list = obj.list_stride(list_off, kInStride);
    const auto sigs = obj.list_stride(sig_off, kSigStride);
    const int n = list.size();
    if (n == 0) return {};
    std::vector<TransferableInput> ins(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const auto e = list.object(i, kInStride);
        auto& in = ins[static_cast<std::size_t>(i)];
        in.utxo.tx_id = read_id(e, kInTxId);
        in.utxo.output_index = e.u32(kInOutputIndex);
        in.asset = read_id(e, kInAssetId);
        in.stake_lock = e.u64(kInStakeLock);
        in.in.amt = e.u64(kInAmount);
        in.in.sig_indices = slice_sigs(sigs, e.u32(kInSigStart), e.u32(kInSigCount));
    }
    return ins;
}

// ── owner

OwnerPtrs write_owner(zap::Builder& b, const Owner& o) {
    OwnerPtrs p;
    p.threshold = o.threshold;
    p.locktime = o.locktime;
    if (!o.addrs.empty()) {
        auto lb = b.start_list(kAddrStride);
        for (const auto& a : o.addrs) lb.add_bytes(a.span());
        const auto [lb_off, lb_count] = lb.finish();
        p.addr_off = lb_off;
        p.addr_count = static_cast<std::int64_t>(o.addrs.size());
    }
    return p;
}

void set_owner(zap::ObjectBuilder& ob, std::int64_t threshold_off, std::int64_t locktime_off,
               std::int64_t addr_ptr_off, const OwnerPtrs& p) {
    ob.set_u32(threshold_off, p.threshold);
    ob.set_u64(locktime_off, p.locktime);
    ob.set_list(addr_ptr_off, p.addr_off, p.addr_count);
}

Owner read_owner(const zap::Object& obj, std::int64_t threshold_off, std::int64_t locktime_off,
                 std::int64_t addr_ptr_off) {
    const auto arr = obj.list_stride(addr_ptr_off, kAddrStride);
    Owner o;
    o.locktime = obj.u64(locktime_off);
    o.threshold = obj.u32(threshold_off);
    o.addrs = slice_addrs(arr, 0, static_cast<std::uint32_t>(arr.size()));
    return o;
}

// ── auth

AuthPtrs write_auth(zap::Builder& b, const Auth& a) {
    AuthPtrs p;
    if (a.empty()) return p;
    auto lb = b.start_list(kSigStride);
    for (auto s : a) lb.add_u32(s);
    const auto [lb_off, lb_count] = lb.finish();
    p.off = lb_off;
    p.count = lb_count;
    return p;
}

Auth read_auth(const zap::Object& obj, std::int64_t ptr_off) {
    const auto arr = obj.list_stride(ptr_off, kSigStride);
    return slice_sigs(arr, 0, static_cast<std::uint32_t>(arr.size()));
}

// ── inline validator (44B) and signer (145B)

void set_validator(zap::ObjectBuilder& ob, std::int64_t off, const Validator& v) {
    set_node_id(ob, off, v.node_id);
    ob.set_u64(off + 20, v.start);
    ob.set_u64(off + 28, v.end);
    ob.set_u64(off + 36, v.weight);
}

Validator read_validator(const zap::Object& obj, std::int64_t off) {
    Validator v;
    v.node_id = read_node_id(obj, off);
    v.start = obj.u64(off + 20);
    v.end = obj.u64(off + 28);
    v.weight = obj.u64(off + 36);
    return v;
}

void set_signer(zap::ObjectBuilder& ob, std::int64_t off, const signer::Signer& s) {
    if (const auto* p = std::get_if<signer::ProofOfPossession>(&s)) {
        ob.set_u8(off, 1);
        ob.set_bytes_fixed(off + 1, {p->public_key.data(), p->public_key.size()});
        ob.set_bytes_fixed(off + 1 + kBlsPubLen, {p->proof.data(), p->proof.size()});
    } else {
        ob.set_u8(off, 0);
    }
}

signer::Signer read_signer(const zap::Object& obj, std::int64_t off) {
    if (obj.u8(off) == 0) return signer::Empty{};
    signer::ProofOfPossession p;
    const auto pk = obj.bytes_fixed(off + 1, kBlsPubLen);
    const auto sig = obj.bytes_fixed(off + 1 + kBlsPubLen, kBlsSigLen);
    if (pk.size() == p.public_key.size()) std::memcpy(p.public_key.data(), pk.data(), pk.size());
    if (sig.size() == p.proof.size()) std::memcpy(p.proof.data(), sig.data(), sig.size());
    return p;
}

// ── id list

IdListPtrs write_id_list(zap::Builder& b, const std::vector<Id>& list) {
    IdListPtrs p;
    if (list.empty()) return p;
    auto lb = b.start_list(static_cast<std::int64_t>(kIdLen));
    for (const auto& id : list) lb.add_bytes(id.span());
    const auto [lb_off, lb_count] = lb.finish();
    p.off = lb_off;
    p.count = static_cast<std::int64_t>(list.size());
    return p;
}

std::vector<Id> read_id_list(const zap::Object& obj, std::int64_t ptr_off) {
    const auto l = obj.list_stride(ptr_off, static_cast<std::uint32_t>(kIdLen));
    const int n = l.size();
    if (n == 0) return {};
    std::vector<Id> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) out[static_cast<std::size_t>(i)] = read_id(l.object(i, kIdLen), 0);
    return out;
}

// ── the network-validator list, shared by CreateNetworkTx and ConvertNetworkTx

NetworkValidatorPtrs write_network_validators(zap::Builder& b, const std::vector<NetworkValidator>& vdrs,
                                              std::vector<std::uint8_t>& node_ids,
                                              std::vector<ShortId>& addrs) {
    NetworkValidatorPtrs p;
    if (vdrs.empty()) return p;
    auto lb = b.start_list(kNvStride);
    for (const auto& v : vdrs) {
        std::uint8_t e[kNvStride] = {};
        put_u64(e + kNvWeight, v.weight);
        put_u64(e + kNvBalance, v.balance);
        std::memcpy(e + kNvSignerPub, v.pop.public_key.data(), v.pop.public_key.size());
        std::memcpy(e + kNvSignerPop, v.pop.proof.data(), v.pop.proof.size());
        put_u32(e + kNvNodeIdStart, static_cast<std::uint32_t>(node_ids.size()));
        put_u32(e + kNvNodeIdLen, static_cast<std::uint32_t>(v.node_id.size()));
        node_ids.insert(node_ids.end(), v.node_id.begin(), v.node_id.end());
        put_u32(e + kNvRemThreshold, v.remaining_balance_owner.threshold);
        put_u32(e + kNvRemAddrStart, static_cast<std::uint32_t>(addrs.size()));
        put_u32(e + kNvRemAddrCount, static_cast<std::uint32_t>(v.remaining_balance_owner.addresses.size()));
        addrs.insert(addrs.end(), v.remaining_balance_owner.addresses.begin(),
                     v.remaining_balance_owner.addresses.end());
        put_u32(e + kNvDeacThreshold, v.deactivation_owner.threshold);
        put_u32(e + kNvDeacAddrStart, static_cast<std::uint32_t>(addrs.size()));
        put_u32(e + kNvDeacAddrCount, static_cast<std::uint32_t>(v.deactivation_owner.addresses.size()));
        addrs.insert(addrs.end(), v.deactivation_owner.addresses.begin(),
                     v.deactivation_owner.addresses.end());
        lb.add_bytes({e, kNvStride});
    }
    const auto [lb_off, lb_count] = lb.finish();
    p.list_off = lb_off;
    p.list_count = static_cast<std::int64_t>(vdrs.size());
    return p;
}

std::vector<NetworkValidator> read_network_validators(const zap::Object& obj, std::int64_t list_off,
                                                      std::int64_t node_id_pool_off,
                                                      std::int64_t addr_pool_off) {
    const auto list = obj.list_stride(list_off, kNvStride);
    const int n = list.size();
    if (n == 0) return {};
    const auto node_blob = obj.bytes(node_id_pool_off);
    const auto addrs = obj.list_stride(addr_pool_off, kAddrStride);
    std::vector<NetworkValidator> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const auto e = list.object(i, kNvStride);
        auto& v = out[static_cast<std::size_t>(i)];
        v.weight = e.u64(kNvWeight);
        v.balance = e.u64(kNvBalance);
        const auto pub = e.bytes_fixed(kNvSignerPub, kBlsPubLen);
        if (pub.size() == v.pop.public_key.size())
            std::memcpy(v.pop.public_key.data(), pub.data(), pub.size());
        const auto pop = e.bytes_fixed(kNvSignerPop, kBlsSigLen);
        if (pop.size() == v.pop.proof.size()) std::memcpy(v.pop.proof.data(), pop.data(), pop.size());
        const std::uint32_t ns = e.u32(kNvNodeIdStart);
        const std::uint32_t nl = e.u32(kNvNodeIdLen);
        if (nl > 0 && static_cast<std::size_t>(ns) + nl <= node_blob.size())
            v.node_id.assign(node_blob.begin() + ns, node_blob.begin() + ns + nl);
        v.remaining_balance_owner.threshold = e.u32(kNvRemThreshold);
        v.remaining_balance_owner.addresses = slice_addrs(addrs, e.u32(kNvRemAddrStart), e.u32(kNvRemAddrCount));
        v.deactivation_owner.threshold = e.u32(kNvDeacThreshold);
        v.deactivation_owner.addresses = slice_addrs(addrs, e.u32(kNvDeacAddrStart), e.u32(kNvDeacAddrCount));
    }
    return out;
}

}  // namespace lux::platformvm::txs::wire
