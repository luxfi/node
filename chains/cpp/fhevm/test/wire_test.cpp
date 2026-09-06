// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire_test.cpp — struct-is-wire, and the one encoding each value has.
//
// Ported case for case from the Go F-Chain's wire_test.go. The cross-language
// bytes are checked in differential_test; what this file adds is every way a
// buffer can fail to be a transaction or a block — which is the half an
// adversary controls.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/block.hpp"
#include "lux/fhevm/transaction.hpp"
#include <zap/zap.hpp>

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

// The Go harness's sampleTx, field for field. It is deliberately NOT signed:
// what this file tests is the encoding, and a real signature would only make
// the vectors depend on a key.
Transaction sample_tx() {
    Transaction tx;
    tx.type = kTxRegisterCiphertext;
    tx.scheme = "ckks-n14";
    tx.subject = Id{9, 8, 7, 6, 5, 4, 3, 2, 1};
    tx.gas_limit = 81000;
    tx.nonce = 42;
    constexpr std::string_view payload = "register-ciphertext-payload";
    tx.payload.assign(payload.begin(), payload.end());
    constexpr std::string_view auth = "payer-public-key-bytes";
    tx.auth.assign(auth.begin(), auth.end());
    constexpr std::string_view sig = "payer-signature-bytes";
    tx.sig.assign(sig.begin(), sig.end());
    constexpr std::string_view payer = "payer-address-20byte";
    std::copy(payer.begin(), payer.end(), tx.payer.begin());
    return tx;
}

// The offsets are the format. A test that could not name them would be testing
// the implementation against itself.
constexpr int kTxPayer = 1;
constexpr int kTxSubject = 21;

std::size_t declared_len(const Bytes& b) { return zap::load_u32(b.data() + 12); }

void set_declared_len(Bytes& b, std::size_t at, std::uint32_t n) {
    zap::store_u32(b.data() + at + 12, n);
}

// wire_vm is the least VM a block needs to name itself: a chain id. A block id
// commits to the chain it belongs to, so there is no such thing as a block
// without one.
struct WireVM {
    Memory store;
    std::unique_ptr<VM> vm;
    WireVM() : vm(std::make_unique<VM>(&store, VM::Config{96369, test_chain_id(), "F"})) {}
};

std::shared_ptr<Block> block_of(VM& vm, Id parent, std::uint64_t height, std::int64_t ts,
                                std::vector<Transaction> txs) {
    return std::make_shared<Block>(&vm, parent, height, ts, std::move(txs));
}

void canonical_round_trip() {
    Transaction tx = sample_tx();
    Bytes data = tx.bytes();

    auto parsed = parse_transaction(view(data));
    accepted(parsed, "the canonical encoding parses");
    if (!parsed) return;

    check_eq(hex_of(parsed->bytes()), hex_of(data), "and re-serializes to exactly what arrived");
    check_eq(hex_of(parsed->id()), hex_of(sha256(view(data))), "the id is sha256 of the wire");

    check(parsed->type == tx.type && parsed->scheme == tx.scheme &&
              parsed->gas_limit == tx.gas_limit && parsed->nonce == tx.nonce &&
              parsed->payer == tx.payer && parsed->subject == tx.subject &&
              parsed->payload == tx.payload && parsed->auth == tx.auth && parsed->sig == tx.sig,
          "every field survives the seam");
}

void content_is_the_prefix_the_signature_covers() {
    // What is authenticated and what is transmitted cannot come apart: the
    // content object is a genuine byte-prefix of the wire, and the signed
    // preimage ENDS with that same prefix. The only thing the signature covers
    // that the wire does not carry is the chain id, which the verifier supplies.
    Transaction tx = sample_tx();
    Bytes content = tx.content();
    Bytes full = tx.bytes();
    check(content.size() <= full.size() &&
              std::equal(content.begin(), content.end(), full.begin()),
          "content() is a prefix of bytes()");
    check_eq(declared_len(full), content.size(),
             "and the leading message's declared length names exactly it");

    Bytes pre = tx.signing_bytes(test_chain_id());
    check(pre.size() >= content.size() &&
              std::equal(content.begin(), content.end(), pre.end() - std::int64_t(content.size())),
          "the signed preimage ends with the transmitted content");
    constexpr std::string_view domain = "fhevm/tx/";
    check_eq(pre.size(), domain.size() + 32 + content.size(),
             "and is exactly domain + chain + content");
    check(std::equal(domain.begin(), domain.end(), pre.begin()),
          "the preimage is domain-separated from everything else this chain hashes");
}

void signing_bytes_bind_the_chain() {
    Transaction tx = sample_tx();
    Id other{'o', 't', 'h', 'e', 'r'};
    check(hex_of(tx.signing_bytes(test_chain_id())) != hex_of(tx.signing_bytes(other)),
          "two chains must not produce one preimage");
}

void signing_bytes_bind_every_field() {
    const std::string base = hex_of(sample_tx().signing_bytes(test_chain_id()));
    struct Case {
        const char* name;
        void (*mutate)(Transaction&);
    } cases[] = {
        {"type", [](Transaction& t) { t.type = kTxGrantPermit; }},
        {"scheme", [](Transaction& t) { t.scheme = "bfv-n13"; }},
        {"payer", [](Transaction& t) { t.payer[0] ^= 0xff; }},
        {"subject", [](Transaction& t) { t.subject[0] ^= 0xff; }},
        {"gas limit", [](Transaction& t) { ++t.gas_limit; }},
        {"nonce", [](Transaction& t) { ++t.nonce; }},
        {"payload", [](Transaction& t) { t.payload.push_back('x'); }},
    };
    for (const auto& c : cases) {
        Transaction tx = sample_tx();
        c.mutate(tx);
        check(hex_of(tx.signing_bytes(test_chain_id())) != base,
              std::string("changing the ") + c.name + " changes the signed preimage");
    }
    // Auth and sig are deliberately EXCLUDED: a signature cannot cover itself.
    Transaction tx = sample_tx();
    constexpr std::string_view other_key = "a-different-public-key";
    tx.auth.assign(other_key.begin(), other_key.end());
    constexpr std::string_view other_sig = "a-different-signature";
    tx.sig.assign(other_sig.begin(), other_sig.end());
    check_eq(hex_of(tx.signing_bytes(test_chain_id())), base,
             "auth and sig do not appear in the signed preimage");
}

void non_canonical_is_refused() {
    // ZAP follows the root offset and ignores unreferenced padding inside a
    // message's declared size, so a twin buffer can decode to identical fields
    // yet hash differently. Exactly one byte-string authenticates per logical
    // transaction, or the id is malleable.
    Transaction tx = sample_tx();
    Bytes data = tx.bytes();
    std::size_t n = declared_len(data);

    Bytes sig(data.begin() + std::int64_t(n), data.end());
    sig.insert(sig.end(), 8, 0);
    zap::store_u32(sig.data() + 12, std::uint32_t(sig.size()));
    Bytes twin(data.begin(), data.begin() + std::int64_t(n));
    twin.insert(twin.end(), sig.begin(), sig.end());

    check(hex_of(twin) != hex_of(data), "the twin differs from the canonical form");
    refused(parse_transaction(view(twin)), Err::InvalidPayload,
            "a padded twin is refused — id malleability is closed");
}

void truncation_and_trailing_are_refused() {
    Bytes data = sample_tx().bytes();
    for (std::size_t cut : {std::size_t(0), std::size_t(4), std::size_t(zap::kHeaderSize),
                            data.size() - 1}) {
        refused(parse_transaction(ByteView(data.data(), cut)), Err::InvalidPayload,
                "a buffer cut to " + std::to_string(cut) + " bytes");
    }
    Bytes trailing = data;
    trailing.push_back(0xde);
    trailing.push_back(0xad);
    refused(parse_transaction(view(trailing)), Err::InvalidPayload,
            "bytes appended after a complete transaction");
}

void a_transaction_must_carry_both_messages() {
    // The second of a transaction's two concatenated messages must actually be
    // there. A declared length covering the whole buffer leaves nothing for the
    // auth/sig object, and an empty remainder is not a message.
    Bytes data = sample_tx().bytes();
    zap::store_u32(data.data() + 12, std::uint32_t(data.size()));
    refused(parse_transaction(view(data)), Err::InvalidPayload,
            "a transaction with no auth/sig object");

    // And a trailing object that is a header and nothing else decodes to
    // nothing.
    Bytes sound = sample_tx().bytes();
    std::size_t n = declared_len(sound);
    Bytes broken(sound.begin(), sound.begin() + std::int64_t(n));
    broken.insert(broken.end(), std::size_t(zap::kHeaderSize), 0);
    set_declared_len(broken, n, std::uint32_t(zap::kHeaderSize));
    refused(parse_transaction(view(broken)), Err::InvalidPayload,
            "a trailing object that is not a message");
}

void the_leading_message_is_held_to_more_than_its_length() {
    // The length field alone says nothing: a buffer can name a plausible length
    // and still not be a message — a wrong magic, or a wire version this build
    // does not speak.
    Bytes sound = sample_tx().bytes();
    {
        Bytes bad = sound;
        bad[0] ^= 0xff;
        refused(parse_transaction(view(bad)), Err::InvalidPayload, "a magic this is not");
    }
    {
        Bytes bad = sound;
        zap::store_u16(bad.data() + 4, 0xbeef);
        refused(parse_transaction(view(bad)), Err::InvalidPayload, "a version we do not speak");
    }
    accepted(parse_transaction(view(sound)), "the control");
}

void a_truncated_fixed_field_is_refused_not_zero_filled() {
    // A fixed-width field read past the end of its message copies NOTHING,
    // leaving the destination zeroed. Here it cannot pass: a zeroed field
    // re-serializes to bytes that are not the bytes that arrived, and the
    // canonical rule refuses the difference.
    Bytes sound = sample_tx().bytes();
    std::size_t n = declared_len(sound);
    for (int cut : {kTxPayer + 4, kTxSubject + 4, kTxSubject + 20}) {
        Bytes truncated = sound;
        std::size_t size = std::size_t(zap::kHeaderSize + cut);
        check(size < n, "the cut falls inside the message");
        zap::store_u32(truncated.data() + 12, std::uint32_t(size));
        refused(parse_transaction(view(truncated)), Err::InvalidPayload,
                "a fixed field cut at " + std::to_string(cut) + " refuses rather than zero-fills");
    }
    auto parsed = parse_transaction(view(sound));
    check(parsed && parsed->payer == sample_tx().payer && parsed->subject == sample_tx().subject,
          "the control: uncut, the fixed fields survive intact");
}

void absent_is_not_empty() {
    // An absent field comes back nil rather than as an empty non-nil slice, so
    // a re-serialization of what was parsed is byte-identical to what arrived —
    // which is the whole canonical rule.
    Transaction bare;
    bare.type = kTxRevokePermit;
    bare.nonce = 1;
    auto again = parse_transaction(view(bare.bytes()));
    accepted(again, "a transaction with no scheme and no payload round-trips");
    if (again) {
        check(again->scheme.empty() && again->payload.empty() && again->auth.empty() &&
                  again->sig.empty(),
              "and its absent fields come back absent");
    }
}

void block_round_trip() {
    WireVM w;
    Transaction a = sample_tx();
    Transaction b = sample_tx();
    b.nonce = 43;
    constexpr std::string_view second = "second";
    b.payload.assign(second.begin(), second.end());

    auto blk = block_of(*w.vm, Id{1, 2, 3}, 7, 1'700'000'000, {a, b});
    auto parsed = w.vm->parse_block(blk->bytes());
    accepted(parsed, "a block re-parses");
    if (!parsed) return;
    check_eq(hex_of(Id((*parsed)->id())), hex_of(Id(blk->id())), "with the same id");
    check(( *parsed)->height() == blk->height() && Id((*parsed)->parent()) == Id(blk->parent()) &&
              (*parsed)->timestamp() == blk->timestamp(),
          "and the same header");
    check((*parsed)->transactions().size() == 2, "and both transactions");
    for (std::size_t i = 0; i < 2; ++i) {
        check_eq(hex_of((*parsed)->transactions()[i].id()),
                 hex_of(blk->transactions()[i].id()),
                 "transaction " + std::to_string(i) + " keeps its id across the wire");
    }
}

void block_id_binds_the_chain() {
    // Two chains sharing a genesis timestamp must not share a genesis id. They
    // did: the id hashed parent, height, time and transactions, none of which
    // distinguishes one F-Chain from another, so a block built on one resolved
    // its parent on all of them.
    auto genesis_id = [](const Id& chain) {
        Memory store;
        VM vm(&store, VM::Config{96369, chain, "F"});
        auto blk = std::make_shared<Block>(&vm, kEmptyId, 0, kTestGenesisTime,
                                           std::vector<Transaction>{});
        return blk->compute_id();
    };
    Id other{'o', 't', 'h', 'e', 'r'};
    check(hex_of(genesis_id(test_chain_id())) != hex_of(genesis_id(other)),
          "two chains do not share a genesis id");
    check_eq(hex_of(genesis_id(test_chain_id())), hex_of(genesis_id(test_chain_id())),
             "and a block id is a function of its content, not of the call");
}

// block_bytes_with_tx_lens rebuilds a block's wire form with a length list the
// caller chooses, so the parser can be tested against lengths a builder would
// never produce. It is block_bytes verbatim apart from that one substitution.
Bytes block_bytes_with_tx_lens(const Id& parent, std::uint64_t height, std::int64_t ts,
                               const std::vector<Transaction>& txs,
                               const std::vector<std::uint32_t>& lens, const Bytes& blob_pad) {
    Bytes blob;
    for (const auto& tx : txs) {
        Bytes b = tx.bytes();
        blob.insert(blob.end(), b.begin(), b.end());
    }
    blob.insert(blob.end(), blob_pad.begin(), blob_pad.end());

    zap::Builder bld(zap::kHeaderSize + 64 + int(blob.size()) + 4 * int(lens.size()) + 128);
    auto lb = bld.start_list(4);
    for (std::uint32_t x : lens) lb.add_u32(x);
    auto [off, len] = lb.finish();
    auto ob = bld.start_object(64);
    ob.set_bytes_fixed(0, view(parent));
    ob.set_u64(32, height);
    ob.set_u64(40, static_cast<std::uint64_t>(ts));
    ob.set_list(48, off, len);
    ob.set_bytes(56, view(blob));
    ob.finish_as_root();
    return bld.finish();
}

void every_malformed_block_is_refused() {
    WireVM w;
    Transaction tx = sample_tx();
    auto blk = block_of(*w.vm, Id{3}, 4, 1'700'000'000, {tx});
    Bytes sound(blk->bytes().begin(), blk->bytes().end());

    // A header that is not a ZAP message at all.
    Bytes garbage(std::size_t(zap::kHeaderSize + 16), 0);
    zap::store_u32(garbage.data() + 12, std::uint32_t(garbage.size()));
    refused(w.vm->parse_block(view(garbage)), Err::InvalidPayload,
            "a message that decodes to nothing");

    std::size_t tx_len = tx.bytes().size();
    refused(w.vm->parse_block(view(block_bytes_with_tx_lens(
                Id{3}, 4, 1'700'000'000, {tx}, {std::uint32_t(tx_len + 64)}, {}))),
            Err::InvalidPayload, "a length running past the blob");
    refused(w.vm->parse_block(view(block_bytes_with_tx_lens(
                Id{3}, 4, 1'700'000'000, {tx}, {std::uint32_t(tx_len - 8)}, {}))),
            Err::InvalidPayload, "a length that stops short of its transaction");

    Bytes oversize(kMaxBlockSize + 1, 0);
    refused(w.vm->parse_block(view(oversize)), Err::InvalidPayload,
            "a message over the size bound, refused before it is decoded");

    // Bytes inside the transaction blob that no length covers.
    Bytes padded = block_bytes_with_tx_lens(Id{3}, 4, 1'700'000'000, {tx},
                                            {std::uint32_t(tx_len)}, Bytes(4096, 0xAA));
    check(hex_of(padded) != hex_of(sound), "the padded twin differs");
    refused(w.vm->parse_block(view(padded)), Err::InvalidPayload,
            "blob bytes no length covers are refused");

    // And bytes after the message.
    Bytes trailing = sound;
    trailing.push_back(0xff);
    refused(w.vm->parse_block(view(trailing)), Err::InvalidPayload,
            "bytes after the block");

    // A padded twin whose declared size was grown: the root does not reference
    // the padding, so every field still decodes identically.
    Bytes grown = sound;
    grown.insert(grown.end(), 16, 0);
    zap::store_u32(grown.data() + 12, std::uint32_t(grown.size()));
    refused(w.vm->parse_block(view(grown)), Err::InvalidPayload,
            "a block with two encodings is a block with none");

    auto got = w.vm->parse_block(view(sound));
    accepted(got, "the control: the sound encoding parses");
    if (got) check_eq(hex_of(Id((*got)->id())), hex_of(blk->compute_id()), "and names the block");
}

void the_build_budget_never_understates_the_block() {
    // The relation the selection loop depends on: its running total — the empty
    // block plus each transaction's wire length plus kTxEntry — is never LESS
    // than what the block actually serializes to. If it could understate, a
    // proposer would select past the size bound and build a block its own verify
    // refuses, which is the halt-shaped bug of a builder and a checker that
    // disagree. Payload lengths vary per step so the eight-byte alignment is
    // exercised at every offset.
    WireVM w;
    std::vector<Transaction> txs;
    std::size_t budget = empty_block_size();
    bool ok = true;
    for (int n = 0; n <= 24; ++n) {
        auto blk = block_of(*w.vm, Id{7}, std::uint64_t(n), 1, txs);
        if (budget < blk->bytes().size()) {
            std::printf("        %d txs: budget %zu understates %zu bytes\n", n, budget,
                        blk->bytes().size());
            ok = false;
        }
        Transaction tx = sample_tx();
        tx.nonce = std::uint64_t(n);
        tx.payload.assign(std::size_t(n), std::uint8_t(n));
        budget += tx.bytes().size() + kTxEntry;
        txs.push_back(std::move(tx));
    }
    check(ok, "the build budget never understates the block it describes");
}

}  // namespace

int main() {
    std::printf("fhevm — the wire, and the one encoding each value has\n\n");
    canonical_round_trip();
    content_is_the_prefix_the_signature_covers();
    signing_bytes_bind_the_chain();
    signing_bytes_bind_every_field();
    non_canonical_is_refused();
    truncation_and_trailing_are_refused();
    a_transaction_must_carry_both_messages();
    the_leading_message_is_held_to_more_than_its_length();
    a_truncated_fixed_field_is_refused_not_zero_filled();
    absent_is_not_empty();
    block_round_trip();
    block_id_binds_the_chain();
    every_malformed_block_is_refused();
    the_build_budget_never_understates_the_block();
    return report("wire");
}
