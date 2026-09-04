// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// validators_test.cpp — the set consensus samples, and its commitment.
//
// Ported from Go vms/platformvm/validators/manager_test.go and
// state/inherited_key_test.go. The commitment is checked against the NODE's own
// implementation of it, not against this one's: two implementations of one hash
// that only agree with themselves is exactly the failure a set commitment
// exists to prevent.

#include "harness.hpp"
#include "lux/platformvm/validators.hpp"
#include "signing.hpp"

#include <functional>

#ifdef LUX_PLATFORMVM_HAS_NODE_SET_ROOT
#include "lux/node/validators.hpp"
#endif

using namespace lux::platformvm;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

const signer::ProofOfPossession& pop(std::uint8_t seed) {
    static std::map<std::uint8_t, pvmtest::BlsKey> keys;
    auto it = keys.find(seed);
    if (it == keys.end()) it = keys.emplace(seed, pvmtest::BlsKey(seed)).first;
    return it->second.pop();
}

state::Staker validator_of(std::uint8_t n, const Id& chain, std::uint64_t weight,
                           std::optional<signer::PublicKeyBytes> key,
                           txs::Priority p = txs::Priority::PrimaryNetworkValidatorCurrent) {
    state::Staker s;
    s.tx_id = id_of(n);
    s.node_id = node_of(n);
    s.chain_id = chain;
    s.weight = weight;
    s.public_key = std::move(key);
    s.end_time = 1'000'000;
    s.next_time = s.end_time;
    s.priority = p;
    return s;
}

}  // namespace

// Only validators carry weight in the set a vote is sampled from; a delegator's
// stake sits under its validator rather than being an entry of its own.
TEST(CurrentSetHoldsValidatorsOnly) {
    state::MemState s;
    REQUIRE_OK(s.put_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key)));
    REQUIRE_OK(s.put_current_validator(validator_of(2, kPrimaryNetworkId, 200, pop(2).public_key)));

    state::Staker d = validator_of(3, kPrimaryNetworkId, 50, std::nullopt,
                                   txs::Priority::PrimaryNetworkDelegatorCurrent);
    d.node_id = node_of(1);
    s.put_current_delegator(d);

    auto set = validators::current_set(s, kPrimaryNetworkId);
    REQUIRE_OK(set);
    REQUIRE_EQ_NUM(2, set.value().size());
    REQUIRE_U64(100u, set.value().at(node_of(1)).weight);
    REQUIRE_U64(200u, set.value().at(node_of(2)).weight);
    REQUIRE_EQ(id_of(1), set.value().at(node_of(1)).tx_id);
}

// The key is UNCOMPRESSED in the set: a proof of possession signs the 48-byte
// compressed form, and the commitment hashes the 96-byte one.
TEST(TheSetCarriesTheUncompressedKey) {
    state::MemState s;
    REQUIRE_OK(s.put_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key)));

    auto set = validators::current_set(s, kPrimaryNetworkId);
    REQUIRE_OK(set);
    const auto& key = set.value().at(node_of(1)).public_key;
    REQUIRE(key.has_value());
    REQUIRE_EQ_NUM(96, key->size());
    REQUIRE(!(*key == std::vector<std::uint8_t>(pop(1).public_key.begin(), pop(1).public_key.end())));

    // And it decompresses back to the same point: the first 48 bytes of the
    // uncompressed form are the x coordinate the compressed form carries.
    auto again = validators::uncompress_public_key(pop(1).public_key);
    REQUIRE_OK(again);
    REQUIRE(again.value() == *key);
}

// Go: state.getInheritedPublicKey. A validator of a network of its own holds no
// key — its transaction carries no signer — and signs with the key of its
// primary-network entry. Surfacing it keyless would leave its weight in the
// denominator with no way for anyone to vote toward it.
TEST(AChainValidatorInheritsItsPrimaryKey) {
    const Id net = id_of(0x50);
    state::MemState s;
    REQUIRE_OK(s.put_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key)));
    REQUIRE_OK(s.put_current_validator(validator_of(1, net, 7, std::nullopt,
                                                    txs::Priority::ChainPermissionedValidatorCurrent)));

    auto set = validators::current_set(s, net);
    REQUIRE_OK(set);
    REQUIRE_EQ_NUM(1, set.value().size());
    const auto& v = set.value().at(node_of(1));
    REQUIRE_U64(7u, v.weight);  // its weight on THIS network, not the primary one
    REQUIRE(v.public_key.has_value());

    auto expected = validators::uncompress_public_key(pop(1).public_key);
    REQUIRE_OK(expected);
    REQUIRE(*v.public_key == expected.value());

    // A validator of a network with no primary-network entry to inherit from is
    // a refusal, not a keyless entry.
    state::MemState orphan;
    REQUIRE_OK(orphan.put_current_validator(validator_of(9, net, 7, std::nullopt,
                                                          txs::Priority::ChainPermissionedValidatorCurrent)));
    REQUIRE_ERR(validators::current_set(orphan, net), Err::NotValidator);
}

// An empty set is "unbound", not an error, and commits to the zero id.
TEST(AnEmptySetCommitsToZero) {
    REQUIRE_EQ(kEmptyId, validators::set_root({}));
}

// The commitment is the NODE's. This is the case that would catch a port that
// hashed the compressed key, or the declared weight, or the wrong field order.
TEST(TheSetRootIsTheNodes) {
#ifndef LUX_PLATFORMVM_HAS_NODE_SET_ROOT
    SKIP("the node checkout's own validator_set_root was not found to check against");
#else
    state::MemState s;
    REQUIRE_OK(s.put_current_validator(validator_of(3, kPrimaryNetworkId, 300, pop(3).public_key)));
    REQUIRE_OK(s.put_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key)));
    REQUIRE_OK(s.put_current_validator(validator_of(2, kPrimaryNetworkId, 200, pop(2).public_key)));

    auto set = validators::current_set(s, kPrimaryNetworkId);
    REQUIRE_OK(set);

    std::vector<lux::node::SetMember> members;
    for (const auto& [node_id, v] : set.value()) {
        lux::node::SetMember m;
        std::copy(node_id.b.begin(), node_id.b.end(), m.node_id.begin());
        m.weight = v.weight;
        if (v.public_key) m.pubkey = *v.public_key;
        members.push_back(std::move(m));
    }
    // Handed to the node OUT of order, because it sorts internally and a caller
    // must not be able to get the order wrong.
    std::reverse(members.begin(), members.end());

    const lux::node::Id want = lux::node::validator_set_root(members);
    const Id got = validators::set_root(set.value());
    REQUIRE(std::equal(want.begin(), want.end(), got.b.begin()));

    // And it is sensitive to what it commits to: one more unit of weight is a
    // different set.
    auto heavier = set.value();
    heavier.at(node_of(1)).weight += 1;
    REQUIRE(!(validators::set_root(heavier) == got));

    // Including the key, which is the field a port is most likely to get wrong.
    auto keyless = set.value();
    keyless.at(node_of(1)).public_key.reset();
    REQUIRE(!(validators::set_root(keyless) == got));
#endif
}

// ── the set at a height that has already passed

namespace {

// One accepted height: what the layer changed is recorded, then applied. The
// order is the chain's own — a change can only be described against the state
// it lands on.
Status settle(state::MemState& base, validators::History& h, std::uint64_t height,
              const std::function<void(state::Diff&)>& layer) {
    state::Diff d(&base);
    layer(d);
    auto c = validators::changes(d, base);
    if (!c) return std::unexpected(c.error());
    if (auto st = h.record(height, c.value()); !st) return st;
    return d.apply(base);
}

l1::Validator l1_of(const Id& validation, const Id& chain, std::uint8_t node, std::uint64_t weight,
                    const std::vector<std::uint8_t>& key) {
    l1::Validator v;
    v.validation_id = validation;
    v.chain_id = chain;
    v.node_id = node_of(node);
    v.public_key = key;
    v.weight = weight;
    v.end_accumulated_fee = 1;  // paid up: active, and therefore votable
    return v;
}

}  // namespace

// The set at a past height is the set now with everything since undone. Both
// directions of change have to be undone — a validator that left comes back,
// one that joined goes away — and each has to come back with what it had.
TEST(TheSetIsRebuiltAtAPastHeight) {
    state::MemState s;
    s.load_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key));
    s.load_current_validator(validator_of(2, kPrimaryNetworkId, 200, pop(2).public_key));
    validators::History h;

    const auto at_one = validators::current_set(s, kPrimaryNetworkId).value();

    // Height 2: one leaves, another joins.
    REQUIRE_OK(settle(s, h, 2, [](state::Diff& d) {
        d.delete_current_validator(validator_of(2, kPrimaryNetworkId, 200, pop(2).public_key));
        (void)d.put_current_validator(validator_of(3, kPrimaryNetworkId, 300, pop(3).public_key));
    }));
    const auto at_two = validators::current_set(s, kPrimaryNetworkId).value();
    REQUIRE_EQ_NUM(2, at_two.size());

    // Height 3: the first leaves too.
    REQUIRE_OK(settle(s, h, 3, [](state::Diff& d) {
        d.delete_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key));
    }));
    const auto at_three = validators::current_set(s, kPrimaryNetworkId).value();
    REQUIRE_EQ_NUM(1, at_three.size());

    auto back_to_two = at_three;
    REQUIRE_OK(h.rewind(back_to_two, kPrimaryNetworkId, 3, 2));
    REQUIRE(back_to_two == at_two);

    auto back_to_one = at_three;
    REQUIRE_OK(h.rewind(back_to_one, kPrimaryNetworkId, 3, 1));
    REQUIRE(back_to_one == at_one);
    // Including the key it signed with, which is what an old signature is
    // checked against.
    REQUIRE(back_to_one.at(node_of(2)).public_key.has_value());
    REQUIRE_EQ(id_of(2), back_to_one.at(node_of(2)).tx_id);

    // A walk of no distance changes nothing, and a walk forward is a refusal:
    // a height the chain has not reached has no set.
    auto unchanged = at_three;
    REQUIRE_OK(h.rewind(unchanged, kPrimaryNetworkId, 3, 3));
    REQUIRE(unchanged == at_three);
    REQUIRE_ERR(h.rewind(unchanged, kPrimaryNetworkId, 3, 4), Err::InvalidState);
}

// One network's changes are not another's. A set is rebuilt from the record of
// the network being asked about and nothing else.
TEST(APastHeightIsPerNetwork) {
    const Id net = id_of(0x40);
    state::MemState s;
    s.load_current_validator(validator_of(1, kPrimaryNetworkId, 100, pop(1).public_key));
    s.load_current_validator(validator_of(1, net, 100, std::nullopt,
                                          txs::Priority::ChainPermissionedValidatorCurrent));
    validators::History h;

    REQUIRE_OK(settle(s, h, 2, [&](state::Diff& d) {
        d.delete_current_validator(validator_of(1, net, 100, std::nullopt,
                                                txs::Priority::ChainPermissionedValidatorCurrent));
    }));

    // The primary network never changed, so undoing height 2 leaves it alone.
    auto primary = validators::current_set(s, kPrimaryNetworkId).value();
    const auto primary_now = primary;
    REQUIRE_OK(h.rewind(primary, kPrimaryNetworkId, 2, 1));
    REQUIRE(primary == primary_now);

    // The other network's validator comes back — with the key it inherits from
    // its primary-network entry, since it holds none of its own.
    auto other = validators::current_set(s, net).value();
    REQUIRE(other.empty());
    REQUIRE_OK(h.rewind(other, net, 2, 1));
    REQUIRE_EQ_NUM(1, other.size());
    REQUIRE_U64(100u, other.at(node_of(1)).weight);
    REQUIRE(other.at(node_of(1)).public_key.has_value());
}

// An L1 validator can be removed and registered again under a new name in one
// block, at the same weight. Nothing about the weight moved, so a record that
// only tracked weight would have nothing to say — and the entry would keep the
// name it has NOW when a past height is rebuilt, which is the wrong name for a
// message signed then.
TEST(AnL1ValidatorKeepsTheNameItHad) {
    const Id net = id_of(0x40);
    const auto key = validators::uncompress_public_key(pop(1).public_key).value();
    const Id first = id_of(0x50);
    const Id second = id_of(0x60);

    state::MemState s;
    REQUIRE_OK(s.put_l1_validator(l1_of(first, net, 0x11, 100, key)));
    validators::History h;

    const auto at_one = validators::current_set(s, net).value();
    REQUIRE_EQ_NUM(1, at_one.size());
    REQUIRE_EQ(first, at_one.at(node_of(0x11)).tx_id);
    REQUIRE_U64(100u, at_one.at(node_of(0x11)).weight);

    REQUIRE_OK(settle(s, h, 2, [&](state::Diff& d) {
        auto gone = l1_of(first, net, 0x11, 100, key);
        gone.weight = 0;  // removed
        REQUIRE_MSG(d.put_l1_validator(gone).has_value(), "the removal was refused");
        REQUIRE_MSG(d.put_l1_validator(l1_of(second, net, 0x11, 100, key)).has_value(),
                    "the re-registration was refused");
    }));

    const auto at_two = validators::current_set(s, net).value();
    REQUIRE_EQ_NUM(1, at_two.size());
    REQUIRE_EQ(second, at_two.at(node_of(0x11)).tx_id);

    auto back = at_two;
    REQUIRE_OK(h.rewind(back, net, 2, 1));
    REQUIRE(back == at_one);
    REQUIRE_EQ(first, back.at(node_of(0x11)).tx_id);
    REQUIRE_U64(100u, back.at(node_of(0x11)).weight);
}

// An L1 validator that was not there yet is not there when the height it joined
// at is undone.
TEST(AnL1ValidatorLeavesThePastAlone) {
    const Id net = id_of(0x40);
    const auto key = validators::uncompress_public_key(pop(2).public_key).value();
    state::MemState s;
    validators::History h;

    REQUIRE_OK(settle(s, h, 2, [&](state::Diff& d) {
        REQUIRE_MSG(d.put_l1_validator(l1_of(id_of(0x50), net, 0x11, 100, key)).has_value(),
                    "the registration was refused");
    }));
    const auto at_two = validators::current_set(s, net).value();
    REQUIRE_EQ_NUM(1, at_two.size());

    auto back = at_two;
    REQUIRE_OK(h.rewind(back, net, 2, 1));
    REQUIRE(back.empty());
}
