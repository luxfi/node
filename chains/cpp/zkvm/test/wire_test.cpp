// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire_test.cpp — the two rules every frame on this chain is held to.
//
//   CANONICAL   one value has one byte string
//   AGREEMENT   a length vector and the blob it indexes cover each other exactly
//
// Both are ported case for case from the Go Z-Chain's wire tests.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/utxo.hpp"
#include <zap/zap.hpp>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

Bytes with_tail(const Bytes& base, const Bytes& tail) {
    Bytes out = base;
    out.insert(out.end(), tail.begin(), tail.end());
    return out;
}

void utxo_round_trip() {
    std::printf("a UTXO round-trips, and a trailing byte is refused\n");
    Utxo u;
    u.tx_id = Id{1, 2, 3};
    u.output_index = 7;
    u.height = 42;
    u.commitment = b("commit");
    u.ciphertext = b("cipher");
    u.ephemeral_pk = b("epk");

    const Bytes raw = u.marshal();
    auto got = parse_utxo(view(raw));
    check_ok(got, "the record parses");
    if (got) check(*got == u, "into the record it came from");
    check_err(parse_utxo(view(with_tail(raw, {0}))), wire::kErrTrailingBytes,
              "a trailing byte is refused, not ignored");
}

void transaction_round_trip() {
    std::printf("\na transaction round-trips, and its id is derived not carried\n");
    Transaction tx = wire_tx();
    const Bytes raw = tx.marshal();

    auto got = parse_transaction(view(raw));
    check_ok(got, "the frame parses");
    if (!got) return;

    // The id is DERIVED on the way in, not carried. A peer that chooses it
    // chooses the proof-cache key, and the cache answers before anything binds
    // the proof to what the transaction spends.
    check(got->id == tx.compute_id(), "the parsed identity is the content's");
    tx.id = got->id;
    check(*got == tx, "and every field survives the trip");

    // Whatever id the sender puts on the value, the wire carries none, so the
    // bytes and the parsed identity are the same either way.
    tx.id = id_of(0xFF);
    check_eq(hex_of(tx.marshal()), hex_of(raw), "the id must not reach the wire");

    // Canonical: bytes appended to a valid frame are refused rather than
    // ignored. Ignoring them would give a peer unlimited fresh encodings of a
    // transaction the node already holds.
    for (const Bytes& tail : {Bytes{0}, Bytes{0xFF}, Bytes(32, 0)}) {
        check_err(parse_transaction(view(with_tail(raw, tail))), wire::kErrTrailingBytes,
                  "a padded transaction frame is refused");
    }
}

// tx_frame hand-builds a transaction frame so the nullifier length vector and
// the blob it indexes can be made to disagree — what a hostile peer writes and
// an honest encoder never does.
Bytes tx_frame(const std::vector<std::uint32_t>& null_lens, const Bytes& null_blob) {
    zap::Builder bld(zap::kHeaderSize + kTxSize + int(null_blob.size()) +
                     4 * int(null_lens.size()) + 512);
    const int empty = wire::write_u32_list(bld, {});
    const int null_off = wire::write_u32_list(bld, null_lens);

    auto ob = bld.start_object(kTxSize);
    ob.set_u8(0, std::uint8_t(TxType::Transfer));
    ob.set_u8(1, 1);
    ob.set_u64(2, 1);
    ob.set_u64(10, 0);
    ob.set_list(18, empty, 0);
    ob.set_bytes(26, {});
    ob.set_list(34, empty, 0);
    ob.set_bytes(42, {});
    ob.set_list(50, null_off, int(null_lens.size()));
    ob.set_bytes(58, view(null_blob));
    ob.set_list(66, empty, 0);
    ob.set_bytes(74, {});
    ob.set_bytes(82, {});
    ob.set_bytes(90, {});
    ob.finish_as_root();
    return bld.finish();
}

void lengths_must_cover_their_blob() {
    std::printf("\nthe nullifier lengths and their blob must cover each other exactly\n");

    // Positive control, so the refusals below are caused by the disagreement and
    // nothing else.
    auto ok = parse_transaction(view(tx_frame({4, 4}, b("aaaabbbb"))));
    check_ok(ok, "lengths that exactly cover the blob parse");
    if (ok) check(ok->nullifiers.size() == 2, "into the two nullifiers declared");

    struct Case {
        std::vector<std::uint32_t> lens;
        Bytes blob;
        const char* why;
    };
    const std::vector<Case> cases = {
        {{4, 4}, b("aaaa"), "second entry absent"},
        {{4, 4}, b("aaaabbb"), "blob one byte short"},
        {{1u << 20}, b("aaaa"), "length beyond any blob"},
        {{4}, {}, "declared against nothing"},
        {{4}, b("aaaabbbb"), "blob bytes no length claims"},
        {{}, b("aaaa"), "blob with no lengths at all"},
    };
    for (const auto& c : cases) {
        check_err(parse_transaction(view(tx_frame(c.lens, c.blob))), wire::kErrLength, c.why);
    }
}

void a_declared_length_never_sizes_an_allocation() {
    std::printf("\na peer's number never becomes memory\n");
    // Whatever the decoder does with a dishonest length vector, it must not turn
    // a peer's number into an allocation.
    check_err(parse_transaction(view(tx_frame({0xFFFFFFFF, 0xFFFFFFFF}, b("aaaa")))),
              wire::kErrLength, "two 4-billion-byte nullifiers over a 4-byte blob are refused");
}

// block_around hand-assembles a block frame holding one transaction blob, so the
// blob can carry padding an honest encoder never writes.
Bytes block_around(const Bytes& tx_blob) {
    zap::Builder bld(zap::kHeaderSize + kBlkSize + int(tx_blob.size()) + 256);
    const int tx_off = wire::write_u32_list(bld, {std::uint32_t(tx_blob.size())});
    auto ob = bld.start_object(kBlkSize);
    const Id parent{};
    ob.set_bytes_fixed(0, view(parent));
    ob.set_u64(32, 1);
    ob.set_u64(40, 1'600'000'000);
    ob.set_list(48, tx_off, 1);
    ob.set_bytes(56, view(tx_blob));
    ob.set_bytes(64, view(b("root")));
    ob.finish_as_root();
    return bld.finish();
}

void a_block_refuses_a_padded_transaction() {
    std::printf("\na block refuses a padded transaction inside it\n");

    // The block frame's own trailing-byte check cannot see padding INSIDE a
    // transaction blob, because that blob carries its own length and the outer
    // size stays honest. Only the inner frame can refuse it. If it does not, the
    // block parses and re-encodes to bytes that are not the bytes that arrived,
    // so the block a node stores under an id is not the block its peers gossiped
    // under that id.
    const Transaction tx = block_tx(3);
    const Bytes raw = tx.marshal();

    Block honest;
    check_ok(parse_block_bytes(view(block_around(raw)), honest),
             "the same shape without padding parses");
    check(honest.txs.size() == 1 && honest.txs[0] == tx, "into the transaction it held");

    Block padded_blk;
    check_err(parse_block_bytes(view(block_around(with_tail(raw, {0xAA, 0xBB, 0xCC, 0xDD}))),
                                padded_blk),
              wire::kErrTrailingBytes, "padding inside the transaction blob is refused");
}

void a_block_reencodes_to_the_bytes_it_parsed() {
    std::printf("\na block re-encodes to the bytes it parsed\n");
    Block blk;
    blk.parent_id = id_of(0xEE);
    blk.block_height = 7;
    blk.block_timestamp = 1'600'000'000;
    blk.txs = {block_tx(1), block_tx(2)};
    blk.state_root = b("state-root");
    const Bytes raw = blk.marshal();

    Block got;
    check_ok(parse_block_bytes(view(raw), got), "it parses");
    check(got.txs == blk.txs, "with the transactions it carried");
    check_eq(hex_of(got.marshal()), hex_of(raw), "and re-encodes to the same bytes");

    for (const Bytes& tail : {Bytes{0}, Bytes{0xFF}, Bytes(64, 0)}) {
        Block trailing;
        check_err(parse_block_bytes(view(with_tail(raw, tail)), trailing),
                  wire::kErrTrailingBytes, "a padded block frame is refused");
    }
}

void an_absent_proof_is_distinct_from_a_present_one() {
    std::printf("\nan absent proof and a proof of nothing are different values\n");
    Transaction none = shield_tx();
    none.proof.reset();
    auto back = parse_transaction(view(none.marshal()));
    check_ok(back, "a transaction with no proof round-trips");
    if (back) check(!back->proof.has_value(), "and still has no proof");

    Transaction empty = shield_tx();
    empty.proof = ZkProof{"", {}, {}};
    auto back2 = parse_transaction(view(empty.marshal()));
    check_ok(back2, "a transaction with an empty proof round-trips");
    if (back2) check(back2->proof.has_value(), "and still HAS one");
    check(none.compute_id() != empty.compute_id(),
          "the two do not share an identity");
}

}  // namespace

int main() {
    utxo_round_trip();
    transaction_round_trip();
    lengths_must_cover_their_blob();
    a_declared_length_never_sizes_an_allocation();
    a_block_refuses_a_padded_transaction();
    a_block_reencodes_to_the_bytes_it_parsed();
    an_absent_proof_is_distinct_from_a_present_one();
    return report("wire");
}
