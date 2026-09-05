// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// golden_test.cpp — the port's contract with the Go Z-Chain.
//
// Every assertion here compares a value this implementation produced against a
// value the GO implementation produced, verbatim, from test/golden.hpp. Byte
// fidelity is checked in BOTH directions: the bytes this port emits must be the
// bytes Go emits, and the bytes Go emits must parse here into the same values.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/utxo.hpp"
#include "lux/zkvm/vm.hpp"

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

void tx_bytes_and_ids() {
    std::printf("transaction bytes and ids, against the Go Z-Chain\n");

    const Transaction shield = shield_tx();
    check_eq(hex_of(shield.marshal()), golden::kShieldTx, "shieldTx marshals to Go's bytes");
    check_eq(hex_of(shield.compute_id()), golden::kShieldTxID, "shieldTx's id is Go's id");

    const Transaction w = wire_tx();
    check_eq(hex_of(w.marshal()), golden::kWireTx, "the wire fixture marshals to Go's bytes");
    check_eq(hex_of(w.compute_id()), golden::kWireTxID, "the wire fixture's id is Go's id");

    check_eq(hex_of(block_tx(1).marshal()), golden::kBlockTxFee1, "blockTx(1) is Go's bytes");
    check_eq(hex_of(block_tx(2).marshal()), golden::kBlockTxFee2, "blockTx(2) is Go's bytes");
    check_eq(hex_of(block_tx(1).compute_id()), golden::kBlockTxFee1ID, "blockTx(1)'s id");

    // The other direction: Go's bytes parse here into the same transaction, with
    // the identity DERIVED rather than read.
    auto parsed = parse_transaction(view(from_hex(golden::kWireTx)));
    check_ok(parsed, "Go's transaction bytes parse here");
    if (parsed) {
        check_eq(hex_of(parsed->compute_id()), golden::kWireTxID,
                 "a parsed transaction derives Go's id");
        check_eq(hex_of(parsed->marshal()), golden::kWireTx,
                 "and re-encodes to the bytes it came from");
        check(parsed->proof.has_value() && parsed->proof->proof_type == "groth16",
              "the proof survives the round trip");
    }
}

void block_bytes() {
    std::printf("\nblock bytes, against the Go Z-Chain\n");

    Block blk;
    blk.parent_id = id_of(0xEE);
    blk.block_height = 7;
    blk.block_timestamp = 1'600'000'000;
    blk.txs = {block_tx(1), block_tx(2)};
    blk.state_root = b("state-root");
    check_eq(hex_of(blk.marshal()), golden::kBlockTwoTx, "a two-transaction block is Go's bytes");

    Block back;
    check_ok(parse_block_bytes(view(from_hex(golden::kBlockTwoTx)), back),
             "Go's block bytes parse here");
    check(back.block_height == 7 && back.block_timestamp == 1'600'000'000,
          "the parsed block sits where Go's did");
    check(back.txs.size() == 2, "and carries both transactions");
    if (back.txs.size() == 2)
        check_eq(hex_of(back.txs[0].compute_id()), golden::kBlockTxFee1ID,
                 "a nested transaction's id is derived from its content too");
}

void utxo_bytes() {
    std::printf("\nUTXO bytes, against the Go Z-Chain\n");
    Utxo u;
    u.tx_id = Id{1, 2, 3};
    u.output_index = 7;
    u.height = 42;
    u.commitment = b("commit");
    u.ciphertext = b("cipher");
    u.ephemeral_pk = b("epk");
    check_eq(hex_of(u.marshal()), golden::kUtxo, "a UTXO record is Go's bytes");

    auto back = parse_utxo(view(from_hex(golden::kUtxo)));
    check_ok(back, "Go's UTXO bytes parse here");
    if (back) check(*back == u, "and round-trip to the same record");
}

void chain_binding_and_genesis() {
    std::printf("\nthe chain binding, and the genesis it produces\n");

    const Id bind = chain_binding(id_from_hex(golden::kChainId), golden::kNetworkId);
    check_eq(hex_of(bind), golden::kChainBind,
             "sha256(ChainID | NetworkID) is what the Go VM bound");

    store::Memory base;
    Vm vm(golden_config(), base);
    check_ok(vm.initialize(Genesis{}), "the chain comes up on a fresh store");
    check_eq(hex_of(vm.last_accepted()), golden::kGenesisBlockID,
             "the genesis block id is the one the Go VM reported");
    check_eq(hex_of(vm.state_root().get()), golden::kGenesisStateRoot,
             "and the genesis state root is Go's");
}

void the_block_the_go_chain_accepted() {
    std::printf("\nthe block the Go chain verified and accepted\n");

    store::Memory base;
    Vm vm(golden_config(), base);
    vm.set_now(golden::kBlockOneTimestamp);
    check_ok(vm.initialize(Genesis{}), "the chain comes up");

    // The transaction Go admitted: a groth16 proof that SATISFIES the pairing
    // equation for the key this chain is configured with.
    auto tx = parse_transaction(view(from_hex(golden::kSpendingTx)));
    check_ok(tx, "Go's spending transaction parses here");
    if (!tx) return;
    check_eq(hex_of(tx->compute_id()), golden::kSpendingTxID, "and derives Go's transaction id");

    check_ok(vm.admit(*tx, 1), "this node admits it — the proof verifies on the arithmetic");

    check_eq(hex_of(vm.compute_state_root({*tx})), golden::kBlockOneStateRoot,
             "the state root a block over it commits to is Go's");

    auto blk = vm.parse_block(view(from_hex(golden::kBlockOne)));
    check_ok(blk, "Go's block parses here");
    if (!blk) return;
    check_eq(hex_of((*blk)->id()), golden::kBlockOneID, "and derives Go's block id");
    check((*blk)->block_height == golden::kBlockOneHeight, "at Go's height");
    check_eq(hex_of((*blk)->bytes()), golden::kBlockOne, "keeping the bytes it arrived in");

    check_ok((*blk)->check(), "this node verifies it");
    check_ok((*blk)->commit(), "and accepts it");
    check_eq(hex_of(vm.last_accepted()), golden::kTipAfterOneBlock,
             "the tip is where the Go chain's tip ended up");
    check(vm.last_accepted_height() == 1, "at height 1");
    check(vm.nullifiers().count() == 1, "one note is spent");
    check(vm.utxos().count() == 1, "one shielded output exists");
    check_eq(hex_of(vm.state_root().get()), golden::kBlockOneStateRoot,
             "and the committed root advanced to the block's");
}

}  // namespace

int main() {
    tx_bytes_and_ids();
    block_bytes();
    utxo_bytes();
    chain_binding_and_genesis();
    the_block_the_go_chain_accepted();
    return report("golden");
}
