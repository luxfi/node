// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// components_test.cpp — the spending model, ported from
// github.com/luxfi/utxo (transferables_test, secp256k1fx output/input tests)
// and the platformvm's own stakeable lock tests.
//
// The fx wire envelope cases assert against bytes produced by the Go
// reference: that envelope is the SORT KEY for outputs, so getting it wrong
// would make this implementation disagree with the network about which
// transactions are well-formed, silently and only for some address lists.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/components.hpp"

#include <string>

using namespace lux::platformvm;

namespace {

ShortId short_id(std::uint8_t b) {
    ShortId s{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) s[i] = static_cast<std::uint8_t>(b + i);
    return s;
}
Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
}  // namespace

// Go: secp256k1fx TestOutputOwnersVerify — the four refusals, in order.
TEST(OutputOwnersVerify) {
    OutputOwners ok_owners{0, 1, {short_id(0x10)}};
    REQUIRE_OK(ok_owners.verify());

    OutputOwners unspendable{0, 2, {short_id(0x10)}};
    REQUIRE_ERR(unspendable.verify(), Err::OutputUnspendable);

    OutputOwners unoptimized{0, 0, {short_id(0x10)}};
    REQUIRE_ERR(unoptimized.verify(), Err::OutputUnoptimized);

    OutputOwners unsorted{0, 1, {short_id(0x20), short_id(0x10)}};
    REQUIRE_ERR(unsorted.verify(), Err::AddrsNotSortedUnique);

    OutputOwners duplicated{0, 1, {short_id(0x10), short_id(0x10)}};
    REQUIRE_ERR(duplicated.verify(), Err::AddrsNotSortedUnique);

    // Threshold 0 with no addresses is the canonical "burned" owner and is fine.
    OutputOwners empty{0, 0, {}};
    REQUIRE_OK(empty.verify());
}

// Go: secp256k1fx TestTransferOutputVerify
TEST(TransferOutputVerify) {
    TransferOutput zero_value{0, {0, 1, {short_id(0x10)}}};
    REQUIRE_ERR(zero_value.verify(), Err::NoValueOutput);

    TransferOutput good{1, {0, 1, {short_id(0x10)}}};
    REQUIRE_OK(good.verify());
}

// Go: secp256k1fx TestTransferInputVerify / TestInputVerify
TEST(TransferInputVerify) {
    TransferInput zero_value{0, {0}};
    REQUIRE_ERR(zero_value.verify(), Err::NoValueInput);

    TransferInput unsorted{1, {1, 0}};
    REQUIRE_ERR(unsorted.verify(), Err::InputIndicesNotSortedUnique);

    TransferInput duplicated{1, {0, 0}};
    REQUIRE_ERR(duplicated.verify(), Err::InputIndicesNotSortedUnique);

    TransferInput good{1, {0, 1}};
    REQUIRE_OK(good.verify());

    const auto c = good.cost();
    REQUIRE(c.has_value());
    REQUIRE_U64(2000, *c);
}

// Go: utxo TransferableOutput.Verify — an empty asset id is not a valid asset.
TEST(TransferableOutputVerify) {
    TransferableOutput no_asset{Id{}, 0, TransferOutput{1, {0, 1, {short_id(0x10)}}}};
    REQUIRE_ERR(no_asset.verify(), Err::EmptyAssetID);

    TransferableOutput good{id_of(0x10), 0, TransferOutput{1, {0, 1, {short_id(0x10)}}}};
    REQUIRE_OK(good.verify());
}

// Go: utxo BaseTx.Verify — the three metadata refusals.
TEST(BaseTxVerify) {
    const Runtime rt{96369, id_of(0x20), id_of(0x10)};

    BaseTx wrong_network{1, id_of(0x20), {}, {}, {}};
    REQUIRE_ERR(wrong_network.verify(rt), Err::WrongNetworkID);

    BaseTx wrong_chain{96369, id_of(0x21), {}, {}, {}};
    REQUIRE_ERR(wrong_chain.verify(rt), Err::WrongChainID);

    BaseTx big_memo{96369, id_of(0x20), {}, {}, std::vector<std::uint8_t>(kMaxMemoSize + 1, 0)};
    REQUIRE_ERR(big_memo.verify(rt), Err::MemoTooLarge);

    BaseTx good{96369, id_of(0x20), {}, {}, std::vector<std::uint8_t>(kMaxMemoSize, 0)};
    REQUIRE_OK(good.verify(rt));
}

// The output sort key: the inner fx wire envelope, byte-for-byte the Go
// reference's secp256k1fx.TransferOutput.Bytes / stakeable.LockOut.Bytes.
TEST(OutputWireEnvelopeMatchesGo) {
    TransferableOutput plain{id_of(0x10), 0,
                             TransferOutput{1000000, {7, 2, {short_id(0x30), short_id(0x40), short_id(0x50)}}}};
    REQUIRE(hex(plain.wire_bytes()) == pvmgold::secp_transfer_output);

    TransferableOutput locked{id_of(0x10), 999, TransferOutput{42, {0, 1, {short_id(0x30)}}}};
    REQUIRE(hex(locked.wire_bytes()) == pvmgold::locked_output);

    TransferableOutput no_addrs{id_of(0x10), 0, TransferOutput{5, {0, 0, {}}}};
    REQUIRE(hex(no_addrs.wire_bytes()) == pvmgold::secp_transfer_output_no_addrs);
}

// Ordering is consensus: outputs by (asset, envelope bytes), non-strict.
TEST(OutputOrdering) {
    TransferableOutput a{id_of(0x10), 0, TransferOutput{1, {0, 1, {short_id(0x10)}}}};
    TransferableOutput b{id_of(0x20), 0, TransferOutput{1, {0, 1, {short_id(0x10)}}}};
    REQUIRE(outputs_sorted({a, b}));
    REQUIRE(!outputs_sorted({b, a}));
    // Equal outputs are a legitimate transaction, so the order is not strict.
    REQUIRE(outputs_sorted({a, a}));

    std::vector<TransferableOutput> v{b, a};
    sort_outputs(v);
    REQUIRE(v[0].asset == a.asset);
    REQUIRE(v[1].asset == b.asset);
}

// Inputs are STRICTLY ordered and unique: spending one UTXO twice in a
// transaction is a double spend, not a spelling.
TEST(InputOrdering) {
    TransferableInput a{{id_of(0x10), 0}, id_of(0x10), 0, {1, {0}}};
    TransferableInput b{{id_of(0x10), 1}, id_of(0x10), 0, {1, {0}}};
    TransferableInput c{{id_of(0x20), 0}, id_of(0x10), 0, {1, {0}}};
    REQUIRE(inputs_sorted_unique({a, b, c}));
    REQUIRE(!inputs_sorted_unique({b, a}));
    REQUIRE(!inputs_sorted_unique({a, a}));

    std::vector<TransferableInput> v{c, b, a};
    sort_inputs(v);
    REQUIRE(v[0].utxo == a.utxo);
    REQUIRE(v[1].utxo == b.utxo);
    REQUIRE(v[2].utxo == c.utxo);
}

// Post-Durango, the memo field must be empty.
TEST(MemoFieldLength) {
    REQUIRE_OK(verify_memo_field_length({}));
    const std::uint8_t one[1] = {0};
    REQUIRE_ERR(verify_memo_field_length({one, 1}), Err::MemoTooLarge);
}
