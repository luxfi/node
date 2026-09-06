// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_wire.cpp — the arrangement, and only the arrangement.
//
// The format is gen/wire_zap.hpp, printed from schema/wire.zap. What is here
// is what this chain decided to do with it: outputs and inputs are FIXED-STRIDE
// records with their variable parts pooled into one shared run each, so an
// output entry says where its owner addresses start and how many there are
// rather than carrying them. Reading slices that run; writing fills it.

#include "lux/platformvm/txs_wire.hpp"

#include <cstring>

namespace lux::platformvm::wire {
namespace {

// A run named by a record is clamped to the run that is there. A record
// claiming more than the array holds has already lied; it is given nothing
// rather than someone else's addresses.
bool run_fits(std::uint32_t start, std::uint32_t count, std::uint32_t total) {
    return count != 0 && start <= total && count <= total - start;
}

std::vector<ShortId> slice_addrs(const zap::List& pool, std::uint32_t start, std::uint32_t count) {
    const auto total = static_cast<std::uint32_t>(pool.size());
    if (!run_fits(start, count, total)) return {};
    std::vector<ShortId> out(count);
    for (std::uint32_t i = 0; i < count; ++i) {
        const auto o = pool.object(static_cast<int>(start + i), kShortIdLen);
        out[i] = ShortId::from(o.bytes_fixed(0, static_cast<std::int64_t>(kShortIdLen)));
    }
    return out;
}

std::vector<std::uint32_t> slice_sigs(const zap::List& pool, std::uint32_t start, std::uint32_t count) {
    const auto total = static_cast<std::uint32_t>(pool.size());
    if (!run_fits(start, count, total)) return {};
    std::vector<std::uint32_t> out(count);
    for (std::uint32_t i = 0; i < count; ++i) out[i] = pool.u32(static_cast<int>(start + i));
    return out;
}

}  // namespace

std::vector<Addr> addrs(const std::vector<ShortId>& in) {
    std::vector<Addr> out;
    out.reserve(in.size());
    for (const auto& a : in) out.push_back(a.b);
    return out;
}

OutRun outs(const std::vector<TransferableOutput>& in) {
    OutRun r;
    r.list.reserve(in.size());
    for (const auto& o : in) {
        r.list.push_back(OutInput{
            .Asset = o.asset.b,
            .StakeLock = o.stake_lock,
            .Amount = o.out.amt,
            .Threshold = o.out.owners.threshold,
            .OwnerLock = o.out.owners.locktime,
            .AddrStart = static_cast<std::uint32_t>(r.addrs.size()),
            .AddrCount = static_cast<std::uint32_t>(o.out.owners.addrs.size()),
        });
        for (const auto& a : o.out.owners.addrs) r.addrs.push_back(a.b);
    }
    return r;
}

InRun ins(const std::vector<TransferableInput>& in) {
    InRun r;
    r.list.reserve(in.size());
    for (const auto& i : in) {
        r.list.push_back(InInput{
            .TxID = i.utxo.tx_id.b,
            .Index = i.utxo.output_index,
            .Asset = i.asset.b,
            .StakeLock = i.stake_lock,
            .Amount = i.in.amt,
            .SigStart = static_cast<std::uint32_t>(r.sigs.size()),
            .SigCount = static_cast<std::uint32_t>(i.in.sig_indices.size()),
        });
        r.sigs.insert(r.sigs.end(), i.in.sig_indices.begin(), i.in.sig_indices.end());
    }
    return r;
}

ValidatorRun validators(const std::vector<txs::NetworkValidator>& in) {
    ValidatorRun r;
    r.list.reserve(in.size());
    for (const auto& v : in) {
        NetworkValidatorInput e{};
        e.Weight = v.weight;
        e.Balance = v.balance;
        e.SignerKey = v.pop.public_key;
        e.SignerProof = v.pop.proof;
        e.NodeIDStart = static_cast<std::uint32_t>(r.node_ids.size());
        e.NodeIDLen = static_cast<std::uint32_t>(v.node_id.size());
        r.node_ids.insert(r.node_ids.end(), v.node_id.begin(), v.node_id.end());
        e.RemoveThreshold = v.remaining_balance_owner.threshold;
        e.RemoveAddrStart = static_cast<std::uint32_t>(r.addrs.size());
        e.RemoveAddrCount = static_cast<std::uint32_t>(v.remaining_balance_owner.addresses.size());
        for (const auto& a : v.remaining_balance_owner.addresses) r.addrs.push_back(a.b);
        e.DisableThreshold = v.deactivation_owner.threshold;
        e.DisableAddrStart = static_cast<std::uint32_t>(r.addrs.size());
        e.DisableAddrCount = static_cast<std::uint32_t>(v.deactivation_owner.addresses.size());
        for (const auto& a : v.deactivation_owner.addresses) r.addrs.push_back(a.b);
        r.list.push_back(e);
    }
    return r;
}

Spend spend(const BaseTx& base) {
    auto o = outs(base.outs);
    auto i = ins(base.ins);
    return Spend{std::move(o.list), std::move(o.addrs), std::move(i.list), std::move(i.sigs)};
}

std::vector<TransferableOutput> outs(const zap::List& list, const zap::List& pool) {
    const int n = list.size();
    if (n == 0) return {};
    std::vector<TransferableOutput> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const Out e(list.object(i, kOutSize));
        auto& o = out[static_cast<std::size_t>(i)];
        o.asset = Id::from(e.Asset());
        o.stake_lock = e.StakeLock();
        o.out.amt = e.Amount();
        o.out.owners.threshold = e.Threshold();
        o.out.owners.locktime = e.OwnerLock();
        o.out.owners.addrs = slice_addrs(pool, e.AddrStart(), e.AddrCount());
    }
    return out;
}

std::vector<TransferableInput> ins(const zap::List& list, const zap::List& pool) {
    const int n = list.size();
    if (n == 0) return {};
    std::vector<TransferableInput> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const In e(list.object(i, kInSize));
        auto& in = out[static_cast<std::size_t>(i)];
        in.utxo.tx_id = Id::from(e.TxID());
        in.utxo.output_index = e.Index();
        in.asset = Id::from(e.Asset());
        in.stake_lock = e.StakeLock();
        in.in.amt = e.Amount();
        in.in.sig_indices = slice_sigs(pool, e.SigStart(), e.SigCount());
    }
    return out;
}

std::vector<txs::NetworkValidator> validators(const zap::List& list, std::span<const std::uint8_t> node_ids,
                                              const zap::List& pool) {
    const int n = list.size();
    if (n == 0) return {};
    std::vector<txs::NetworkValidator> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const NetworkValidator e(list.object(i, kNetworkValidatorSize));
        auto& v = out[static_cast<std::size_t>(i)];
        v.weight = e.Weight();
        v.balance = e.Balance();
        const auto key = e.SignerKey();
        std::memcpy(v.pop.public_key.data(), key.data(), v.pop.public_key.size());
        const auto proof = e.SignerProof();
        std::memcpy(v.pop.proof.data(), proof.data(), v.pop.proof.size());
        const std::uint32_t start = e.NodeIDStart();
        const std::uint32_t len = e.NodeIDLen();
        if (run_fits(start, len, static_cast<std::uint32_t>(node_ids.size())))
            v.node_id.assign(node_ids.begin() + start, node_ids.begin() + start + len);
        v.remaining_balance_owner.threshold = e.RemoveThreshold();
        v.remaining_balance_owner.addresses = slice_addrs(pool, e.RemoveAddrStart(), e.RemoveAddrCount());
        v.deactivation_owner.threshold = e.DisableThreshold();
        v.deactivation_owner.addresses = slice_addrs(pool, e.DisableAddrStart(), e.DisableAddrCount());
    }
    return out;
}

txs::Owner owner(std::uint32_t threshold, std::uint64_t locktime, const zap::List& pool) {
    txs::Owner o;
    o.locktime = locktime;
    o.threshold = threshold;
    o.addrs = slice_addrs(pool, 0, static_cast<std::uint32_t>(pool.size()));
    return o;
}

txs::Auth auth(const zap::List& pool) {
    return slice_sigs(pool, 0, static_cast<std::uint32_t>(pool.size()));
}

txs::Validator validator(std::span<const std::uint8_t> node_id, std::uint64_t start, std::uint64_t end,
                         std::uint64_t weight) {
    return txs::Validator{NodeId::from(node_id), start, end, weight};
}

}  // namespace lux::platformvm::wire
