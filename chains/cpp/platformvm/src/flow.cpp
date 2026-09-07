// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// flow.cpp — the value-conservation walk.
//
// Rendered from Go vms/platformvm/utxo/verifier.go, step for step and refusal
// for refusal.

#include "lux/platformvm/flow.hpp"

#include "lux/platformvm/safemath.hpp"

namespace lux::platformvm::flow {
namespace {

// asset → locktime → owner → amount.
using LockedLedger = std::map<Id, std::map<std::uint64_t, std::map<Id, std::uint64_t>>>;

// The owner's stable name: sha256 of the ONE canonical owner encoding, which is
// the same layout a transaction carries. Two spellings of an owner would be two
// owners, so there is only one spelling.
Id owner_id(const OutputOwners& o) { return sha256(txs::marshal_owner(o)); }

Status accumulate(std::uint64_t& slot, std::uint64_t amount) {
    auto sum = add64(slot, amount);
    if (!sum) return std::unexpected(sum.error());
    slot = sum.value();
    return ok();
}

}  // namespace

Status verify_spend_utxos(const fx::Fx& f, std::span<const std::uint8_t> tx_bytes,
                          const std::vector<UTXO>& utxos, const std::vector<TransferableInput>& ins,
                          const std::vector<TransferableOutput>& outs,
                          const std::vector<txs::Credential>& creds, Produced unlocked_produced,
                          std::uint64_t now) {
    if (ins.size() != creds.size())
        return fail(Err::WrongNumberCredentials, std::to_string(ins.size()) + " inputs != " +
                                                     std::to_string(creds.size()) + " credentials");
    if (ins.size() != utxos.size())
        return fail(Err::WrongNumberUTXOs,
                    std::to_string(ins.size()) + " inputs != " + std::to_string(utxos.size()) + " utxos");

    Produced unlocked_consumed;
    LockedLedger locked_produced;
    LockedLedger locked_consumed;

    for (std::size_t i = 0; i < ins.size(); ++i) {
        const TransferableInput& in = ins[i];
        const UTXO& utxo = utxos[i];

        if (!(utxo.asset == in.asset))
            return fail(Err::AssetIDMismatch, hex(in.asset) + " != " + hex(utxo.asset));

        const std::uint64_t locktime = utxo.stake_lock;
        // The UTXO says it is locked until `locktime` and this input, which
        // consumes it, does not say so. Refuse: an input that does not carry the
        // lock is an input that intends to drop it.
        if (now < locktime && in.stake_lock == 0) return fail(Err::LockedFundsNotMarkedAsLocked);
        if (in.stake_lock != 0 && in.stake_lock != locktime)
            return fail(Err::LocktimeMismatch,
                        std::to_string(in.stake_lock) + " != " + std::to_string(locktime));

        if (auto s = f.verify_transfer(tx_bytes, in.in, creds[i], utxo.out, now); !s)
            return std::unexpected(s.error());

        const std::uint64_t amount = in.in.amount();
        if (now >= locktime) {
            if (auto s = accumulate(unlocked_consumed[utxo.asset], amount); !s) return s;
            continue;
        }
        if (auto s = accumulate(locked_consumed[utxo.asset][locktime][owner_id(utxo.out.owners)], amount); !s)
            return s;
    }

    for (const auto& out : outs) {
        const std::uint64_t locktime = out.stake_lock;
        const std::uint64_t amount = out.out.amount();
        if (locktime == 0) {
            if (auto s = accumulate(unlocked_produced[out.asset], amount); !s) return s;
            continue;
        }
        if (auto s = accumulate(locked_produced[out.asset][locktime][owner_id(out.out.owners)], amount); !s)
            return s;
    }

    // For every (asset, locktime, owner): locked produced ≤ locked consumed.
    // A shortfall may be covered out of the same asset's UNLOCKED consumption —
    // locking more of your own money is allowed — and that spend is then no
    // longer available to the unlocked balance below.
    for (const auto& [asset, by_locktime] : locked_produced) {
        auto consumed_asset = locked_consumed.find(asset);
        for (const auto& [locktime, by_owner] : by_locktime) {
            for (const auto& [owner, produced] : by_owner) {
                std::uint64_t consumed = 0;
                if (consumed_asset != locked_consumed.end()) {
                    const auto lt = consumed_asset->second.find(locktime);
                    if (lt != consumed_asset->second.end()) {
                        const auto ow = lt->second.find(owner);
                        if (ow != lt->second.end()) consumed = ow->second;
                    }
                }
                if (produced <= consumed) continue;
                const std::uint64_t increase = produced - consumed;
                std::uint64_t& unlocked = unlocked_consumed[asset];
                if (increase > unlocked)
                    return fail(Err::InsufficientLockedFunds,
                                hex(owner) + " needs " + std::to_string(increase - unlocked) + " more " +
                                    hex(asset) + " for locktime " + std::to_string(locktime));
                unlocked -= increase;
            }
        }
    }

    for (const auto& [asset, produced] : unlocked_produced) {
        const auto it = unlocked_consumed.find(asset);
        const std::uint64_t consumed = it == unlocked_consumed.end() ? 0 : it->second;
        if (produced > consumed)
            return fail(Err::InsufficientUnlockedFunds,
                        "needs " + std::to_string(produced - consumed) + " more " + hex(asset));
    }
    return ok();
}

}  // namespace lux::platformvm::flow
