// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gas.hpp — what an operation costs, priced by what it actually dispatches.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/fee.hpp"

#include <string_view>
#include <vector>

namespace lux::fhevm {

struct Transaction;

// GasPrice is nLUX per unit of gas. It is chosen so the cheapest priced
// operation still settles at or above the admission floor (fee::kMinTxFeeFloor,
// 1 mLUX), unifying per-operation settlement with the pre-existing floor.
inline constexpr fee::Gas kGasPrice = 1'000;

// GasPerByte prices the bytes a transaction puts on the chain FOREVER: its
// payload and its scheme, the two fields whose length the payer chooses. It
// follows Ethereum's non-zero calldata rate for the same reason — storage is
// the cost a base fee cannot express — and it is what stops a ciphertext body
// riding onto F for the price of the handle that was supposed to replace it.
inline constexpr fee::Gas kGasPerByte = 16;

// uses_scheme reports whether an operation's price depends on the FHE scheme.
// Registering a ciphertext and requesting its decryption both scale with the
// scheme — the first in the size the network carries and indexes, the second in
// the committee's partial-decryption and combination work. Granting, revoking,
// attesting a finished result, and advancing an epoch are fixed-size record
// writes that touch no ciphertext.
bool uses_scheme(std::uint8_t tx_type);

// gas_for returns the metered gas for a transaction, pricing by operation and —
// for committee-dispatching operations — by scheme. It fails closed on an
// unknown operation type or an unknown/missing scheme for an operation that
// requires one.
Result<fee::Gas> gas_for(const Transaction& tx);

// fee_for returns the nLUX a transaction settles: gas_for(tx) * kGasPrice.
Result<std::uint64_t> fee_for(const Transaction& tx);

// supported_scheme reports whether a scheme is priced — and therefore accepted
// — by the F-Chain gas schedule. Membership of that schedule is the single
// source of truth for which schemes F takes.
bool supported_scheme(std::string_view scheme);

// schemes lists every priced scheme, in the schedule's canonical (sorted)
// order, so a listing is stable across runs.
std::vector<std::string_view> schemes();

// min_scheduled_fee is the smallest fee any valid operation can settle (the
// cheapest base operation at kGasPrice).
std::uint64_t min_scheduled_fee();

}  // namespace lux::fhevm
