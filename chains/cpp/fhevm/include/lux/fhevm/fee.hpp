// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fee.hpp — settlement: metered gas, a balance ledger, and the burn.
//
// Two halves that compose and do not overlap. ADMISSION is the boot-time
// declaration "this chain charges at least the floor", which the chain manager
// validates once. SETTLEMENT is the per-operation debit and supply reduction,
// performed inside block acceptance through the ledger, which writes to the
// VM's store — so a fee burn and the operation it pays for land together or
// neither does.
//
// There is deliberately no account-to-account transfer: service-chain fees are
// BURNED, not paid to a validator. A treasury split, if ever wanted, is a new
// method and not a reinterpretation of this one.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/id.hpp"
#include "lux/fhevm/store.hpp"

#include <cstdint>

namespace lux::fhevm::fee {

// Gas is a unit of metered work; a chain's schedule assigns a cost to each
// operation and cost() converts gas to nLUX at a per-unit price.
using Gas = std::uint64_t;

// MinTxFeeFloor is the minimum fee, in nLUX (1e-6 LUX), that any user-facing
// chain should charge.
inline constexpr std::uint64_t kMinTxFeeFloor = 1'000'000;

// cost converts metered gas to a fee, refusing overflow: a fee must never wrap
// to a smaller number.
Result<std::uint64_t> cost(Gas gas_used, Gas price);

// GasMeter meters consumption against a hard limit, exactly like the EVM gas
// pool. Consume past the limit DENIES the operation rather than overdrawing.
class GasMeter {
public:
    explicit GasMeter(Gas limit) : limit_(limit), remaining_(limit) {}

    Result<void> consume(Gas amount);
    Gas remaining() const { return remaining_; }
    Gas used() const { return limit_ - remaining_; }
    Gas limit() const { return limit_; }

private:
    Gas limit_;
    Gas remaining_;
};

// Ledger is the store-backed balance surface. Balances are nLUX.
class Ledger {
public:
    explicit Ledger(Store* kv) : kv_(kv) {}

    Result<std::uint64_t> balance(const Account& acct) const;
    // credit adds nLUX. Overflow is refused: minting must never silently wrap
    // to a smaller balance.
    Result<void> credit(const Account& acct, std::uint64_t amount);
    // burn debits the payer AND reduces circulating supply by the same amount.
    // It is the only spend path.
    Result<void> burn(const Account& acct, std::uint64_t amount);
    Result<std::uint64_t> burned() const;

private:
    Result<std::uint64_t> read_u64(ByteView key) const;
    Result<void> write_u64(ByteView key, std::uint64_t v);

    Store* kv_;
};

// can_pay is the read-only affordability check, run before a block can be
// accepted. It never mutates state, so verifying a block cannot move funds.
Result<void> can_pay(const Ledger& l, const Account& acct, std::uint64_t amount);

// charge is the authoritative settlement: debit and burn. Because it writes
// through the VM's store, the debit commits atomically with the operation.
Result<void> charge(Ledger& l, const Account& acct, std::uint64_t amount);

// FlatPolicy is the chain's ADMISSION declaration.
struct FlatPolicy {
    std::uint64_t fee = 0;
    Id asset_id{};

    std::uint64_t min_tx_fee() const { return fee; }
};

// validate refuses a user-facing chain that declares a zero minimum.
Result<void> validate(const FlatPolicy& p);

// utxo_asset_id_for is Go constants.UTXOAssetIDFor: mainnet keeps its historic
// asset id, every other network derives one, so the same address on two
// networks owns UTXOs with distinct asset ids.
Id utxo_asset_id_for(std::uint32_t network_id);

}  // namespace lux::fhevm::fee
