// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state_test.cpp — the validator set and the layer a block is verified on.
//
// Ported from Go vms/platformvm/state: staker_test.go (TestStakerLess),
// stakers_test.go (base and diff staker sets, pruning), and
// staker_diff_iterator_test.go (the mutable walk and the diff walk). Each case
// asserts the same behaviour, in the same order, as the Go original — the
// ordering here IS consensus, so "some order" is not an answer.

#include "harness.hpp"
#include "lux/platformvm/state.hpp"

using namespace lux::platformvm;
using namespace lux::platformvm::state;

namespace {

// Ids the reference generates randomly. A test that must be reproducible names
// them instead, and the properties under test (ordering, presence, pruning) do
// not depend on the values being random.
Id id_of(std::uint8_t b) {
    Id v{};
    v[0] = b;
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    v.b[0] = b;
    return v;
}

std::uint8_t g_seq = 0;

// Go: newTestStaker.
Staker new_test_staker() {
    ++g_seq;
    Staker s;
    s.tx_id = id_of(g_seq);
    s.node_id = node_of(g_seq);
    s.chain_id = id_of(static_cast<std::uint8_t>(0x80 + g_seq));
    s.weight = 1;
    s.start_time = 1000;
    s.end_time = 1000 + 14 * 24 * 60 * 60;
    s.potential_reward = 1;
    s.next_time = s.end_time;
    s.priority = Priority::PrimaryNetworkDelegatorCurrent;
    return s;
}

std::vector<Id> tx_ids(const StakerList& l) {
    std::vector<Id> out;
    for (const auto& s : l) out.push_back(s.tx_id);
    return out;
}

}  // namespace

// Go: TestStakerLess — the seven cases, verbatim.
TEST(StakerLess) {
    auto mk = [](std::uint8_t tx, std::uint64_t next, Priority p) {
        Staker s;
        s.tx_id = id_of(tx);
        s.next_time = next;
        s.priority = p;
        return s;
    };
    // left time < right time
    REQUIRE(mk(0, 0, Priority::PrimaryNetworkValidatorCurrent)
                .less(mk(0, 1, Priority::PrimaryNetworkValidatorCurrent)));
    // left time > right time
    REQUIRE(!mk(0, 1, Priority::PrimaryNetworkValidatorCurrent)
                 .less(mk(0, 0, Priority::PrimaryNetworkValidatorCurrent)));
    // left priority < right priority
    REQUIRE(mk(0, 0, Priority::PrimaryNetworkDelegatorLegacyPending)
                .less(mk(0, 0, Priority::PrimaryNetworkValidatorPending)));
    // left priority > right priority
    REQUIRE(!mk(0, 0, Priority::PrimaryNetworkValidatorPending)
                 .less(mk(0, 0, Priority::PrimaryNetworkDelegatorLegacyPending)));
    // left txID < right txID
    REQUIRE(mk(0, 0, Priority::PrimaryNetworkValidatorPending)
                .less(mk(1, 0, Priority::PrimaryNetworkValidatorPending)));
    // left txID > right txID
    REQUIRE(!mk(1, 0, Priority::PrimaryNetworkValidatorPending)
                 .less(mk(0, 0, Priority::PrimaryNetworkValidatorPending)));
    // equal
    REQUIRE(!mk(0, 0, Priority::PrimaryNetworkValidatorCurrent)
                 .less(mk(0, 0, Priority::PrimaryNetworkValidatorCurrent)));
}

// Go: TestBaseStakersPruning — a node with neither a validator nor a delegator
// left must leave no trace, or the set's emptiness is a lie.
TEST(BaseStakersPruning) {
    Staker staker = new_test_staker();
    Staker delegator = new_test_staker();
    delegator.chain_id = staker.chain_id;
    delegator.node_id = staker.node_id;

    BaseStakers v;
    v.put_validator(staker);
    REQUIRE_OK(v.get_validator(staker.chain_id, staker.node_id));

    v.put_delegator(delegator);
    REQUIRE_OK(v.get_validator(staker.chain_id, staker.node_id));

    v.delete_validator(staker);
    REQUIRE_ERR(v.get_validator(staker.chain_id, staker.node_id), Err::NotFound);

    v.delete_delegator(delegator);
    REQUIRE(v.empty());

    v.put_validator(staker);
    REQUIRE_OK(v.get_validator(staker.chain_id, staker.node_id));

    v.put_delegator(delegator);
    REQUIRE_OK(v.get_validator(staker.chain_id, staker.node_id));

    v.delete_delegator(delegator);
    REQUIRE_OK(v.get_validator(staker.chain_id, staker.node_id));

    v.delete_validator(staker);
    REQUIRE_ERR(v.get_validator(staker.chain_id, staker.node_id), Err::NotFound);
    REQUIRE(v.empty());
}

// Go: TestBaseStakersValidator.
TEST(BaseStakersValidator) {
    Staker staker = new_test_staker();
    Staker delegator = new_test_staker();

    BaseStakers v;
    v.put_delegator(delegator);

    REQUIRE_ERR(v.get_validator(id_of(0xF1), delegator.node_id), Err::NotFound);
    REQUIRE_ERR(v.get_validator(delegator.chain_id, node_of(0xF2)), Err::NotFound);
    // A delegator does not make its node a validator.
    REQUIRE_ERR(v.get_validator(delegator.chain_id, delegator.node_id), Err::NotFound);

    REQUIRE_EQ(std::vector<Id>({delegator.tx_id}), tx_ids(v.staker_list()));

    v.put_validator(staker);
    auto got = v.get_validator(staker.chain_id, staker.node_id);
    REQUIRE_OK(got);
    REQUIRE_EQ(staker, got.value());

    v.delete_delegator(delegator);
    REQUIRE_EQ(std::vector<Id>({staker.tx_id}), tx_ids(v.staker_list()));

    v.delete_validator(staker);
    REQUIRE_ERR(v.get_validator(staker.chain_id, staker.node_id), Err::NotFound);
    REQUIRE(v.staker_list().empty());
}

// Go: TestBaseStakersDelegator.
TEST(BaseStakersDelegator) {
    Staker staker = new_test_staker();
    Staker delegator = new_test_staker();

    BaseStakers v;
    REQUIRE(v.delegator_list(delegator.chain_id, delegator.node_id).empty());

    v.put_delegator(delegator);
    REQUIRE(v.delegator_list(delegator.chain_id, node_of(0xF3)).empty());
    REQUIRE_EQ(std::vector<Id>({delegator.tx_id}),
               tx_ids(v.delegator_list(delegator.chain_id, delegator.node_id)));

    v.delete_delegator(delegator);
    REQUIRE(v.delegator_list(delegator.chain_id, delegator.node_id).empty());

    v.put_validator(staker);
    v.put_delegator(delegator);
    v.delete_delegator(delegator);
    REQUIRE(v.delegator_list(staker.chain_id, staker.node_id).empty());
}

// Go: TestDiffStakersValidator — a delegator change never moves the VALIDATOR
// status, and a validator added and removed in one layer is as if it never was.
TEST(DiffStakersValidator) {
    Staker staker = new_test_staker();
    Staker delegator = new_test_staker();

    DiffStakers v;
    v.put_delegator(delegator);

    REQUIRE(v.get_validator(id_of(0xF4), delegator.node_id).second == DiffStatus::Unmodified);
    REQUIRE(v.get_validator(delegator.chain_id, node_of(0xF5)).second == DiffStatus::Unmodified);
    REQUIRE(v.get_validator(delegator.chain_id, delegator.node_id).second == DiffStatus::Unmodified);

    REQUIRE_EQ(std::vector<Id>({delegator.tx_id}), tx_ids(v.staker_list({})));

    REQUIRE_OK(v.put_validator(staker));
    auto got = v.get_validator(staker.chain_id, staker.node_id);
    REQUIRE(got.second == DiffStatus::Added);
    REQUIRE(got.first.has_value());
    REQUIRE_EQ(staker, *got.first);

    v.delete_validator(staker);
    REQUIRE(v.get_validator(staker.chain_id, staker.node_id).second == DiffStatus::Unmodified);
    REQUIRE_EQ(std::vector<Id>({delegator.tx_id}), tx_ids(v.staker_list({})));
}

// Go: TestDiffStakersDeleteValidator.
TEST(DiffStakersDeleteValidator) {
    Staker staker = new_test_staker();
    Staker delegator = new_test_staker();

    DiffStakers v;
    REQUIRE(v.get_validator(id_of(0xF6), delegator.node_id).second == DiffStatus::Unmodified);

    v.delete_validator(staker);
    auto got = v.get_validator(staker.chain_id, staker.node_id);
    REQUIRE(got.second == DiffStatus::Deleted);
    REQUIRE(!got.first.has_value());
}

// Go: TestDiffStakersDelegator.
TEST(DiffStakersDelegator) {
    Staker staker = new_test_staker();
    Staker delegator = new_test_staker();

    DiffStakers v;
    REQUIRE_OK(v.put_validator(staker));
    REQUIRE(v.delegator_list({}, id_of(0xF7), delegator.node_id).empty());

    v.put_delegator(delegator);
    REQUIRE_EQ(std::vector<Id>({delegator.tx_id}),
               tx_ids(v.delegator_list({}, delegator.chain_id, delegator.node_id)));

    v.delete_delegator(delegator);
    REQUIRE(v.delegator_list({}, id_of(0xF8), delegator.node_id).empty());
}

// Go: state.ErrAddingStakerAfterDeletion — a layer that says a validator both
// left and arrived says nothing, so the second claim is refused.
TEST(DiffStakersRefusesAddAfterDelete) {
    Staker staker = new_test_staker();
    DiffStakers v;
    v.delete_validator(staker);
    REQUIRE_ERR(v.put_validator(staker), Err::AddingStakerAfterDeletion);
}

// Go: TestMutableStakerIterator — elements pushed mid-walk still come out in
// order.
TEST(MutableStakerWalkOrder) {
    auto mk = [](std::uint8_t tx, std::uint64_t t) {
        Staker s;
        s.tx_id = id_of(tx);
        s.end_time = t;
        s.next_time = t;
        s.priority = Priority::PrimaryNetworkValidatorCurrent;
        return s;
    };
    const StakerList initial = {mk(1, 10), mk(2, 20), mk(3, 30)};
    MutableStakerWalk it(initial);

    // Next must be called before Add.
    REQUIRE(it.next());
    for (const auto& s : {mk(4, 5), mk(5, 15), mk(6, 25)}) it.add(s);

    const std::vector<Id> want = {id_of(4), id_of(1), id_of(5), id_of(2), id_of(6), id_of(3)};
    bool has_next = true;
    for (const auto& expected : want) {
        REQUIRE(has_next);
        REQUIRE_EQ(expected, it.value().tx_id);
        has_next = it.next();
    }
    REQUIRE(!has_next);
    it.release();
    REQUIRE(!it.next());
}

// Go: TestStakerDiffIterator — the exact nine-event sequence.
TEST(StakerDiffWalkSequence) {
    Staker current;
    current.tx_id = id_of(1);
    current.end_time = 10;
    current.next_time = 10;
    current.priority = Priority::PrimaryNetworkValidatorCurrent;

    auto pending = [](std::uint8_t tx, std::uint64_t start, std::uint64_t end, Priority p) {
        Staker s;
        s.tx_id = id_of(tx);
        s.start_time = start;
        s.end_time = end;
        s.next_time = start;
        s.priority = p;
        return s;
    };
    const StakerList pendings = {
        pending(2, 0, 5, Priority::PrimaryNetworkDelegatorLegacyPending),
        pending(3, 5, 10, Priority::PrimaryNetworkDelegatorLegacyPending),
        pending(4, 11, 20, Priority::PrimaryNetworkValidatorPending),
        pending(5, 11, 20, Priority::PrimaryNetworkDelegatorLegacyPending),
    };

    struct Event {
        std::uint8_t tx;
        bool added;
    };
    const std::vector<Event> want = {
        {2, true}, {3, true}, {2, false}, {3, false}, {1, false},
        {4, true}, {5, true}, {5, false}, {4, false},
    };

    StakerDiffWalk it({current}, pendings);
    for (const auto& e : want) {
        REQUIRE(it.next());
        REQUIRE_EQ(id_of(e.tx), it.value().tx_id);
        REQUIRE_EQ_NUM(e.added ? 1 : 0, it.is_added() ? 1 : 0);
    }
    REQUIRE(!it.next());
    it.release();
    REQUIRE(!it.next());
}

// Go: ValidatorWeightDiff.addOrSub — a signed quantity as (sign, magnitude).
TEST(WeightDiffArithmetic) {
    WeightDiff d;
    REQUIRE_OK(d.add(10));
    REQUIRE_U64(10u, d.amount);
    REQUIRE(!d.decrease);

    REQUIRE_OK(d.sub(3));
    REQUIRE_U64(7u, d.amount);
    REQUIRE(!d.decrease);

    // Cancelling exactly flips the sign and leaves zero — the reference's
    // behaviour, and what a comparison of two diffs sees.
    REQUIRE_OK(d.sub(7));
    REQUIRE_U64(0u, d.amount);
    REQUIRE(d.decrease);

    REQUIRE_OK(d.sub(5));
    REQUIRE_U64(5u, d.amount);
    REQUIRE(d.decrease);

    REQUIRE_OK(d.add(9));
    REQUIRE_U64(4u, d.amount);
    REQUIRE(!d.decrease);

    WeightDiff big;
    REQUIRE_OK(big.add(UINT64_MAX));
    REQUIRE_ERR(big.add(1), Err::Overflow);
}

// Go: diffValidator.WeightDiff — one node's net weight change over a layer.
TEST(ValidatorDiffWeight) {
    Staker v = new_test_staker();
    v.weight = 100;
    Staker d1 = new_test_staker();
    d1.chain_id = v.chain_id;
    d1.node_id = v.node_id;
    d1.weight = 7;
    Staker d2 = new_test_staker();
    d2.chain_id = v.chain_id;
    d2.node_id = v.node_id;
    d2.weight = 3;

    ValidatorDiff vd;
    vd.status = DiffStatus::Added;
    vd.validator = v;
    vd.added_delegators.insert(d1);
    vd.deleted_delegators[d2.tx_id] = d2;

    auto w = vd.weight_diff();
    REQUIRE_OK(w);
    REQUIRE(!w.value().decrease);
    REQUIRE_U64(104u, w.value().amount);  // +100 -3 +7

    ValidatorDiff removed;
    removed.status = DiffStatus::Deleted;
    removed.validator = v;
    auto w2 = removed.weight_diff();
    REQUIRE_OK(w2);
    REQUIRE(w2.value().decrease);
    REQUIRE_U64(100u, w2.value().amount);
}

// The layered read: a diff answers from itself, then from its parent.
TEST(DiffReadsThroughToParent) {
    MemState base;
    base.set_timestamp(100);
    base.set_current_supply(kPrimaryNetworkId, 1000);

    Staker s = new_test_staker();
    s.chain_id = kPrimaryNetworkId;
    REQUIRE_OK(base.put_current_validator(s));

    Diff d(&base);
    REQUIRE_U64(100u, d.timestamp());
    auto supply = d.current_supply(kPrimaryNetworkId);
    REQUIRE_OK(supply);
    REQUIRE_U64(1000u, supply.value());
    REQUIRE_OK(d.get_current_validator(kPrimaryNetworkId, s.node_id));

    // The layer's own answer wins.
    d.set_timestamp(200);
    d.set_current_supply(kPrimaryNetworkId, 2000);
    REQUIRE_U64(100u, base.timestamp());
    REQUIRE_U64(200u, d.timestamp());
    REQUIRE_U64(2000u, d.current_supply(kPrimaryNetworkId).value());
    REQUIRE_U64(1000u, base.current_supply(kPrimaryNetworkId).value());

    // A validator deleted in the layer is gone from the layer and still in the
    // parent — that is what makes a rejected block cost nothing.
    d.delete_current_validator(s);
    REQUIRE_ERR(d.get_current_validator(kPrimaryNetworkId, s.node_id), Err::NotFound);
    REQUIRE_OK(base.get_current_validator(kPrimaryNetworkId, s.node_id));

    REQUIRE_OK(d.apply(base));
    REQUIRE_U64(200u, base.timestamp());
    REQUIRE_U64(2000u, base.current_supply(kPrimaryNetworkId).value());
    REQUIRE_ERR(base.get_current_validator(kPrimaryNetworkId, s.node_id), Err::NotFound);
}

// UTXOs: added, hidden, and deleted through a layer.
TEST(DiffUTXOs) {
    MemState base;
    UTXO u;
    u.utxo = UtxoId{id_of(9), 0};
    u.asset = id_of(0x10);
    u.out = TransferOutput{500, OutputOwners{0, 1, {}}};
    base.add_utxo(u);

    Diff d(&base);
    REQUIRE_OK(d.get_utxo(u.id()));
    d.delete_utxo(u.id());
    REQUIRE_ERR(d.get_utxo(u.id()), Err::NotFound);
    REQUIRE_OK(base.get_utxo(u.id()));

    UTXO n;
    n.utxo = UtxoId{id_of(10), 1};
    n.asset = id_of(0x10);
    n.out = TransferOutput{7, OutputOwners{0, 1, {}}};
    d.add_utxo(n);
    REQUIRE_OK(d.get_utxo(n.id()));
    REQUIRE_ERR(base.get_utxo(n.id()), Err::NotFound);

    REQUIRE_OK(d.apply(base));
    REQUIRE_ERR(base.get_utxo(u.id()), Err::NotFound);
    REQUIRE_OK(base.get_utxo(n.id()));
}

// Go: metadata.GetDelegateeReward — a validator that is not in the set has no
// ledger, and asking is a refusal rather than a zero.
TEST(DelegateeRewardLedgerFollowsTheValidator) {
    MemState base;
    Staker s = new_test_staker();
    s.chain_id = kPrimaryNetworkId;

    REQUIRE_ERR(base.delegatee_reward(kPrimaryNetworkId, s.node_id), Err::NotFound);
    REQUIRE_ERR(base.set_delegatee_reward(kPrimaryNetworkId, s.node_id, 100), Err::NotFound);

    REQUIRE_OK(base.put_current_validator(s));
    auto r = base.delegatee_reward(kPrimaryNetworkId, s.node_id);
    REQUIRE_OK(r);
    REQUIRE_U64(0u, r.value());

    REQUIRE_OK(base.set_delegatee_reward(kPrimaryNetworkId, s.node_id, 100));
    REQUIRE_U64(100u, base.delegatee_reward(kPrimaryNetworkId, s.node_id).value());

    base.delete_current_validator(s);
    REQUIRE_ERR(base.delegatee_reward(kPrimaryNetworkId, s.node_id), Err::NotFound);
}

// The next moment the set changes bounds how far a block may move the clock.
TEST(NextStakerChangeTime) {
    MemState base;
    base.set_timestamp(0);
    REQUIRE_U64(9999u, next_staker_change_time(base, 9999));

    Staker cur = new_test_staker();
    cur.chain_id = kPrimaryNetworkId;
    cur.end_time = 500;
    cur.next_time = 500;
    cur.priority = Priority::PrimaryNetworkValidatorCurrent;
    REQUIRE_OK(base.put_current_validator(cur));
    REQUIRE_U64(500u, next_staker_change_time(base, 9999));

    Staker pend = new_test_staker();
    pend.chain_id = kPrimaryNetworkId;
    pend.start_time = 300;
    pend.next_time = 300;
    pend.priority = Priority::PrimaryNetworkValidatorPending;
    REQUIRE_OK(base.put_pending_validator(pend));
    REQUIRE_U64(300u, next_staker_change_time(base, 9999));

    // The bound still wins when it is the earliest.
    REQUIRE_U64(100u, next_staker_change_time(base, 100));
}
