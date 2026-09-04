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
