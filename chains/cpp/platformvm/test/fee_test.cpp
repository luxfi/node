// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fee_test.cpp — what a transaction costs, and what that costs in µLUX.
//
// Ported from Go vms/platformvm/txs/fee/complexity_test.go and
// vms/components/gas (dimensions_test.go, gas_test.go, state_test.go). Every
// expected number below is the Go original's, unchanged: the fee schedule is a
// protocol constant, and two implementations that price a transaction
// differently do not agree about which blocks are valid.

#include "harness.hpp"
#include "lux/platformvm/complexity.hpp"
#include "signing.hpp"

using namespace lux::platformvm;
using namespace lux::platformvm::fee;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

gas::Dimensions dims(std::uint64_t bandwidth, std::uint64_t db_read, std::uint64_t db_write,
                     std::uint64_t compute) {
    gas::Dimensions d;
    d[gas::Bandwidth] = bandwidth;
    d[gas::DBRead] = db_read;
    d[gas::DBWrite] = db_write;
    d[gas::Compute] = compute;
    return d;
}

std::vector<ShortId> addrs(std::size_t n) { return std::vector<ShortId>(n); }
std::vector<std::uint32_t> indices(std::size_t n) { return std::vector<std::uint32_t>(n); }

TransferableOutput out_with(std::size_t owners, std::uint64_t stake_lock) {
    return TransferableOutput{Id{}, stake_lock, TransferOutput{0, OutputOwners{0, 0, addrs(owners)}}};
}

TransferableInput in_with(std::size_t sigs, std::uint64_t stake_lock) {
    TransferableInput in;
    in.stake_lock = stake_lock;
    in.in = TransferInput{0, indices(sigs)};
    return in;
}

}  // namespace

// Go: TestOutputComplexity.
TEST(OutputComplexity) {
    auto check = [](const TransferableOutput& o, gas::Dimensions want) {
        auto got = output_complexity({o});
        REQUIRE_OK(got);
        REQUIRE(got.value() == want);
    };
    check(out_with(0, 0), dims(60, 0, 1, 0));
    check(out_with(1, 0), dims(80, 0, 1, 0));
    check(out_with(3, 0), dims(120, 0, 1, 0));
    // A stake-locked output costs the wrapper it carries on the wire.
    check(out_with(3, 999), dims(132, 0, 1, 0));
}

// Go: TestInputComplexity. Note the compute: every signature the input names is
// a recovery every node in the network has to perform.
TEST(InputComplexity) {
    auto check = [](const TransferableInput& i, gas::Dimensions want) {
        auto got = input_complexity({i});
        REQUIRE_OK(got);
        REQUIRE(got.value() == want);
    };
    check(in_with(0, 0), dims(92, 1, 1, 0));
    check(in_with(1, 0), dims(161, 1, 1, 200));
    check(in_with(3, 0), dims(299, 1, 1, 600));
    check(in_with(3, 999), dims(311, 1, 1, 600));
}

// Go: TestOwnerComplexity.
TEST(OwnerComplexity) {
    auto check = [](std::size_t n, gas::Dimensions want) {
        auto got = owner_complexity(txs::Owner{0, 0, addrs(n)});
        REQUIRE_OK(got);
        REQUIRE(got.value() == want);
    };
    check(0, dims(16, 0, 0, 0));
    check(1, dims(36, 0, 0, 0));
    check(3, dims(76, 0, 0, 0));
}

// Go: TestAuthComplexity.
TEST(AuthComplexity) {
    auto check = [](std::size_t n, gas::Dimensions want) {
        auto got = auth_complexity(indices(n));
        REQUIRE_OK(got);
        REQUIRE(got.value() == want);
    };
    check(0, dims(8, 0, 0, 0));
    check(1, dims(77, 0, 0, 200));
    check(3, dims(215, 0, 0, 600));
}

// Go: TestSignerComplexity. A registered BLS key costs the pairing that proves
// its holder actually holds it.
TEST(SignerComplexity) {
    auto empty = signer_complexity(signer::Signer{signer::Empty{}});
    REQUIRE_OK(empty);
    REQUIRE(empty.value() == gas::Dimensions{});

    auto pop = signer_complexity(signer::Signer{signer::ProofOfPossession{}});
    REQUIRE_OK(pop);
    REQUIRE(pop.value() == dims(144, 0, 0, 1050));
}

// Go: TestTxComplexity — every supported transaction prices, the batch is the
// sum of the parts, and the chain's own transaction is refused.
TEST(TxComplexity) {
    BaseTx base;
    base.network_id = 10;
    base.blockchain_id = id_of(0x20);
    base.outs = {TransferableOutput{id_of(0x10), 0, TransferOutput{1000, OutputOwners{0, 1, addrs(1)}}}};
    TransferableInput in;
    in.utxo = UtxoId{id_of(0x30), 0};
    in.asset = id_of(0x10);
    in.in = TransferInput{1000, {0}};
    base.ins = {in};

    auto base_tx = txs::BaseTxUnsigned::create(base);
    REQUIRE_OK(base_tx);
    auto chain_tx = txs::CreateChainTx::create(base, id_of(0x40), "chain", id_of(0x50), {},
                                                std::vector<std::uint8_t>{'g', 'e', 'n'}, txs::Auth{0});
    REQUIRE_OK(chain_tx);

    auto a = tx_complexity(*base_tx.value());
    REQUIRE_OK(a);
    REQUIRE(a.value()[gas::Bandwidth] > 0);
    auto b = tx_complexity(*chain_tx.value());
    REQUIRE_OK(b);
    REQUIRE(b.value()[gas::Bandwidth] > 0);

    auto want = a.value().add(b.value());
    REQUIRE_OK(want);
    auto batch = tx_complexity(std::vector<const txs::UnsignedTx*>{base_tx.value().get(),
                                                                   chain_tx.value().get()});
    REQUIRE_OK(batch);
    REQUIRE(batch.value() == want.value());

    // The chain emits the reward proposal about itself, so it carries no price.
    auto reward = txs::RewardValidatorTx::create(id_of(7));
    REQUIRE_ERR(tx_complexity(*reward), Err::UnsupportedTx);
}

// The base transaction's price, computed the long way, so the intrinsic table is
// not merely asserted against itself.
TEST(BaseTxComplexityAddsUp) {
    BaseTx base;
    base.network_id = 10;
    base.blockchain_id = id_of(0x20);
    base.outs = {TransferableOutput{id_of(0x10), 0, TransferOutput{1000, OutputOwners{0, 1, addrs(1)}}}};
    TransferableInput in;
    in.utxo = UtxoId{id_of(0x30), 0};
    in.asset = id_of(0x10);
    in.in = TransferInput{1000, {0}};
    base.ins = {in};
    const std::string memo = "hello";
    base.memo.assign(memo.begin(), memo.end());

    auto tx = txs::BaseTxUnsigned::create(base);
    REQUIRE_OK(tx);
    auto got = tx_complexity(*tx.value());
    REQUIRE_OK(got);

    // intrinsic base bandwidth is version(2) + typeID(4) + networkID(4) +
    // blockchainID(32) + four counts(4 each) = 58; plus one one-owner
    // output(80, DBWrite 1), one one-signature input(161, DBRead 1, DBWrite 1,
    // Compute 200), and the memo's own 5 bytes.
    REQUIRE_U64(58u, intrinsic_base_tx()[gas::Bandwidth]);
    REQUIRE_U64(58u + 80u + 161u + 5u, got.value()[gas::Bandwidth]);
    REQUIRE_U64(1u, got.value()[gas::DBRead]);
    REQUIRE_U64(2u, got.value()[gas::DBWrite]);
    REQUIRE_U64(200u, got.value()[gas::Compute]);
}

// Go: gas.Dimensions Add / Sub / ToGas.
TEST(GasDimensions) {
    const auto a = dims(1, 2, 3, 4);
    const auto b = dims(10, 20, 30, 40);
    auto s = a.add(b);
    REQUIRE_OK(s);
    REQUIRE(s.value() == dims(11, 22, 33, 44));

    auto d = b.sub(a);
    REQUIRE_OK(d);
    REQUIRE(d.value() == dims(9, 18, 27, 36));
    REQUIRE_ERR(a.sub(b), Err::Underflow);

    auto g = a.to_gas(dims(1, 10, 100, 1000));
    REQUIRE_OK(g);
    REQUIRE_U64(1u + 20u + 300u + 4000u, g.value());

    // Nothing wraps: a fee that overflows to a small number is a free block.
    const auto huge = dims(UINT64_MAX, 0, 0, 0);
    REQUIRE_ERR(huge.add(dims(1, 0, 0, 0)), Err::Overflow);
    REQUIRE_ERR(huge.to_gas(dims(2, 0, 0, 0)), Err::Overflow);
}

// Go: gas.State AdvanceTime / ConsumeGas.
TEST(GasState) {
    gas::State s{100, 50};
    // Time refills capacity toward the cap and drains the excess toward zero.
    const auto later = s.advance_time(/*max_capacity=*/1000, /*max_per_second=*/10,
                                      /*target_per_second=*/5, /*duration=*/10);
    REQUIRE_U64(200u, later.capacity);
    REQUIRE_U64(0u, later.excess);

    const auto capped = s.advance_time(150, 10, 5, 10);
    REQUIRE_U64(150u, capped.capacity);

    auto consumed = s.consume(60);
    REQUIRE_OK(consumed);
    REQUIRE_U64(40u, consumed.value().capacity);
    REQUIRE_U64(110u, consumed.value().excess);

    // A block that asks for more than the chain has left is refused.
    REQUIRE_ERR(s.consume(101), Err::InsufficientCapacity);

    // The excess is a signal, not a balance: it saturates rather than wrapping.
    gas::State full{UINT64_MAX, UINT64_MAX};
    auto sat = full.consume(1);
    REQUIRE_OK(sat);
    REQUIRE_U64(UINT64_MAX, sat.value().excess);
}

// Go: gas.CalculatePrice — the EIP-4844 fake exponential. At zero excess the
// price is the floor; it rises with the excess and saturates rather than wraps.
TEST(CalculatePrice) {
    REQUIRE_U64(1u, gas::calculate_price(1, 0, 1));
    REQUIRE_U64(100u, gas::calculate_price(100, 0, 10'000));

    // e^1 ≈ 2.718…, so one conversion constant of excess roughly triples it.
    const auto e1 = gas::calculate_price(100, 10'000, 10'000);
    REQUIRE(e1 >= 271 && e1 <= 272);

    // Monotone in the excess.
    REQUIRE(gas::calculate_price(100, 20'000, 10'000) > e1);

    // Far out, it saturates at the largest price there is rather than wrapping
    // to a cheap one.
    REQUIRE_U64(UINT64_MAX, gas::calculate_price(1'000'000, UINT64_MAX, 1));
}
