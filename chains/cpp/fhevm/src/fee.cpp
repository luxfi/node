// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/fee.hpp"

#include <cstring>

namespace lux::fhevm::fee {
namespace {

constexpr std::string_view kBalPrefix = "fee/bal/";
constexpr std::string_view kBurnedKey = "fee/burned";

bool add_overflow(std::uint64_t a, std::uint64_t b, std::uint64_t* sum) {
    *sum = a + b;
    return *sum < a;
}

Bytes bal_key(const Account& acct) { return key(kBalPrefix, view(acct)); }

}  // namespace

Result<std::uint64_t> cost(Gas gas_used, Gas price) {
    if (gas_used == 0 || price == 0) return std::uint64_t(0);
    std::uint64_t f = gas_used * price;
    if (f / price != gas_used) return fail(Err::BalanceOverflow);
    return f;
}

Result<void> GasMeter::consume(Gas amount) {
    if (amount > remaining_) return fail(Err::OutOfGas);
    remaining_ -= amount;
    return {};
}

Result<std::uint64_t> Ledger::read_u64(ByteView k) const {
    auto v = kv_->get(k);
    if (!v) return std::unexpected(v.error());
    if (!v->has_value()) return std::uint64_t(0);
    const Bytes& b = **v;
    if (b.size() != 8) return fail(Err::Database, "fee ledger: corrupt u64");
    std::uint64_t out = 0;
    for (std::uint8_t c : b) out = (out << 8) | c;
    return out;
}

Result<void> Ledger::write_u64(ByteView k, std::uint64_t v) {
    std::uint8_t b[8];
    for (int i = 0; i < 8; ++i) b[i] = std::uint8_t(v >> (56 - 8 * i));
    return kv_->put(k, ByteView(b, 8));
}

Result<std::uint64_t> Ledger::balance(const Account& acct) const {
    return read_u64(view(bal_key(acct)));
}

Result<void> Ledger::credit(const Account& acct, std::uint64_t amount) {
    if (amount == 0) return {};
    Bytes k = bal_key(acct);
    auto cur = read_u64(view(k));
    if (!cur) return std::unexpected(cur.error());
    std::uint64_t next = 0;
    if (add_overflow(*cur, amount, &next)) return fail(Err::BalanceOverflow);
    return write_u64(view(k), next);
}

Result<void> Ledger::burn(const Account& acct, std::uint64_t amount) {
    if (amount == 0) return {};
    Bytes k = bal_key(acct);
    auto cur = read_u64(view(k));
    if (!cur) return std::unexpected(cur.error());
    if (*cur < amount) return fail(Err::InsufficientFunds);
    auto b = read_u64(view(kBurnedKey));
    if (!b) return std::unexpected(b.error());
    std::uint64_t next_burned = 0;
    // Burned-supply accounting must never wrap: refuse rather than corrupt the
    // audit total.
    if (add_overflow(*b, amount, &next_burned)) return fail(Err::BalanceOverflow);
    auto w = write_u64(view(k), *cur - amount);
    if (!w) return w;
    return write_u64(view(kBurnedKey), next_burned);
}

Result<std::uint64_t> Ledger::burned() const { return read_u64(view(kBurnedKey)); }

Result<void> can_pay(const Ledger& l, const Account& acct, std::uint64_t amount) {
    auto bal = l.balance(acct);
    if (!bal) return std::unexpected(bal.error());
    if (*bal < amount) return fail(Err::InsufficientFunds);
    return {};
}

Result<void> charge(Ledger& l, const Account& acct, std::uint64_t amount) {
    return l.burn(acct, amount);
}

Result<void> validate(const FlatPolicy& p) {
    if (p.min_tx_fee() == 0) {
        return fail(Err::InvalidBlock, "fee policy declares zero min tx fee on a user-facing chain");
    }
    return {};
}

Id utxo_asset_id_for(std::uint32_t network_id) {
    if (network_id == 1) {
        Id mainnet{};
        constexpr std::string_view kWord = "lux asset id";
        std::memcpy(mainnet.data(), kWord.data(), kWord.size());
        return mainnet;
    }
    std::uint8_t preimage[16] = {};
    constexpr std::string_view kWord = "lux asset id";
    std::memcpy(preimage, kWord.data(), kWord.size());
    for (int i = 0; i < 4; ++i) preimage[12 + i] = std::uint8_t(network_id >> (24 - 8 * i));
    return sha256(ByteView(preimage, 16));
}

}  // namespace lux::fhevm::fee
