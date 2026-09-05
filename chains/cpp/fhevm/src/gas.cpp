// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/gas.hpp"

#include "lux/fhevm/transaction.hpp"

#include <map>

namespace lux::fhevm {
namespace {

// op_base_gas prices the STRUCTURAL cost of an operation — signature
// authentication, state writes, indexing — independent of any FHE scheme.
const std::map<std::uint8_t, fee::Gas>& op_base_gas() {
    static const std::map<std::uint8_t, fee::Gas> m{
        {kTxRegisterCiphertext, 21'000},
        {kTxGrantPermit, 8'000},
        {kTxRevokePermit, 3'000},
        {kTxRequestDecrypt, 15'000},
        {kTxFulfillDecrypt, 10'000},
        {kTxAdvanceEpoch, 5'000},
    };
    return m;
}

// scheme_gas prices the cryptographic work an operation DISPATCHES to the
// off-chain threshold committee, BY SCHEME AND RING DIMENSION — the two
// parameters that actually set the cost. A ciphertext's size is linear in the
// ring dimension N and its transform cost is N log N, so doubling N roughly
// doubles the gas; and at equal N the schemes differ because they carry
// different numbers of ring elements and moduli (TFHE's small LWE ciphertexts
// are cheapest, CKKS's rescaling chain the dearest). Pricing every FHE
// operation at one flat rate would charge a TFHE boolean the same as a CKKS
// n=2^15 vector, which is off by an order of magnitude.
//
// Membership of this map is ALSO the single source of truth for "which schemes
// F accepts": an operation naming a scheme absent here is refused (fail
// closed), never priced at the bare base cost.
const std::map<std::string, fee::Gas>& scheme_gas() {
    static const std::map<std::string, fee::Gas> m{
        {"tfhe-n10", 12'000}, {"tfhe-n11", 24'000},  {"bfv-n13", 25'000},
        {"bfv-n14", 50'000},  {"bgv-n13", 26'000},   {"bgv-n14", 52'000},
        {"ckks-n13", 30'000}, {"ckks-n14", 60'000},  {"ckks-n15", 120'000},
    };
    return m;
}

}  // namespace

bool uses_scheme(std::uint8_t tx_type) {
    return tx_type == kTxRegisterCiphertext || tx_type == kTxRequestDecrypt;
}

Result<fee::Gas> gas_for(const Transaction& tx) {
    auto base = op_base_gas().find(tx.type);
    if (base == op_base_gas().end()) return fail(Err::InvalidTxType, "unknown tx type");
    fee::Gas total = base->second;
    if (uses_scheme(tx.type)) {
        auto sg = scheme_gas().find(tx.scheme);
        if (sg == scheme_gas().end()) return fail(Err::UnknownScheme, tx.scheme);
        total += sg->second;
    }
    return total + fee::Gas(tx.payload.size() + tx.scheme.size()) * kGasPerByte;
}

Result<std::uint64_t> fee_for(const Transaction& tx) {
    auto g = gas_for(tx);
    if (!g) return std::unexpected(g.error());
    return fee::cost(*g, kGasPrice);
}

bool supported_scheme(std::string_view scheme) {
    return scheme_gas().find(std::string(scheme)) != scheme_gas().end();
}

std::vector<std::string_view> schemes() {
    std::vector<std::string_view> out;
    out.reserve(scheme_gas().size());
    for (const auto& [name, _] : scheme_gas()) out.push_back(name);
    return out;
}

std::uint64_t min_scheduled_fee() {
    fee::Gas smallest = 0;
    bool first = true;
    for (const auto& [_, g] : op_base_gas()) {
        if (first || g < smallest) {
            smallest = g;
            first = false;
        }
    }
    auto f = fee::cost(smallest, kGasPrice);
    return f ? *f : 0;
}

}  // namespace lux::fhevm
