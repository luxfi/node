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
//
// Everything here goes through mempool::admit, which is the chain's ONE door
// onto the pool — the same call submit() makes and the same one a rejected
// block's transactions come back through. Testing the pool underneath directly
// would test the door nobody uses.

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

// A door: the pool and the refusals a submitter is told in this chain's words.
struct Door {
    mempool::Pool pool;
    mempool::Dropped dropped;

    explicit Door(std::size_t capacity = mempool::kMaxPoolSize) : pool(capacity) {}
    Status admit(const txs::Tx& tx) { return mempool::admit(pool, dropped, tx); }
};

// The cheap refusals, in the order a node can afford to make them: what is
// already here, what is too big to relay, and what there is no room for.
TEST(APoolRefusesWhatItShouldNotSpendOn) {
    Door d;
    const auto a = pay(0x40, 0, 1'000);
    REQUIRE_OK(d.admit(a));
    REQUIRE_EQ_NUM(1, d.pool.size());
    REQUIRE(d.pool.has(a.tx_id));

    // The same transaction twice is one transaction.
    REQUIRE_ERR(d.admit(a), Err::DuplicateTx);
    REQUIRE_EQ_NUM(1, d.pool.size());

    // A pool with almost no room refuses what will not fit, and takes it once
    // there is room.
    Door tight(a.bytes.size() - 1);
    REQUIRE_ERR(tight.admit(a), Err::MempoolFull);
    Door exact(a.bytes.size());
    REQUIRE_OK(exact.admit(a));
    REQUIRE_U64(0u, exact.pool.bytes_available());
}

// Two transactions spending one output cannot both be accepted, so holding both
// is holding one of them for nothing.
TEST(OnlyOneRivalForAnOutputWaits) {
    Door d;
    REQUIRE_OK(d.admit(pay(0x40, 0, 1'000)));
    REQUIRE_ERR(d.admit(pay(0x40, 0, 2'000)), Err::ConflictsWithOtherTx);
    REQUIRE_EQ_NUM(1, d.pool.size());

    // A different output of the same transaction is a different output.
    REQUIRE_OK(d.admit(pay(0x40, 1, 1'000)));
    REQUIRE_EQ_NUM(2, d.pool.size());
}

// The chain's own transaction. Nobody submits it — the builder produces it from
// state — so one arriving from outside is either a mistake or an attempt to
// choose the chain's own business.
TEST(TheChainsOwnTransactionIsNotSubmitted) {
    Door d;
    REQUIRE_ERR(d.admit(seal(txs::RewardValidatorTx::create(id_of(0x77)))),
                Err::CantIssueRewardValidatorTx);
    REQUIRE(d.pool.empty());
}

// Order is insertion order. A pool that reordered by fee would be a policy, and
// policy about whose transaction goes first is not a thing to decide quietly.
TEST(ThePoolIsInOrderOfArrival) {
    Door d;
    std::vector<Id> arrived;
    for (std::uint8_t i = 0; i < 5; ++i) {
        const auto tx = pay(static_cast<std::uint8_t>(0x40 + i), 0, 1'000);
        REQUIRE_OK(d.admit(tx));
        arrived.push_back(tx.tx_id);
    }
    REQUIRE(d.pool.peek() != nullptr && d.pool.peek()->tx_id == arrived.front());

    const auto three = mempool::oldest(d.pool, 3);
    REQUIRE_EQ_NUM(3, three.size());
    for (std::size_t i = 0; i < three.size(); ++i) REQUIRE(three[i].tx_id == arrived[i]);

    // Asking for more than there is gives what there is.
    REQUIRE_EQ_NUM(5, mempool::oldest(d.pool, 50).size());
    REQUIRE(mempool::Pool().peek() == nullptr);
}

// A transaction that was accepted makes every rival for its outputs
// unacceptable forever, so removing it removes them too — even though the rival
// is not the transaction being removed.
TEST(RemovingATransactionRemovesItsRivals) {
    Door d;
    const auto waiting = pay(0x40, 0, 1'000);
    REQUIRE_OK(d.admit(waiting));
    REQUIRE_OK(d.admit(pay(0x50, 0, 1'000)));
    const std::size_t room = d.pool.bytes_available();

    // The rival was accepted somewhere else. It is not in the pool at all.
    const auto rival = pay(0x40, 0, 2'000);
    REQUIRE(!d.pool.has(rival.tx_id));
    d.pool.remove({rival});

    REQUIRE(!d.pool.has(waiting.tx_id));
    REQUIRE_EQ_NUM(1, d.pool.size());
    REQUIRE(d.pool.bytes_available() > room);

    // And removing what IS here takes it out by name.
    const auto other = pay(0x50, 0, 1'000);
    d.pool.remove({other});
    REQUIRE(d.pool.empty());
    REQUIRE_U64(mempool::kMaxPoolSize, d.pool.bytes_available());
}

// Why a transaction is not here, for as long as that is a courtesy rather than
// a record the chain owes anyone.
TEST(APoolRemembersWhyItRefused) {
    Door d;
    const auto rejected = seal(txs::RewardValidatorTx::create(id_of(0x77)));
    REQUIRE_ERR(d.admit(rejected), Err::CantIssueRewardValidatorTx);
    REQUIRE(d.dropped.why(rejected.tx_id).has_value());
    REQUIRE(d.dropped.why(rejected.tx_id)->code == Err::CantIssueRewardValidatorTx);

    // A transaction that is here is not a transaction that was refused: the
    // duplicate refusal is reported to the caller and NOT recorded against
    // something the pool is holding.
    const auto tx = pay(0x40, 0, 1'000);
    REQUIRE_OK(d.admit(tx));
    REQUIRE_ERR(d.admit(tx), Err::DuplicateTx);
    REQUIRE(!d.dropped.why(tx.tx_id).has_value());

    // A full pool says nothing about the transaction, only about the moment it
    // arrived, so it is not held against it.
    Door tight(10);
    const auto other = pay(0x50, 0, 1'000);
    REQUIRE_ERR(tight.admit(other), Err::MempoolFull);
    REQUIRE(!tight.dropped.why(other.tx_id).has_value());

    // And only so many reasons are kept.
    mempool::Dropped small;
    const Error reason{Err::InvalidState, "a reason"};
    std::vector<Id> ids;
    for (std::size_t i = 0; i < mempool::kDroppedRemembered + 1; ++i) {
        Id id{};
        id[0] = static_cast<std::uint8_t>(i);
        id[1] = 0xEE;
        small.mark(id, reason);
        ids.push_back(id);
    }
    REQUIRE(!small.why(ids.front()).has_value());
    REQUIRE(small.why(ids.back()).has_value());
}

// A bond that would have started before this moment can no longer be admitted,
// so holding it is holding something that can only ever be refused.
TEST(ABondThatCanNoLongerStartIsDropped) {
    Door d;
    const auto early = bond(0x40, 100, 1'000);
    const auto later = bond(0x50, 900, 1'000);
    const auto payment = pay(0x60, 0, 1'000);
    REQUIRE_OK(d.admit(early));
    REQUIRE_OK(d.admit(later));
    REQUIRE_OK(d.admit(payment));

    const auto expired = mempool::drop_expired_stakers(d.pool, 500);
    REQUIRE_EQ_NUM(1, expired.size());
    REQUIRE(expired.front() == early.tx_id);
    REQUIRE(!d.pool.has(early.tx_id));
    // A bond still ahead of the moment stays, and a payment has no start to be
    // past.
    REQUIRE(d.pool.has(later.tx_id));
    REQUIRE(d.pool.has(payment.tx_id));
}
