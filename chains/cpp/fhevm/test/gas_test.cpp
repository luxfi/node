// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gas_test.cpp — the cost model, and its relation to the admission floor.
//
// Ported case for case from the Go F-Chain's gas_test.go. The exact numbers are
// checked against Go's own in differential_test; what this file checks is that
// they are the RIGHT SHAPE — that the model says something true about the work
// each operation dispatches, and that settlement and admission cannot drift.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/gas.hpp"
#include "lux/fhevm/service.hpp"
#include "lux/fhevm/transaction.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

std::uint64_t fee_of(std::uint8_t type, std::string_view scheme) {
    Transaction tx;
    tx.type = type;
    tx.scheme = std::string(scheme);
    auto f = fee_for(tx);
    if (!f) {
        std::printf("  FAIL  the schedule could not price op %u scheme %s\n", unsigned(type),
                    std::string(scheme).c_str());
        ++g_fail;
        return 0;
    }
    return *f;
}

void per_scheme_costs_are_distinct() {
    // The cost model is real: a TFHE boolean, a BFV vector and a CKKS vector are
    // priced differently, and doubling the ring dimension raises the price.
    auto f = [](std::string_view s) { return fee_of(kTxRequestDecrypt, s); };
    check(f("tfhe-n10") < f("bfv-n13"), "small LWE ciphertexts are cheapest");
    check(f("bfv-n13") < f("ckks-n13"), "at equal N, CKKS costs more than BFV");
    check(f("bfv-n13") < f("bfv-n14"), "doubling the ring dimension must cost more");
    check(f("ckks-n14") < f("ckks-n15"), "and again at the next dimension");
    check(f("bfv-n13") != f("bgv-n13"), "distinct schemes must price distinctly");
}

void every_operation_meets_the_floor() {
    // Every scheduled operation settles at or above the node admission floor, so
    // settlement and admission never drift apart.
    bool all = true;
    for (std::uint8_t op : {kTxRegisterCiphertext, kTxGrantPermit, kTxRevokePermit,
                            kTxRequestDecrypt, kTxFulfillDecrypt, kTxAdvanceEpoch}) {
        if (uses_scheme(op)) {
            for (std::string_view s : schemes()) {
                if (fee_of(op, s) < fee::kMinTxFeeFloor) all = false;
            }
            continue;
        }
        if (fee_of(op, "") < fee::kMinTxFeeFloor) all = false;
    }
    check(all, "every (operation, scheme) settles at or above the admission floor");
    check(min_scheduled_fee() >= fee::kMinTxFeeFloor,
          "and the cheapest scheduled fee satisfies it too");
}

void an_unknown_scheme_is_refused() {
    // An unrecognised scheme is refused (fail closed), never priced at the bare
    // base cost.
    Transaction tx;
    tx.type = kTxRegisterCiphertext;
    tx.scheme = "paillier";
    refused(gas_for(tx), Err::UnknownScheme, "a scheme the schedule does not name");

    Transaction bare;
    bare.type = kTxRequestDecrypt;
    refused(gas_for(bare), Err::UnknownScheme,
            "and an operation that needs one, naming none");

    check(!supported_scheme("paillier"), "so it is not a supported scheme");
    check(supported_scheme(kTestScheme), "and the control is");
}

void an_unknown_operation_is_refused() {
    Transaction tx;
    tx.type = 200;
    tx.scheme = std::string(kTestScheme);
    refused(gas_for(tx), Err::InvalidTxType, "an unpriced transaction type cannot slip through");
    refused(fee_for(tx), Err::InvalidTxType, "and the fee surface fails the same way");

    Transaction unpriceable;
    unpriceable.type = kTxRegisterCiphertext;
    unpriceable.scheme = "rot13-n1";
    refused(fee_for(unpriceable), Err::UnknownScheme, "as does a scheme it cannot price");

    Transaction sound;
    sound.type = kTxRegisterCiphertext;
    sound.scheme = std::string(kTestScheme);
    auto f = fee_for(sound);
    check(f && *f > 0, "the control: a priced operation settles something");
}

void record_operations_ignore_which_scheme() {
    // The fixed-size record writes do not care WHICH scheme is named — only how
    // many bytes it takes to name it, which every operation pays for because
    // every operation stores it.
    for (std::uint8_t op : {kTxGrantPermit, kTxRevokePermit, kTxFulfillDecrypt, kTxAdvanceEpoch}) {
        std::uint64_t a = fee_of(op, "ckks-n15");
        std::uint64_t b = fee_of(op, "tfhe-n10");
        check_eq(a, b, "operation " + std::to_string(op) + " does not care which scheme");
        check(a > fee_of(op, ""), "but still pays for the bytes it stores");
    }
}

void every_operation_is_priced_and_named() {
    // The schedule covers every transaction type the package defines, and each
    // has exactly one public name — a new operation cannot ship unpriced or
    // unnamed.
    bool all = true;
    for (std::uint8_t op : {kTxRegisterCiphertext, kTxGrantPermit, kTxRevokePermit,
                            kTxRequestDecrypt, kTxFulfillDecrypt, kTxAdvanceEpoch}) {
        Transaction tx;
        tx.type = op;
        tx.scheme = uses_scheme(op) ? std::string(kTestScheme) : std::string();
        if (!gas_for(tx)) all = false;
        if (Service::operation_name(op).empty()) all = false;
    }
    check(all, "every operation has a base cost and a public name");
    check(Service::operation_name(99).empty(), "and an operation that is not one has neither");
}

void stored_bytes_are_priced() {
    // What a transaction stores it pays for. Two identical operations differing
    // only in payload length must not cost the same.
    Transaction base;
    base.type = kTxRevokePermit;
    std::string empty_reason = marshal(RevokePayload{});
    base.payload.assign(empty_reason.begin(), empty_reason.end());
    auto b = gas_for(base);

    Transaction longer = base;
    RevokePayload big;
    big.reason = std::string(1000, 'r');
    std::string doc = marshal(big);
    longer.payload.assign(doc.begin(), doc.end());
    auto l = gas_for(longer);

    check(b && l && *l > *b, "a longer payload must cost more");
    check(b && l && (*l - *b) >= 1000 * kGasPerByte, "every stored byte is priced");

    // The scheme string is stored too, so it is priced too.
    Transaction wide = base;
    wide.scheme = "0123456789abcdef";
    auto w = gas_for(wide);
    check(b && w && *w == *b + 16 * kGasPerByte, "sixteen bytes of scheme cost sixteen bytes");
}

void the_fee_is_gas_times_the_price() {
    Transaction tx;
    tx.type = kTxRegisterCiphertext;
    tx.scheme = std::string(kTestScheme);
    auto g = gas_for(tx);
    auto f = fee_for(tx);
    check(g && f && *f == *g * kGasPrice, "a fee is metered gas at the chain's price");

    // The conversion refuses overflow rather than wrapping to a smaller number.
    refused(fee::cost(~std::uint64_t(0), 2), Err::BalanceOverflow,
            "a fee that would wrap is refused");
    auto zero = fee::cost(0, kGasPrice);
    check(zero && *zero == 0, "and no gas costs nothing");
}

void the_meter_denies_rather_than_overdrawing() {
    fee::GasMeter m(100);
    check_eq(m.limit(), 100, "a meter starts at its limit");
    check_eq(m.remaining(), 100, "fully unconsumed");
    accepted(m.consume(60), "it consumes what fits");
    check_eq(m.used(), 60, "and reports what it used");
    refused(m.consume(41), Err::OutOfGas, "past the limit it denies the operation");
    check_eq(m.remaining(), 40, "and changes nothing when it does");
    accepted(m.consume(40), "the last unit still fits");
    check_eq(m.remaining(), 0, "leaving nothing");
}

}  // namespace

int main() {
    std::printf("fhevm — what an operation costs, and why\n\n");
    per_scheme_costs_are_distinct();
    every_operation_meets_the_floor();
    an_unknown_scheme_is_refused();
    an_unknown_operation_is_refused();
    record_operations_ignore_which_scheme();
    every_operation_is_priced_and_named();
    stored_bytes_are_priced();
    the_fee_is_gas_times_the_price();
    the_meter_denies_rather_than_overdrawing();
    return report("gas");
}
