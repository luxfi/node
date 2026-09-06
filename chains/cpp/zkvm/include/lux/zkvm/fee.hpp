// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fee.hpp — what this chain charges to admit a user transaction, and the check
// that refuses one paying less.
//
// It is a DECLARATION at the boundary, orthogonal to settlement: this says what
// the chain costs to submit to, while the per-operation debit and burn happen
// inside consensus against the payer's balance. A chain declares one of these at
// initialize, and the boot-time gate reads it.
//
// THE ZERO Fee ADMITS NOTHING, so a chain that forgets to declare one refuses
// every caller rather than admitting every caller.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/wire.hpp"

#include <cstdint>

namespace lux::zkvm {

// The floor a user-facing chain must charge at least. Go: fee.MinTxFeeFloor.
inline constexpr std::uint64_t kMinTxFeeFloor = 1'000'000;

inline constexpr const char* kErrChainAcceptsNoUserTxs = "chain accepts no user-submitted txs";
inline constexpr const char* kErrFeeTooLow = "tx fee below policy minimum";
inline constexpr const char* kErrZeroFeeUserChain =
    "fee policy declares zero min tx fee on a user-facing chain";

class Fee {
public:
    // closed is the declaration for a chain that accepts no user transactions at
    // all. Every caller is refused, so an entry that exposes itself as
    // user-callable still refuses explicitly instead of by omission.
    static Fee closed() { return Fee(false, 0); }

    // floor is the canonical declaration for a chain that accepts
    // user-submitted work: the minimum transaction fee. The Z-Chain accepts
    // user-submitted shielded transactions, so it declares this one.
    static Fee floor() { return Fee(true, kMinTxFeeFloor); }

    // The fee asset is a constant of the declaration — the Z-Chain's primary
    // UTXO asset, always — so there is no asset to compare here. A field that
    // can only hold one value is not a check.
    wire::Result<void> admit(std::uint64_t paid) const {
        if (!accepts_user_txs_) return std::unexpected(kErrChainAcceptsNoUserTxs);
        if (paid < minimum_) return std::unexpected(kErrFeeTooLow);
        return {};
    }

    bool accepts_user_txs() const { return accepts_user_txs_; }
    std::uint64_t minimum() const { return minimum_; }

    // validate is the boot-time gate: a user-facing chain declaring a zero floor
    // is refused at boot rather than discovered when the pool fills with free
    // transactions.
    wire::Result<void> validate() const {
        if (accepts_user_txs_ && minimum_ == 0) return std::unexpected(kErrZeroFeeUserChain);
        return {};
    }

private:
    Fee(bool accepts, std::uint64_t minimum) : accepts_user_txs_(accepts), minimum_(minimum) {}

    bool accepts_user_txs_ = false;
    std::uint64_t minimum_ = 0;
};

}  // namespace lux::zkvm
