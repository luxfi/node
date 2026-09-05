// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis_test.cpp — the state the network starts in.
//
// Ported from Go vms/platformvm/genesis/genesis_test.go and the UTXO envelope
// in github.com/luxfi/utxo/wire. The blob under test is the one the Go package
// wrote, so a chain that boots from these bytes boots into the state the Go
// node boots into — which is the only sense in which two implementations start
// the same network.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/genesis.hpp"
#include "signing.hpp"

#include <string>

using namespace lux::platformvm;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

std::vector<std::uint8_t> unhex(const char* s) {
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        return -1;
    };
    std::vector<std::uint8_t> out;
    for (std::size_t i = 0; s[i] != 0 && s[i + 1] != 0; i += 2)
        out.push_back(static_cast<std::uint8_t>(nib(s[i]) * 16 + nib(s[i + 1])));
    return out;
}

// The same fixtures the golden generator used.
UTXO reference_utxo() {
    UTXO u;
    u.utxo = UtxoId{id_of(0x90), 2};
    u.asset = id_of(0x10);
    u.out = TransferOutput{4242, OutputOwners{5, 1, {short_of(0x30)}}};
    return u;
}

UTXO locked_utxo() {
    UTXO u;
    u.utxo = UtxoId{id_of(0x91), 0};
    u.asset = id_of(0x10);
    u.stake_lock = 777;
    u.out = TransferOutput{99, OutputOwners{0, 1, {short_of(0x40)}}};
    return u;
}

BaseTx base_fixture() {
    const Id asset = id_of(0x10);
    BaseTx b;
    b.network_id = 96369;
    b.blockchain_id = id_of(0x20);
    b.outs = {
        TransferableOutput{asset, 0,
                           TransferOutput{1'000'000,
                                          OutputOwners{7, 2, {short_of(0x30), short_of(0x40), short_of(0x50)}}}},
        TransferableOutput{asset, 999, TransferOutput{42, OutputOwners{0, 1, {short_of(0x30)}}}},
    };
    b.ins = {TransferableInput{UtxoId{id_of(0x60), 1}, asset, 999, TransferInput{1'000'042, {0, 1}}}};
    const std::string memo = "native-zap";
    b.memo.assign(memo.begin(), memo.end());
    return b;
}

signer::ProofOfPossession pop_fixture() {
    signer::ProofOfPossession p{};
    for (std::size_t i = 0; i < signer::kPublicKeyLen; ++i) p.public_key[i] = static_cast<std::uint8_t>(i);
    for (std::size_t i = 0; i < signer::kSignatureLen; ++i)
        p.proof[i] = static_cast<std::uint8_t>(255 - static_cast<int>(i));
    return p;
}

txs::Tx unsigned_only(std::shared_ptr<txs::UnsignedTx> u) {
    txs::Tx t;
    t.unsigned_tx = std::move(u);
    (void)t.initialize();
    return t;
}

genesis::Genesis reference_genesis() {
    genesis::Genesis g;
    g.timestamp = 1'600'000'000;
    g.initial_supply = 720'000'000'000'000;
    g.message = "hello genesis";
    const std::string alloc = "alloc-1";
    g.utxos.push_back(genesis::Allocation{reference_utxo(), {alloc.begin(), alloc.end()}});

    const signer::Signer sig = pop_fixture();
    const std::vector<TransferableOutput> stake = {
        TransferableOutput{id_of(0x10), 0, TransferOutput{2'000'000, OutputOwners{0, 1, {short_of(0x40)}}}}};
    const txs::Owner rewards{3, 1, {short_of(0x30)}};
    auto apv = txs::AddPermissionlessValidatorTx::create(
        base_fixture(), txs::Validator{node_of(0x93), 103, 203, 2'000'000}, kEmptyId, sig, stake, rewards,
        rewards, 20000);
    g.validators.push_back(unsigned_only(apv.value()));

    const std::string gen = "genesis-bytes";
    auto cc = txs::CreateChainTx::create(base_fixture(), id_of(0x72), "My Chain 7", id_of(0x73),
                                          {id_of(0x01), id_of(0x02)},
                                          std::vector<std::uint8_t>(gen.begin(), gen.end()), txs::Auth{0, 2});
    g.chains.push_back(unsigned_only(cc.value()));
    return g;
}

// The reference genesis's validator carries the golden proof-of-possession
// bytes, which are a fixture and not a real proof — the blob under test is the
// one Go wrote, and Go does not verify a proof to write bytes. Every check
// ABOUT a genesis therefore runs against a validator of its own network, which
// registers no key at all, so nothing has to be proved about one.
genesis::Genesis verifiable_genesis(std::uint64_t weight = 2'000'000, std::uint64_t end = 203) {
    genesis::Genesis g = reference_genesis();
    g.timestamp = 0;
    const std::vector<TransferableOutput> stake = {
        TransferableOutput{id_of(0x10), 0,
                           TransferOutput{2'000'000, OutputOwners{0, 1, {short_of(0x40)}}}}};
    const txs::Owner rewards{3, 1, {short_of(0x30)}};
    auto apv = txs::AddPermissionlessValidatorTx::create(
        base_fixture(), txs::Validator{node_of(0x93), 103, end, weight}, id_of(0x40),
        signer::Signer{signer::Empty{}}, stake, rewards, rewards, 20000);
    g.validators[0] = unsigned_only(apv.value());
    return g;
}

}  // namespace

// The cross-chain UTXO envelope: what genesis stores and what crosses to
// another chain. One encoding, both ways.
TEST(TheUTXOEnvelope) {
    REQUIRE_EQ(std::string(pvmgold::utxo_wire), hex(reference_utxo().wire_bytes()));
    REQUIRE_EQ(std::string(pvmgold::utxo_wire_locked), hex(locked_utxo().wire_bytes()));

    auto back = UTXO::from_wire_bytes(reference_utxo().wire_bytes());
    REQUIRE_OK(back);
    REQUIRE_EQ(reference_utxo(), back.value());

    auto locked_back = UTXO::from_wire_bytes(locked_utxo().wire_bytes());
    REQUIRE_OK(locked_back);
    REQUIRE_EQ(locked_utxo(), locked_back.value());
    // The lock is a VALUE on the output here, not a second type, and it comes
    // back as one.
    REQUIRE_U64(777u, locked_back.value().stake_lock);
}

// A UTXO this chain cannot spend is not a UTXO it should pretend to hold.
TEST(AnUnspendableEnvelopeIsRefused) {
    auto b = reference_utxo().wire_bytes();
    REQUIRE_ERR(UTXO::from_wire_bytes(std::vector<std::uint8_t>{0x00}), Err::BufferTooSmall);

    // Relabelled as some other shape.
    auto wrong_shape = b;
    wrong_shape[1] = 0x0B;
    REQUIRE_ERR(UTXO::from_wire_bytes(wrong_shape), Err::UnsupportedFxOutput);

    // A UTXO whose OUTPUT belongs to one of the six other signature families.
    auto foreign = reference_utxo().wire_bytes();
    // Find the inner envelope's discriminator and relabel the family.
    const auto inner = reference_utxo().out;
    TransferableOutput as_out{reference_utxo().asset, 0, inner};
    auto inner_bytes = as_out.wire_bytes();
    inner_bytes[0] = 0x02;  // an ML-DSA output: real, and not one this chain spends
    REQUIRE_ERR(TransferableOutput::from_wire_bytes(inner_bytes, id_of(0x10)), Err::UnsupportedFxOutput);
}

// The genesis blob: the bytes the Go package wrote, and every field back out of
// them. A chain that boots from these boots into the state the Go node boots
// into.
TEST(TheGenesisBlob) {
    const auto g = reference_genesis();
    REQUIRE_EQ(std::string(pvmgold::genesis_blob), hex(g.encode()));

    auto back = genesis::Genesis::parse(unhex(pvmgold::genesis_blob));
    REQUIRE_OK(back);
    const auto& p = back.value();
    REQUIRE_U64(1'600'000'000u, p.timestamp);
    REQUIRE_U64(720'000'000'000'000u, p.initial_supply);
    REQUIRE_EQ(std::string("hello genesis"), p.message);

    REQUIRE_EQ_NUM(1, p.utxos.size());
    REQUIRE_EQ(reference_utxo(), p.utxos[0].utxo);
    const std::string alloc = "alloc-1";
    REQUIRE_EQ(std::vector<std::uint8_t>(alloc.begin(), alloc.end()), p.utxos[0].message);

    // The transactions come back from their own SIGNED bytes, so their ids are
    // what they were when the genesis was written.
    REQUIRE_EQ_NUM(1, p.validators.size());
    REQUIRE_EQ(g.validators[0].tx_id, p.validators[0].tx_id);
    REQUIRE(p.validators[0].unsigned_tx->kind() == txs::Kind::AddPermissionlessValidator);

    REQUIRE_EQ_NUM(1, p.chains.size());
    REQUIRE_EQ(g.chains[0].tx_id, p.chains[0].tx_id);
    const auto* chain = dynamic_cast<const txs::CreateChainTx*>(p.chains[0].unsigned_tx.get());
    REQUIRE(chain != nullptr);
    REQUIRE_EQ(std::string("My Chain 7"), chain->blockchain_name());
}

// An empty genesis is a legitimate genesis — a network with no money and no
// validators is a network that has not started, not a malformed one.
TEST(AnEmptyGenesisRoundTrips) {
    genesis::Genesis g;
    g.timestamp = 42;
    g.initial_supply = 0;
    auto back = genesis::Genesis::parse(g.encode());
    REQUIRE_OK(back);
    REQUIRE_U64(42u, back.value().timestamp);
    REQUIRE(back.value().utxos.empty());
    REQUIRE(back.value().validators.empty());
    REQUIRE(back.value().chains.empty());
    REQUIRE_OK(back.value().verify());
}

// Go: the genesis refusals. Each is a state no transaction could have produced,
// so a chain that started there could never be reasoned about.
TEST(GenesisRefusals) {
    {  // money that is not money
        genesis::Genesis g = verifiable_genesis();
        g.utxos[0].utxo.out.amt = 0;
        REQUIRE_ERR(g.verify(), Err::BadGenesis);
    }
    {  // a validator nobody can vote toward
        REQUIRE_ERR(verifiable_genesis(0).verify(), Err::BadGenesis);
    }
    {  // a validator whose term is already over would have to be removed by the
       // first block, which is a state no transaction could have produced
        genesis::Genesis g = verifiable_genesis(2'000'000, 203);
        g.timestamp = 1'000'000'000;
        REQUIRE_ERR(g.verify(), Err::BadGenesis);
    }
    {  // a key nobody holds is a validator nobody is. The refusal names the
       // PROOF rather than the genesis, because that is the fact that is wrong.
        genesis::Genesis g = reference_genesis();
        g.timestamp = 0;
        REQUIRE_ERR(g.verify(), Err::InvalidPublicKey);
    }
    {  // a "chain" that does not create a chain
        genesis::Genesis g = verifiable_genesis();
        auto base = txs::BaseTxUnsigned::create(base_fixture());
        g.chains[0] = unsigned_only(base.value());
        REQUIRE_ERR(g.verify(), Err::BadGenesis);
    }
    {  // and a well-formed one passes
        REQUIRE_OK(verifiable_genesis().verify());
    }
}

// A truncated blob is a refusal, not a partial genesis.
TEST(AMalformedBlobIsRefused) {
    REQUIRE_ERR(genesis::Genesis::parse(std::vector<std::uint8_t>{1, 2, 3}), Err::BadGenesis);

    // A length that overruns the blob it indexes into.
    auto g = reference_genesis();
    auto b = g.encode();
    const std::int64_t root = static_cast<std::int64_t>(
        static_cast<std::uint32_t>(b[8]) | (static_cast<std::uint32_t>(b[9]) << 8) |
        (static_cast<std::uint32_t>(b[10]) << 16) | (static_cast<std::uint32_t>(b[11]) << 24));
    const std::int64_t slot = root + 24;  // the utxo length list
    const std::int32_t rel = static_cast<std::int32_t>(
        static_cast<std::uint32_t>(b[slot]) | (static_cast<std::uint32_t>(b[slot + 1]) << 8) |
        (static_cast<std::uint32_t>(b[slot + 2]) << 16) | (static_cast<std::uint32_t>(b[slot + 3]) << 24));
    const std::int64_t list = slot + rel;
    b[list] = 0xff;
    b[list + 1] = 0xff;
    REQUIRE_ERR(genesis::Genesis::parse(b), Err::BadGenesis);
}
