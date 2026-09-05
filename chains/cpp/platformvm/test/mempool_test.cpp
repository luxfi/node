// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool_test.cpp — what is waiting to go into a block.
//
// Ported from Go vms/txs/mempool/mempool_test.go and
// vms/platformvm/txs/mempool/mempool_test.go. The Go tests use a dummy
// transaction with a made-up size and made-up inputs; these use real ones,
// because the pool's rules are about a transaction's real size and the real
// outputs it would spend.

#include "harness.hpp"
#include "lux/platformvm/mempool.hpp"
#include "signing.hpp"

using namespace lux::platformvm;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

const Id kLux = id_of(0x10);
const Id kPChain = id_of(0x20);

OutputOwners owners() { return OutputOwners{0, 1, {short_of(0x30)}}; }

txs::Tx seal(std::shared_ptr<txs::UnsignedTx> u) {
    txs::Tx tx;
    tx.unsigned_tx = std::move(u);
    (void)tx.initialize();
    return tx;
}

// A payment spending one named output, with an amount that makes each one a
// distinct transaction.
txs::Tx pay(std::uint8_t utxo, std::uint32_t index, std::uint64_t amount) {
    BaseTx b;
    b.network_id = 96369;
    b.blockchain_id = kPChain;
    b.outs = {TransferableOutput{kLux, 0, TransferOutput{amount - 1, owners()}}};
    b.ins = {TransferableInput{UtxoId{id_of(utxo), index}, kLux, 0, TransferInput{amount, {0}}}};
    return seal(txs::BaseTxUnsigned::create(b).value());
}

// A bond, so the pool has something with a start time to drop.
txs::Tx bond(std::uint8_t utxo, std::uint64_t start, std::uint64_t end) {
    BaseTx b;
    b.network_id = 96369;
    b.blockchain_id = kPChain;
    b.ins = {TransferableInput{UtxoId{id_of(utxo), 0}, kLux, 0, TransferInput{2'000'000, {0}}}};
    static pvmtest::BlsKey key(3);
    auto u = txs::AddPermissionlessValidatorTx::create(
        b, txs::Validator{node_of(0x90), start, end, 2'000'000}, kPrimaryNetworkId,
        signer::Signer{key.pop()},
        {TransferableOutput{kLux, 0, TransferOutput{2'000'000, owners()}}}, txs::Owner{0, 1, {short_of(0x30)}},
        txs::Owner{0, 1, {short_of(0x30)}}, 20'000);
    return seal(u.value());
}

}  // namespace

// The cheap refusals, in the order a node can afford to make them: what is
// already here, what is too big to relay, and what there is no room for.
TEST(APoolRefusesWhatItShouldNotSpendOn) {
    mempool::Pool pool;
    const auto a = pay(0x40, 0, 1'000);
    REQUIRE_OK(pool.add(a));
    REQUIRE_EQ_NUM(1, pool.size());
    REQUIRE(pool.has(a.tx_id));

    // The same transaction twice is one transaction.
    REQUIRE_ERR(pool.add(a), Err::DuplicateTx);
    REQUIRE_EQ_NUM(1, pool.size());

    // A pool with almost no room refuses what will not fit, and takes it once
    // there is room.
    mempool::Pool tight(a.bytes.size() - 1);
    REQUIRE_ERR(tight.add(a), Err::MempoolFull);
    mempool::Pool exact(a.bytes.size());
    REQUIRE_OK(exact.add(a));
    REQUIRE_U64(0u, exact.bytes_available());
}

// Two transactions spending one output cannot both be accepted, so holding both
// is holding one of them for nothing.
TEST(OnlyOneRivalForAnOutputWaits) {
    mempool::Pool pool;
    REQUIRE_OK(pool.add(pay(0x40, 0, 1'000)));
    REQUIRE_ERR(pool.add(pay(0x40, 0, 2'000)), Err::ConflictsWithOtherTx);
    REQUIRE_EQ_NUM(1, pool.size());

    // A different output of the same transaction is a different output.
    REQUIRE_OK(pool.add(pay(0x40, 1, 1'000)));
    REQUIRE_EQ_NUM(2, pool.size());
}

// The chain's own transaction. Nobody submits it — the builder produces it from
// state — so one arriving from outside is either a mistake or an attempt to
// choose the chain's own business.
TEST(TheChainsOwnTransactionIsNotSubmitted) {
    mempool::Pool pool;
    REQUIRE_ERR(pool.add(seal(txs::RewardValidatorTx::create(id_of(0x77)))),
                Err::CantIssueRewardValidatorTx);
    REQUIRE(pool.empty());
}

// Order is insertion order. A pool that reordered by fee would be a policy, and
// policy about whose transaction goes first is not a thing to decide quietly.
TEST(ThePoolIsInOrderOfArrival) {
    mempool::Pool pool;
    std::vector<Id> arrived;
    for (std::uint8_t i = 0; i < 5; ++i) {
        const auto tx = pay(static_cast<std::uint8_t>(0x40 + i), 0, 1'000);
        REQUIRE_OK(pool.add(tx));
        arrived.push_back(tx.tx_id);
    }
    REQUIRE(pool.peek()->tx_id == arrived.front());

    const auto three = pool.peek(3);
    REQUIRE_EQ_NUM(3, three.size());
    for (std::size_t i = 0; i < three.size(); ++i) REQUIRE(three[i].tx_id == arrived[i]);

    // Asking for more than there is gives what there is.
    REQUIRE_EQ_NUM(5, pool.peek(50).size());
    REQUIRE(!mempool::Pool().peek().has_value());
}

// A transaction that was accepted makes every rival for its outputs
// unacceptable forever, so removing it removes them too — even though the rival
// is not the transaction being removed.
TEST(RemovingATransactionRemovesItsRivals) {
    mempool::Pool pool;
    const auto waiting = pay(0x40, 0, 1'000);
    REQUIRE_OK(pool.add(waiting));
    REQUIRE_OK(pool.add(pay(0x50, 0, 1'000)));
    const std::size_t room = pool.bytes_available();

    // The rival was accepted somewhere else. It is not in the pool at all.
    const auto rival = pay(0x40, 0, 2'000);
    REQUIRE(!pool.has(rival.tx_id));
    pool.remove({rival});

    REQUIRE(!pool.has(waiting.tx_id));
    REQUIRE_EQ_NUM(1, pool.size());
    REQUIRE(pool.bytes_available() > room);

    // And removing what IS here takes it out by name.
    const auto other = pay(0x50, 0, 1'000);
    pool.remove({other});
    REQUIRE(pool.empty());
    REQUIRE_U64(mempool::kMaxSize, pool.bytes_available());
}

// Why a transaction is not here, for as long as that is a courtesy rather than
// a record the chain owes anyone.
TEST(APoolRemembersWhyItRefused) {
    mempool::Pool pool;
    const auto tx = pay(0x40, 0, 1'000);
    const Error reason{Err::InvalidState, "a reason"};

    pool.mark_dropped(tx.tx_id, reason);
    REQUIRE(pool.drop_reason(tx.tx_id).has_value());
    REQUIRE(pool.drop_reason(tx.tx_id)->code == Err::InvalidState);

    // A transaction that is here is not a transaction that was refused, and
    // saying so afterwards does not make it one.
    REQUIRE_OK(pool.add(tx));
    REQUIRE(!pool.drop_reason(tx.tx_id).has_value());
    pool.mark_dropped(tx.tx_id, reason);
    REQUIRE(!pool.drop_reason(tx.tx_id).has_value());

    // A full pool says nothing about the transaction, only about the moment it
    // arrived, so it is not held against it.
    const auto other = pay(0x50, 0, 1'000);
    pool.mark_dropped(other.tx_id, Error{Err::MempoolFull, "no room"});
    REQUIRE(!pool.drop_reason(other.tx_id).has_value());

    // And only so many reasons are kept.
    mempool::Pool small;
    std::vector<Id> ids;
    for (std::size_t i = 0; i < mempool::kDroppedRemembered + 1; ++i) {
        Id id{};
        id[0] = static_cast<std::uint8_t>(i);
        id[1] = 0xEE;
        small.mark_dropped(id, reason);
        ids.push_back(id);
    }
    REQUIRE(!small.drop_reason(ids.front()).has_value());
    REQUIRE(small.drop_reason(ids.back()).has_value());
}

// A bond that would have started before this moment can no longer be admitted,
// so holding it is holding something that can only ever be refused.
TEST(ABondThatCanNoLongerStartIsDropped) {
    mempool::Pool pool;
    const auto early = bond(0x40, 100, 1'000);
    const auto later = bond(0x50, 900, 1'000);
    const auto payment = pay(0x60, 0, 1'000);
    REQUIRE_OK(pool.add(early));
    REQUIRE_OK(pool.add(later));
    REQUIRE_OK(pool.add(payment));

    const auto dropped = pool.drop_expired_stakers(500);
    REQUIRE_EQ_NUM(1, dropped.size());
    REQUIRE(dropped.front() == early.tx_id);
    REQUIRE(!pool.has(early.tx_id));
    // A bond still ahead of the moment stays, and a payment has no start to be
    // past.
    REQUIRE(pool.has(later.tx_id));
    REQUIRE(pool.has(payment.tx_id));
}
