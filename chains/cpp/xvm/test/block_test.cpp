// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block_test.cpp — the block, ported from block/block_test.go.
//
// A block has no codec and no version byte: its bytes are authoritative and its
// id is sha256 of them. So the test that matters is not "do the fields survive"
// but "do the BYTES survive" — parse a block and the buffer that comes back
// must be the buffer that went in, or two nodes holding the same block would
// disagree about its name.

#include "check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/block.hpp"
#include "lux/xvm/txs.hpp"

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

constexpr std::uint32_t kUnitTestID = 369;

Id chain_id() { return id(0x21); }
Id asset_id() { return id(0x41); }

// create_test_txs is Go's createTestTxs, signed with the same key.
std::vector<std::shared_ptr<txs::Tx>> create_test_txs(int count) {
    std::vector<std::shared_ptr<txs::Tx>> out;
    for (int i = 0; i < count; ++i) {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kUnitTestID;
        utx->base.blockchain_id = chain_id();

        auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
        o->amt = 12345;
        o->out_owners = fx::OutputOwners{0, 1, {test_address(0)}};
        utx->base.outs.push_back(txs::TransferableOutput{asset_id(), o});

        auto in = std::make_shared<fx::secp256k1fx::TransferInput>();
        in->amt = 54321;
        in->input.sig_indices = {2};
        Id spent{};
        spent[0] = 't';
        spent[1] = 'x';
        spent[2] = 'I';
        spent[3] = 'D';
        spent[4] = std::uint8_t(i);  // one distinct spend per tx
        utx->base.ins.push_back(
            txs::TransferableInput{txs::UTXOID{spent, 1, false}, asset_id(), in});
        utx->base.memo = Bytes{1, 2, 3, 4, 5, 6, 7, 8};

        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
        (void)tx->initialize();
        out.push_back(tx);
    }
    return out;
}

void invalid_block() {
    std::printf("\n  -- an unparseable block --\n");
    check(!block::parse(ByteView{}), "empty bytes are not a block");
    Bytes junk{0x00, 0x01, 0x02};
    check(!block::parse(view(junk)), "three bytes are not a block");
    Bytes not_zap(64, 0xAB);
    check(!block::parse(view(not_zap)), "64 bytes with no ZAP header are not a block");
}

void standard_block_round_trip() {
    std::printf("\n  -- a standard block --\n");

    const Id parent = id(0x71);
    const std::uint64_t height = 2022;
    const std::uint64_t timestamp = 1749000000;
    auto tx_list = create_test_txs(1);

    auto built = block::build(parent, height, timestamp, kEmptyId, tx_list);
    if (!built) {
        check(false, "build: " + built.error());
        return;
    }
    const auto& blk = *built;

    auto parsed = block::parse(view(blk->bytes));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check((*parsed)->block_id == blk->block_id, "the id survives");
    check((*parsed)->parent_id == blk->parent_id, "the parent survives");
    check((*parsed)->height == blk->height, "the height survives");
    check((*parsed)->time == blk->time, "the timestamp survives");
    check((*parsed)->root == blk->root, "the root survives");
    check_eq(hex_of((*parsed)->bytes), hex_of(blk->bytes), "the BYTES survive");
    check_eq(hex_of((*parsed)->block_id), hex_of(sha256(view(blk->bytes))),
             "the id is sha256 of those bytes");

    check((*parsed)->transactions.size() == tx_list.size(), "every tx comes back");
    bool txs_match = true;
    for (std::size_t i = 0; i < tx_list.size(); ++i) {
        txs_match = txs_match && (*parsed)->transactions[i]->id() == tx_list[i]->id() &&
                    (*parsed)->transactions[i]->bytes() == tx_list[i]->bytes();
    }
    check(txs_match, "…each with its own id and its own bytes");
}

void many_txs() {
    std::printf("\n  -- a block of several transactions --\n");

    auto tx_list = create_test_txs(5);
    auto built = block::build(id(0x72), 9, 1749000001, id(0x73), tx_list);
    if (!built) {
        check(false, "build: " + built.error());
        return;
    }
    auto parsed = block::parse(view((*built)->bytes));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check((*parsed)->transactions.size() == 5, "five transactions round-trip");
    bool ordered = true;
    for (std::size_t i = 0; i < tx_list.size(); ++i) {
        ordered = ordered && (*parsed)->transactions[i]->id() == tx_list[i]->id();
    }
    check(ordered, "…in the order the block put them");
    check((*parsed)->root == id(0x73), "a non-empty root round-trips");
}

void empty_and_identity() {
    std::printf("\n  -- what a block's identity depends on --\n");

    // A block with no transactions is representable — the genesis block is one.
    // Refusing an empty block is a rule of VERIFICATION, not of encoding.
    auto empty = block::build(id(0x74), 0, 1749000002, kEmptyId, {});
    if (!empty) {
        check(false, "an empty block builds: " + empty.error());
        return;
    }
    auto parsed_empty = block::parse(view((*empty)->bytes));
    check(parsed_empty && (*parsed_empty)->transactions.empty(),
          "a block with no transactions round-trips");

    // Every field in the fixed section is part of the hashed bytes, so changing
    // any one of them changes the block's name.
    auto base = block::build(id(0x74), 5, 1749000003, kEmptyId, create_test_txs(1));
    auto other_parent = block::build(id(0x75), 5, 1749000003, kEmptyId, create_test_txs(1));
    auto other_height = block::build(id(0x74), 6, 1749000003, kEmptyId, create_test_txs(1));
    auto other_time = block::build(id(0x74), 5, 1749000004, kEmptyId, create_test_txs(1));
    auto other_root = block::build(id(0x74), 5, 1749000003, id(0x76), create_test_txs(1));
    auto other_txs = block::build(id(0x74), 5, 1749000003, kEmptyId, create_test_txs(2));
    if (!base || !other_parent || !other_height || !other_time || !other_root || !other_txs) {
        check(false, "the comparison blocks build");
        return;
    }
    check((*base)->block_id != (*other_parent)->block_id, "the parent is part of the id");
    check((*base)->block_id != (*other_height)->block_id, "the height is part of the id");
    check((*base)->block_id != (*other_time)->block_id, "the timestamp is part of the id");
    check((*base)->block_id != (*other_root)->block_id, "THE ROOT is part of the id");
    check((*base)->block_id != (*other_txs)->block_id, "the transactions are part of the id");

    // Building the same block twice gives the same bytes: the encoding has no
    // hidden state, so a block is a function of its fields.
    auto again = block::build(id(0x74), 5, 1749000003, kEmptyId, create_test_txs(1));
    check(again && (*again)->block_id == (*base)->block_id,
          "building the same block twice gives the same id");
    check(again && (*again)->bytes == (*base)->bytes, "…and the same bytes");
}

void uninitialized_tx_is_refused() {
    std::printf("\n  -- a block cannot carry an unfinished transaction --\n");

    // A tx that was never initialized has no wire bytes. Serializing it would
    // write an empty slot the parser could not read back, so the builder refuses
    // rather than writing a block nobody can parse.
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = std::make_shared<txs::BaseTx>();
    auto built = block::build(id(0x77), 1, 1749000005, kEmptyId, {tx});
    check(!built, "a tx with no wire bytes is refused");

    auto with_null = block::build(id(0x77), 1, 1749000005, kEmptyId,
                                  {std::shared_ptr<txs::Tx>{nullptr}});
    check(!with_null, "a null tx is refused");
}

}  // namespace

int main() {
    std::printf("xvm — the block, ported from block/block_test.go\n");
    invalid_block();
    standard_block_round_trip();
    many_txs();
    empty_and_identity();
    uninitialized_tx_is_refused();
    return report("block");
}
