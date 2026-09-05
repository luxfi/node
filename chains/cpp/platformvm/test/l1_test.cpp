// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// l1_test.cpp — a validator of a sovereign network, and the fee it pays.
//
// Ported from Go vms/platformvm/state/l1_validator_test.go, state/expiry_test.go,
// vms/platformvm/validators/fee/fee_test.go, and the four L1 arms of
// txs/executor/standard_tx_executor_test.go.
//
// An L1 validator is not a staker. A staker bonds a stake and is PAID for the
// time it stood; an L1 validator PAYS for the P-chain's trouble in tracking it,
// so it leaves when its balance runs out rather than when a clock strikes.

#include "harness.hpp"
#include "lux/platformvm/executor.hpp"
#include "lux/platformvm/l1.hpp"
#include "signing.hpp"

using namespace lux::platformvm;
namespace ex = lux::platformvm::executor;

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
const Id kL1 = id_of(0x50);         // the sovereign network
const Id kManagerChain = id_of(0x51);  // where its manager contract lives
constexpr std::uint32_t kNetworkId = 96369;
constexpr std::uint64_t kChainTime = 1'000'000;

pvmtest::Key& key() {
    static pvmtest::Key k(1);
    return k;
}
pvmtest::BlsKey& validator_key() {
    static pvmtest::BlsKey k(21);
    return k;
}

OutputOwners mine() { return OutputOwners{0, 1, {key().address()}}; }
warpmsg::PChainOwner owner_of_mine() { return warpmsg::PChainOwner{1, {key().address()}}; }

const ex::FlatFee& fees() {
    static ex::FlatFee f(1'000'000);
    return f;
}

ex::Backend backend() {
    ex::Backend b;
    b.runtime = Runtime{kNetworkId, kPChain, kLux};
    b.fees = &fees();
    b.bootstrapped = true;
    b.now = kChainTime;
    b.validator_fee_config.capacity = 2;  // room for two active validators
    b.validator_fee_config.target = 1;
    b.validator_fee_config.min_price = 1;
    b.validator_fee_config.excess_conversion_constant = 100;
    return b;
}

// A chain that has already converted, so it has an address that may speak for it.
state::MemState converted(std::uint64_t funds = 10'000'000'000) {
    state::MemState s;
    s.set_timestamp(kChainTime);
    s.set_current_supply(kPrimaryNetworkId, 0);
    s.add_network(kL1);
    s.set_network_owner(kL1, mine());
    const std::string addr = "the-manager";
    s.set_network_conversion(kL1, state::NetToL1Conversion{kManagerChain,
                                                           {addr.begin(), addr.end()}, id_of(0x52)});
    UTXO u;
    u.utxo = UtxoId{id_of(0xA0), 0};
    u.asset = kLux;
    u.out = TransferOutput{funds, mine()};
    s.add_utxo(u);
    return s;
}

BaseTx envelope(std::uint64_t funds, std::vector<TransferableOutput> outs) {
    BaseTx b;
    b.network_id = kNetworkId;
    b.blockchain_id = kPChain;
    b.outs = std::move(outs);
    TransferableInput in;
    in.utxo = UtxoId{id_of(0xA0), 0};
    in.asset = kLux;
    in.in = TransferInput{funds, {0}};
    b.ins = {in};
    return b;
}

TransferableOutput out_to_me(std::uint64_t amt) {
    return TransferableOutput{kLux, 0, TransferOutput{amt, mine()}};
}

txs::Tx sign(std::shared_ptr<txs::UnsignedTx> u, std::size_t creds = 1) {
    txs::Tx tx;
    tx.unsigned_tx = std::move(u);
    for (std::size_t i = 0; i < creds; ++i) tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
    (void)tx.initialize();
    return tx;
}

// A warp message from the L1's manager, wrapped the way a transaction carries
// it: the L1's own message, inside an addressed call, inside a signed envelope.
// The SIGNATURE is checked separately (verify_warp_messages); the executor's job
// is the source, the expiry and the replay.
std::vector<std::uint8_t> from_the_manager(std::span<const std::uint8_t> l1_message,
                                           const Id& source_chain = kManagerChain,
                                           const char* address = "the-manager") {
    const std::string addr = address;
    auto call = warpmsg::AddressedCall::build(
        std::vector<std::uint8_t>(addr.begin(), addr.end()),
        std::vector<std::uint8_t>(l1_message.begin(), l1_message.end()));
    auto unsigned_message = warp::UnsignedMessage::build(kNetworkId, source_chain, call.value().bytes);
    warp::BitSetSignature sig;
    sig.signers = {0x01};
    auto message = warp::Message::build(unsigned_message.value(), sig);
    return message.value().bytes;
}

warpmsg::RegisterL1Validator registration(std::uint64_t expiry = kChainTime + 3600,
                                          std::uint64_t weight = 100,
                                          std::uint8_t node = 0x90) {
    return warpmsg::RegisterL1Validator::build(kL1, node_of(node), validator_key().pop().public_key, expiry,
                                                owner_of_mine(), owner_of_mine(), weight)
        .value();
}

txs::Tx register_tx(const warpmsg::RegisterL1Validator& msg, std::uint64_t balance,
                    const Id& source_chain = kManagerChain, const char* address = "the-manager") {
    const std::uint64_t change = 10'000'000'000 - balance - 1'000'000;
    auto u = txs::RegisterL1ValidatorTx::create(envelope(10'000'000'000, {out_to_me(change)}), balance,
                                                 validator_key().pop().proof,
                                                 from_the_manager(msg.bytes, source_chain, address));
    return sign(u.value());
}

}  // namespace

// Go: L1Validator.Compare / IsActive / immutableFieldsAreUnmodified.
TEST(TheL1ValidatorValue) {
    l1::Validator a;
    a.validation_id = id_of(1);
    a.weight = 10;
    a.end_accumulated_fee = 100;
    l1::Validator b = a;
    b.validation_id = id_of(2);
    b.end_accumulated_fee = 50;

    // Ordered by when the money runs out, then by name.
    REQUIRE(b.less(a));
    l1::Validator c = a;
    c.validation_id = id_of(0);
    REQUIRE(c.less(a));

    // A weight of zero REMOVES; a balance of zero DEACTIVATES. Different things.
    REQUIRE(a.is_active());
    l1::Validator inactive = a;
    inactive.end_accumulated_fee = 0;
    REQUIRE(!inactive.is_active());
    REQUIRE(!inactive.is_deleted());
    l1::Validator removed = a;
    removed.weight = 0;
    REQUIRE(removed.is_deleted());

    // An inactive validator has no node and no key in the set it is in: it holds
    // weight but cannot be sampled, so surfacing its node would make a quorum
    // wait for a vote that can never come.
    a.node_id = node_of(7);
    a.public_key = std::vector<std::uint8_t>(96, 0xAB);
    REQUIRE_EQ(node_of(7), a.effective_node_id());
    inactive = a;
    inactive.end_accumulated_fee = 0;
    REQUIRE_EQ(NodeId(), inactive.effective_node_id());
    REQUIRE(inactive.effective_public_key().empty());

    // Weight, nonce and balance may move; nothing else may.
    l1::Validator moved = a;
    moved.weight = 11;
    moved.min_nonce = 5;
    moved.end_accumulated_fee = 200;
    REQUIRE(a.immutable_fields_unmodified(moved));
    l1::Validator renamed = a;
    renamed.node_id = node_of(8);
    REQUIRE(!a.immutable_fields_unmodified(renamed));
    // A different validation id is a different validator, so nothing is fixed.
    renamed.validation_id = id_of(9);
    REQUIRE(a.immutable_fields_unmodified(renamed));
}

// Go: state.ExpiryEntry — big-endian, so the byte order and the time order are
// the same order.
TEST(TheExpiryKey) {
    const l1::ExpiryEntry e{0x0102030405060708ull, id_of(3)};
    const auto b = e.marshal();
    REQUIRE_EQ_NUM(40, b.size());
    REQUIRE_EQ_NUM(0x01, b[0]);
    REQUIRE_EQ_NUM(0x08, b[7]);

    auto back = l1::ExpiryEntry::unmarshal(b);
    REQUIRE_OK(back);
    REQUIRE(back.value() == e);
    REQUIRE_ERR(l1::ExpiryEntry::unmarshal(std::vector<std::uint8_t>(39, 0)), Err::BufferTooSmall);

    // Sorting the bytes sorts the times.
    const l1::ExpiryEntry earlier{1, id_of(9)};
    const l1::ExpiryEntry later{2, id_of(0)};
    REQUIRE(earlier.less(later));
    REQUIRE(earlier.marshal() < later.marshal());
}

// Go: state.PutL1Validator — the three invariants that keep a validation id
// naming one validator.
TEST(TheL1ValidatorSet) {
    state::MemState s;
    l1::Validator v;
    v.validation_id = id_of(1);
    v.chain_id = kL1;
    v.node_id = node_of(0x90);
    v.weight = 100;
    v.end_accumulated_fee = 500;
    REQUIRE_OK(s.put_l1_validator(v));

    REQUIRE_OK(s.get_l1_validator(id_of(1)));
    REQUIRE(s.has_l1_validator(kL1, node_of(0x90)));
    REQUIRE(!s.has_l1_validator(kL1, node_of(0x91)));
    REQUIRE_EQ_NUM(1, s.num_active_l1_validators());
    REQUIRE_U64(100u, s.weight_of_l1_validators(kL1).value());

    // A constant field cannot move under a name that is already taken.
    l1::Validator renamed = v;
    renamed.node_id = node_of(0x91);
    REQUIRE_ERR(s.put_l1_validator(renamed), Err::MutatedL1Validator);

    // Two validators cannot share a (chain, node) pair: that is one node voting
    // twice.
    l1::Validator twin = v;
    twin.validation_id = id_of(2);
    REQUIRE_ERR(s.put_l1_validator(twin), Err::DuplicateL1Validator);

    // A second validator on the same chain is fine, and adds to its weight.
    l1::Validator other = v;
    other.validation_id = id_of(2);
    other.node_id = node_of(0x91);
    other.weight = 50;
    REQUIRE_OK(s.put_l1_validator(other));
    REQUIRE_U64(150u, s.weight_of_l1_validators(kL1).value());

    // The active walk is by when the money runs out.
    l1::Validator sooner = other;
    sooner.end_accumulated_fee = 100;
    REQUIRE_OK(s.put_l1_validator(sooner));
    const auto active = s.active_l1_validators();
    REQUIRE_EQ_NUM(2, active.size());
    REQUIRE_EQ(id_of(2), active[0].validation_id);

    // An inactive validator still weighs on the set it is in.
    l1::Validator off = v;
    off.end_accumulated_fee = 0;
    REQUIRE_OK(s.put_l1_validator(off));
    REQUIRE_EQ_NUM(1, s.num_active_l1_validators());
    REQUIRE_U64(150u, s.weight_of_l1_validators(kL1).value());

    // A weight of zero removes it entirely.
    l1::Validator gone = v;
    gone.weight = 0;
    REQUIRE_OK(s.put_l1_validator(gone));
    REQUIRE_ERR(s.get_l1_validator(id_of(1)), Err::NotFound);
    REQUIRE_U64(50u, s.weight_of_l1_validators(kL1).value());
}

// The layer's L1 view, and that applying it lands the same set.
TEST(L1ValidatorsThroughALayer) {
    state::MemState base;
    l1::Validator v;
    v.validation_id = id_of(1);
    v.chain_id = kL1;
    v.node_id = node_of(0x90);
    v.weight = 100;
    v.end_accumulated_fee = 500;
    REQUIRE_OK(base.put_l1_validator(v));

    state::Diff d(&base);
    REQUIRE_OK(d.get_l1_validator(id_of(1)));

    l1::Validator added = v;
    added.validation_id = id_of(2);
    added.node_id = node_of(0x91);
    added.weight = 50;
    added.end_accumulated_fee = 200;
    REQUIRE_OK(d.put_l1_validator(added));
    REQUIRE_EQ_NUM(2, d.num_active_l1_validators());
    REQUIRE_EQ_NUM(1, base.num_active_l1_validators());
    REQUIRE_U64(150u, d.weight_of_l1_validators(kL1).value());

    // A removal in the layer does not fall through and resurrect the parent's.
    l1::Validator gone = v;
    gone.weight = 0;
    REQUIRE_OK(d.put_l1_validator(gone));
    REQUIRE_ERR(d.get_l1_validator(id_of(1)), Err::NotFound);
    REQUIRE(!d.has_l1_validator(kL1, node_of(0x90)));
    REQUIRE_OK(base.get_l1_validator(id_of(1)));

    const l1::ExpiryEntry e{kChainTime + 10, id_of(7)};
    d.put_expiry(e);
    REQUIRE(d.has_expiry(e));
    REQUIRE(!base.has_expiry(e));

    REQUIRE_OK(d.apply(base));
    REQUIRE_ERR(base.get_l1_validator(id_of(1)), Err::NotFound);
    REQUIRE_OK(base.get_l1_validator(id_of(2)));
    REQUIRE(base.has_expiry(e));
}

// Go: validators/fee — the continuous price.
TEST(TheContinuousFee) {
    l1::FeeConfig c;
    c.capacity = 20;
    c.target = 10;
    c.min_price = 100;
    c.excess_conversion_constant = 1000;

    // At target the price does not move, so the cost is linear.
    const l1::FeeState at_target{10, 0};
    REQUIRE_U64(100u * 60u, at_target.cost_of(c, 60));
    REQUIRE_U64(60u, at_target.seconds_remaining(c, 1000, 100 * 60));

    // Below target the excess drains toward zero and stays there.
    const l1::FeeState below{5, 100};
    const auto drained = below.advance_time(c.target, 20);
    REQUIRE_U64(0u, drained.excess);

    // Above target it climbs, and the price with it.
    const l1::FeeState above{20, 0};
    const auto climbed = above.advance_time(c.target, 10);
    REQUIRE_U64(100u, climbed.excess);
    REQUIRE(above.cost_of(c, 10) > 100u * 10u);

    // A floor price of zero buys forever rather than dividing by zero.
    l1::FeeConfig free_config = c;
    free_config.min_price = 0;
    REQUIRE_U64(1000u, l1::FeeState({20, 0}).seconds_remaining(free_config, 1000, 1));

    // The cost saturates rather than wrapping: an unpayable price is unpayable,
    // not free.
    l1::FeeConfig steep = c;
    steep.min_price = UINT64_MAX;
    REQUIRE_U64(UINT64_MAX, l1::FeeState({10, 0}).cost_of(steep, 2));
}

// The registration path, end to end: the message, the proof, the replay
// defence, and the record it writes.
TEST(RegisterL1Validator) {
    const auto b = backend();

    {  // the happy path
        auto s = converted();
        state::Diff layer(&s);
        const auto msg = registration();
        const auto tx = register_tx(msg, 5'000'000);
        REQUIRE_OK(ex::standard_tx(b, tx, layer));

        auto v = layer.get_l1_validator(msg.validation_id());
        REQUIRE_OK(v);
        REQUIRE_EQ(kL1, v.value().chain_id);
        REQUIRE_EQ(node_of(0x90), v.value().node_id);
        REQUIRE_U64(100u, v.value().weight);
        REQUIRE_U64(kChainTime, v.value().start_time);
        REQUIRE_U64(0u, v.value().min_nonce);
        // The balance is stored as the accrued mark it can pay up to.
        REQUIRE_U64(5'000'000u, v.value().end_accumulated_fee);
        REQUIRE(v.value().is_active());
        // The key in the record is the uncompressed one the set commits to.
        REQUIRE_EQ_NUM(96, v.value().public_key.size());

        // And the message can never be issued again.
        REQUIRE(layer.has_expiry(l1::ExpiryEntry{msg.expiry, msg.validation_id()}));
    }
    {  // a zero balance registers an INACTIVE validator: in the set, not charged
        auto s = converted();
        state::Diff layer(&s);
        const auto msg = registration();
        REQUIRE_OK(ex::standard_tx(b, register_tx(msg, 0), layer));
        auto v = layer.get_l1_validator(msg.validation_id());
        REQUIRE_OK(v);
        REQUIRE(!v.value().is_active());
        REQUIRE_EQ_NUM(0, layer.num_active_l1_validators());
    }
    {  // a message from a chain that is not this L1's manager
        auto s = converted();
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, register_tx(registration(), 5'000'000, id_of(0x99)), layer),
                    Err::WrongWarpSourceChain);
    }
    {  // a message from the right chain but the wrong address
        auto s = converted();
        state::Diff layer(&s);
        REQUIRE_ERR(
            ex::standard_tx(b, register_tx(registration(), 5'000'000, kManagerChain, "an-impostor"), layer),
            Err::WrongWarpSourceAddress);
    }
    {  // a network that never converted has nobody who may speak for it
        state::MemState s;
        s.set_timestamp(kChainTime);
        UTXO u;
        u.utxo = UtxoId{id_of(0xA0), 0};
        u.asset = kLux;
        u.out = TransferOutput{10'000'000'000, mine()};
        s.add_utxo(u);
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, register_tx(registration(), 5'000'000), layer),
                    Err::CouldNotLoadConversion);
    }
    {  // an expiry that has already passed
        auto s = converted();
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, register_tx(registration(kChainTime), 5'000'000), layer),
                    Err::WarpMessageExpired);
    }
    {  // an expiry further out than the window the chain will remember
        auto s = converted();
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, register_tx(registration(kChainTime + 24 * 60 * 60 + 1), 5'000'000),
                                    layer),
                    Err::WarpMessageNotYetAllowed);
    }
    {  // the same registration twice is a replay
        auto s = converted();
        state::Diff layer(&s);
        const auto msg = registration();
        REQUIRE_OK(ex::standard_tx(b, register_tx(msg, 5'000'000), layer));
        REQUIRE_OK(layer.apply(s));
        UTXO again;
        again.utxo = UtxoId{id_of(0xA0), 0};
        again.asset = kLux;
        again.out = TransferOutput{10'000'000'000, mine()};
        s.add_utxo(again);
        state::Diff second(&s);
        REQUIRE_ERR(ex::standard_tx(b, register_tx(msg, 5'000'000), second), Err::WarpMessageAlreadyIssued);
    }
    {  // the proof of possession must be for the key the message names
        auto s = converted();
        state::Diff layer(&s);
        pvmtest::BlsKey impostor(99);
        const auto msg = registration();
        auto u = txs::RegisterL1ValidatorTx::create(
            envelope(10'000'000'000, {out_to_me(9'000'000'000)}), 5'000'000, impostor.pop().proof,
            from_the_manager(msg.bytes));
        REQUIRE_ERR(ex::standard_tx(b, sign(u.value()), layer), Err::InvalidProofOfPossession);
    }
    {  // there is only so much room for ACTIVE validators
        auto s = converted();
        auto capped = backend();
        // Fill the capacity with two that are already there.
        for (std::uint8_t i = 0; i < 2; ++i) {
            l1::Validator v;
            v.validation_id = id_of(static_cast<std::uint8_t>(0xE0 + i));
            v.chain_id = kL1;
            v.node_id = node_of(static_cast<std::uint8_t>(0xE0 + i));
            v.weight = 1;
            v.end_accumulated_fee = 1;
            REQUIRE_OK(s.put_l1_validator(v));
        }
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(capped, register_tx(registration(), 5'000'000), layer),
                    Err::MaxNumActiveValidators);
        // But an inactive one still fits: it costs the chain nothing to hold.
        REQUIRE_OK(ex::standard_tx(capped, register_tx(registration(), 0), layer));
    }
}

// Weight changes, and the two things that stop them being abused: the nonce and
// the last-validator rule.
TEST(SetL1ValidatorWeight) {
    const auto b = backend();

    auto with_two = [&](state::MemState& s) {
        for (std::uint8_t i = 0; i < 2; ++i) {
            l1::Validator v;
            v.validation_id = id_of(static_cast<std::uint8_t>(0xE0 + i));
            v.chain_id = kL1;
            v.node_id = node_of(static_cast<std::uint8_t>(0xE0 + i));
            v.weight = 100;
            v.min_nonce = 3;
            v.end_accumulated_fee = 5'000'000;
            v.remaining_balance_owner = txs::marshal_owner(mine());
            v.deactivation_owner = txs::marshal_owner(mine());
            REQUIRE_MSG(s.put_l1_validator(v).has_value(), "seeding failed");
        }
    };

    auto weight_tx = [&](const Id& validation_id, std::uint64_t nonce, std::uint64_t weight) {
        auto msg = warpmsg::L1ValidatorWeight::build(validation_id, nonce, weight);
        auto u = txs::SetL1ValidatorWeightTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                                      from_the_manager(msg.value().bytes));
        return sign(u.value());
    };

    {  // a new weight, with a nonce at least as large as the one required
        auto s = converted();
        with_two(s);
        state::Diff layer(&s);
        REQUIRE_OK(ex::standard_tx(b, weight_tx(id_of(0xE0), 3, 250), layer));
        auto v = layer.get_l1_validator(id_of(0xE0));
        REQUIRE_OK(v);
        REQUIRE_U64(250u, v.value().weight);
        // The next change must use a bigger nonce, which is what stops this one
        // being replayed over it.
        REQUIRE_U64(4u, v.value().min_nonce);
    }
    {  // an old nonce is a replay
        auto s = converted();
        with_two(s);
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, weight_tx(id_of(0xE0), 2, 250), layer), Err::StaleNonce);
    }
    {  // the largest nonce is reserved for the change that removes a validator
        auto s = converted();
        with_two(s);
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, weight_tx(id_of(0xE0), UINT64_MAX, 250), layer),
                    Err::NonceReservedForRemoval);
        REQUIRE_OK(ex::standard_tx(b, weight_tx(id_of(0xE0), UINT64_MAX, 0), layer));
    }
    {  // removal refunds what the validator prepaid and did not spend
        auto s = converted();
        with_two(s);
        s.set_accrued_fees(1'000'000);
        state::Diff layer(&s);
        const auto tx = weight_tx(id_of(0xE0), 3, 0);
        REQUIRE_OK(ex::standard_tx(b, tx, layer));
        REQUIRE_ERR(layer.get_l1_validator(id_of(0xE0)), Err::NotFound);
        // 5'000'000 prepaid, 1'000'000 accrued: 4'000'000 comes back, as the
        // output after the transaction's own.
        auto refund = layer.get_utxo(UtxoId{tx.tx_id, 1}.input_id());
        REQUIRE_OK(refund);
        REQUIRE_U64(4'000'000u, refund.value().out.amt);
        REQUIRE_EQ(mine(), refund.value().out.owners);
    }
    {  // the LAST validator cannot be removed: a chain with none is a chain
       // nobody can ever speak for again
        auto s = converted();
        l1::Validator only;
        only.validation_id = id_of(0xE0);
        only.chain_id = kL1;
        only.node_id = node_of(0xE0);
        only.weight = 100;
        only.min_nonce = 0;
        only.end_accumulated_fee = 5'000'000;
        only.remaining_balance_owner = txs::marshal_owner(mine());
        only.deactivation_owner = txs::marshal_owner(mine());
        REQUIRE_OK(s.put_l1_validator(only));
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, weight_tx(id_of(0xE0), 0, 0), layer), Err::RemovingLastValidator);
    }
    {  // a message about a validator that is not there
        auto s = converted();
        with_two(s);
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, weight_tx(id_of(0xEE), 0, 5), layer), Err::CouldNotLoadL1Validator);
    }
}

// Topping up, and the activation it can cause.
TEST(IncreaseL1ValidatorBalance) {
    const auto b = backend();

    auto seed = [&](state::MemState& s, std::uint64_t end_fee) {
        l1::Validator v;
        v.validation_id = id_of(0xE0);
        v.chain_id = kL1;
        v.node_id = node_of(0xE0);
        v.weight = 100;
        v.end_accumulated_fee = end_fee;
        v.remaining_balance_owner = txs::marshal_owner(mine());
        v.deactivation_owner = txs::marshal_owner(mine());
        REQUIRE_MSG(s.put_l1_validator(v).has_value(), "seeding failed");
    };

    auto topup_tx = [&](const Id& validation_id, std::uint64_t amount) {
        auto u = txs::IncreaseL1ValidatorBalanceTx::create(
            envelope(10'000'000'000, {out_to_me(10'000'000'000 - amount - 1'000'000)}), validation_id,
            amount);
        return sign(u.value());
    };

    {  // an active validator's mark moves up by exactly what was paid
        auto s = converted();
        seed(s, 5'000'000);
        state::Diff layer(&s);
        REQUIRE_OK(ex::standard_tx(b, topup_tx(id_of(0xE0), 2'000'000), layer));
        REQUIRE_U64(7'000'000u, layer.get_l1_validator(id_of(0xE0)).value().end_accumulated_fee);
    }
    {  // topping up an INACTIVE validator switches it back on, from the mark
       // the chain has reached now rather than from where it left off
        auto s = converted();
        seed(s, 0);
        s.set_accrued_fees(3'000'000);
        state::Diff layer(&s);
        REQUIRE_OK(ex::standard_tx(b, topup_tx(id_of(0xE0), 2'000'000), layer));
        auto v = layer.get_l1_validator(id_of(0xE0));
        REQUIRE_OK(v);
        REQUIRE(v.value().is_active());
        REQUIRE_U64(5'000'000u, v.value().end_accumulated_fee);
    }
    {  // and it cannot switch on if there is no room
        auto s = converted();
        seed(s, 0);
        for (std::uint8_t i = 0; i < 2; ++i) {
            l1::Validator v;
            v.validation_id = id_of(static_cast<std::uint8_t>(0xF0 + i));
            v.chain_id = kL1;
            v.node_id = node_of(static_cast<std::uint8_t>(0xF0 + i));
            v.weight = 1;
            v.end_accumulated_fee = 1;
            REQUIRE_OK(s.put_l1_validator(v));
        }
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, topup_tx(id_of(0xE0), 2'000'000), layer),
                    Err::MaxNumActiveValidators);
    }
    {  // a validator that is not there
        auto s = converted();
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, topup_tx(id_of(0xEE), 2'000'000), layer),
                    Err::CouldNotLoadL1Validator);
    }
}

// Switching a validator off is the ONE L1 operation the P-chain authorises
// itself, against the owner the registration named.
TEST(DisableL1Validator) {
    const auto b = backend();

    auto seed = [&](state::MemState& s, const OutputOwners& deactivation) {
        l1::Validator v;
        v.validation_id = id_of(0xE0);
        v.chain_id = kL1;
        v.node_id = node_of(0xE0);
        v.weight = 100;
        v.end_accumulated_fee = 5'000'000;
        v.remaining_balance_owner = txs::marshal_owner(mine());
        v.deactivation_owner = txs::marshal_owner(deactivation);
        REQUIRE_MSG(s.put_l1_validator(v).has_value(), "seeding failed");
    };

    auto disable_tx = [&](const Id& validation_id) {
        auto u = txs::DisableL1ValidatorTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                                    validation_id, txs::Auth{0});
        // Two credentials: one for the spend, one for the authorisation.
        return sign(u.value(), 2);
    };

    {  // the owner may switch it off, and gets the unspent balance back
        auto s = converted();
        seed(s, mine());
        s.set_accrued_fees(1'000'000);
        state::Diff layer(&s);
        const auto tx = disable_tx(id_of(0xE0));
        REQUIRE_OK(ex::standard_tx(b, tx, layer));

        auto v = layer.get_l1_validator(id_of(0xE0));
        REQUIRE_OK(v);
        // Off, but still in the set: it holds weight and is not charged.
        REQUIRE(!v.value().is_active());
        REQUIRE(!v.value().is_deleted());
        REQUIRE_U64(100u, v.value().weight);

        auto refund = layer.get_utxo(UtxoId{tx.tx_id, 1}.input_id());
        REQUIRE_OK(refund);
        REQUIRE_U64(4'000'000u, refund.value().out.amt);
    }
    {  // anyone else may not
        auto s = converted();
        pvmtest::Key stranger(9);
        seed(s, OutputOwners{0, 1, {stranger.address()}});
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, disable_tx(id_of(0xE0)), layer), Err::NotAuthorized);
    }
    {  // switching off one that is already off changes nothing and refunds
       // nothing
        auto s = converted();
        seed(s, mine());
        l1::Validator off;
        REQUIRE_OK(s.get_l1_validator(id_of(0xE0)));
        off = s.get_l1_validator(id_of(0xE0)).value();
        off.end_accumulated_fee = 0;
        REQUIRE_OK(s.put_l1_validator(off));

        state::Diff layer(&s);
        const auto tx = disable_tx(id_of(0xE0));
        REQUIRE_OK(ex::standard_tx(b, tx, layer));
        REQUIRE_ERR(layer.get_utxo(UtxoId{tx.tx_id, 1}.input_id()), Err::NotFound);
    }
    {  // a validator that is not there
        auto s = converted();
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, disable_tx(id_of(0xEE)), layer), Err::CouldNotLoadL1Validator);
    }
}

// The signature on a warp-carrying transaction is checked separately, against
// the SOURCE chain's weight — a question about the P-chain's own state, so the
// caller resolves the set and hands it in.
TEST(WarpMessagesAreVerifiedAgainstTheSourceChain) {
    const auto b = backend();
    const auto msg = registration();
    const auto tx = register_tx(msg, 5'000'000);

    // An empty set: nothing has signed, and two-thirds of nothing is nothing, so
    // the weight check passes and the SIGNATURE is what refuses.
    warp::CanonicalValidatorSet empty;
    REQUIRE_ERR(ex::verify_warp_messages(*tx.unsigned_tx, kNetworkId, empty), Err::UnknownValidator);

    // A transaction that carries no message has nothing to answer about.
    auto plain = txs::BaseTxUnsigned::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}));
    REQUIRE_OK(plain);
    REQUIRE_OK(ex::verify_warp_messages(*plain.value(), kNetworkId, empty));
}

// The clock charges every active L1 validator, and switches off exactly the
// ones that can no longer pay. That is the whole lifecycle: an L1 validator
// leaves when its money runs out, not when a clock strikes.
TEST(TheClockChargesL1Validators) {
    auto b = backend();
    b.validator_fee_config.capacity = 10;
    b.validator_fee_config.target = 10;  // at target, so the price is the floor
    b.validator_fee_config.min_price = 1;
    b.validator_fee_config.excess_conversion_constant = 100;

    state::MemState s;
    s.set_timestamp(kChainTime);
    s.set_current_supply(kPrimaryNetworkId, 0);

    // Three validators, prepaid to 10, 100 and 1000.
    for (std::uint8_t i = 0; i < 3; ++i) {
        l1::Validator v;
        v.validation_id = id_of(static_cast<std::uint8_t>(0xE0 + i));
        v.chain_id = kL1;
        v.node_id = node_of(static_cast<std::uint8_t>(0xE0 + i));
        v.weight = 1;
        v.end_accumulated_fee = i == 0 ? 10 : (i == 1 ? 100 : 1000);
        REQUIRE_OK(s.put_l1_validator(v));
    }
    REQUIRE_EQ_NUM(3, s.num_active_l1_validators());

    // Fifty seconds at one per second: the first one's ten is gone, the other
    // two are still paying.
    auto changed = ex::advance_time_to(b, s, kChainTime + 50);
    REQUIRE_OK(changed);
    REQUIRE(changed.value());
    REQUIRE_U64(50u, s.accrued_fees());
    REQUIRE_EQ_NUM(2, s.num_active_l1_validators());
    REQUIRE(!s.get_l1_validator(id_of(0xE0)).value().is_active());
    REQUIRE(s.get_l1_validator(id_of(0xE1)).value().is_active());

    // A switched-off validator is still in the set and still weighs on it.
    REQUIRE_U64(3u, s.weight_of_l1_validators(kL1).value());

    // Another hundred seconds takes the second one too.
    changed = ex::advance_time_to(b, s, kChainTime + 150);
    REQUIRE_OK(changed);
    REQUIRE(changed.value());
    REQUIRE_EQ_NUM(1, s.num_active_l1_validators());
    REQUIRE(!s.get_l1_validator(id_of(0xE1)).value().is_active());
    REQUIRE(s.get_l1_validator(id_of(0xE2)).value().is_active());

    // Nothing to charge, nothing changes.
    changed = ex::advance_time_to(b, s, kChainTime + 151);
    REQUIRE_OK(changed);
    REQUIRE(!changed.value());
}

// ── the birth of a sovereign network
//
// Go: registerOwnSet, reached by both CreateNetworkTx (∅→Network) and
// ConvertNetworkTx (Network→Network). One primitive, so a set is established
// exactly one way whichever transaction establishes it.
namespace {

txs::NetworkValidator genesis_validator(std::uint8_t node, std::uint64_t weight, std::uint64_t balance,
                                        const signer::ProofOfPossession& pop) {
    txs::NetworkValidator v;
    const NodeId n = node_of(node);
    v.node_id.assign(n.b.begin(), n.b.end());
    v.weight = weight;
    v.balance = balance;
    v.pop = pop;
    v.remaining_balance_owner = txs::PChainOwner{1, {key().address()}};
    v.deactivation_owner = txs::PChainOwner{1, {key().address()}};
    return v;
}

}  // namespace

TEST(ANetworkIsBornSovereign) {
    auto b = backend();
    b.validator_fee_config.capacity = 4;

    pvmtest::BlsKey k1(31), k2(32);
    const std::string mgr = "0xmanager";
    const security::Mode sovereign{false, security::Admission::Open, 1000, security::Manager::Contract};

    state::MemState s;
    s.set_timestamp(kChainTime);
    s.set_accrued_fees(500);
    UTXO u;
    u.utxo = UtxoId{id_of(0xA0), 0};
    u.asset = kLux;
    u.out = TransferOutput{10'000'000'000, mine()};
    s.add_utxo(u);
    state::Diff layer(&s);

    const std::vector<txs::NetworkValidator> vdrs = {
        genesis_validator(0x70, 100, 2'000'000, k1.pop()),
        genesis_validator(0x71, 200, 0, k2.pop()),  // no balance: in the set, inactive
    };

    auto ut = txs::CreateNetworkTx::create(
        envelope(10'000'000'000, {out_to_me(10'000'000'000 - 2'000'000 - 1'000'000)}), kPrimaryNetworkId,
        mine(), sovereign, vdrs, kManagerChain, std::vector<std::uint8_t>(mgr.begin(), mgr.end()));
    REQUIRE_OK(ut);
    const auto tx = sign(ut.value());
    REQUIRE_OK(ex::standard_tx(b, tx, layer));

    // The network exists, and it is its own transaction's id.
    REQUIRE(layer.has_network(tx.tx_id));

    // Both validators are in its set; their names are DERIVED from the network's
    // id and their index, so nothing had to be agreed.
    const Id first = append_id(tx.tx_id, 0);
    const Id second = append_id(tx.tx_id, 1);
    auto a = layer.get_l1_validator(first);
    REQUIRE_OK(a);
    REQUIRE_EQ(tx.tx_id, a.value().chain_id);
    REQUIRE_EQ(node_of(0x70), a.value().node_id);
    REQUIRE_U64(100u, a.value().weight);
    REQUIRE_U64(kChainTime, a.value().start_time);
    // Prepaid from where the accrued clock stands, not from zero.
    REQUIRE_U64(2'000'500u, a.value().end_accumulated_fee);
    REQUIRE(a.value().is_active());
    REQUIRE_EQ_NUM(96, a.value().public_key.size());

    auto second_v = layer.get_l1_validator(second);
    REQUIRE_OK(second_v);
    REQUIRE(!second_v.value().is_active());
    REQUIRE_U64(200u, second_v.value().weight);
    REQUIRE_EQ_NUM(1, layer.num_active_l1_validators());
    REQUIRE_U64(300u, layer.weight_of_l1_validators(tx.tx_id).value());

    // And the authority that may change the set is recorded, under a conversion
    // id that is the hash of the set as it was established.
    auto conv = layer.network_conversion(tx.tx_id);
    REQUIRE_OK(conv);
    REQUIRE_EQ(kManagerChain, conv.value().chain_id);
    REQUIRE_EQ(std::vector<std::uint8_t>(mgr.begin(), mgr.end()), conv.value().addr);

    warpmsg::ConversionData expected;
    expected.chain_id = tx.tx_id;
    expected.manager_chain_id = kManagerChain;
    expected.manager_address.assign(mgr.begin(), mgr.end());
    for (const auto& v : vdrs)
        expected.validators.push_back(
            warpmsg::ConversionValidator{v.node_id, v.pop.public_key, v.weight});
    REQUIRE_EQ(expected.conversion_id().value(), conv.value().validation_id);

    // A network that is NOT sovereign gets no set and no authority: it leans on
    // its parent's validators instead.
    state::MemState plain_state;
    plain_state.set_timestamp(kChainTime);
    plain_state.add_utxo(u);
    state::Diff plain_layer(&plain_state);
    const security::Mode restaked{true, security::Admission::NoOwnSet, 0, security::Manager::PChain};
    auto pt = txs::CreateNetworkTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                            kPrimaryNetworkId, mine(), restaked, {}, kEmptyId, {});
    REQUIRE_OK(pt);
    const auto plain_tx = sign(pt.value());
    REQUIRE_OK(ex::standard_tx(b, plain_tx, plain_layer));
    REQUIRE(plain_layer.has_network(plain_tx.tx_id));
    REQUIRE_ERR(plain_layer.network_conversion(plain_tx.tx_id), Err::NotFound);
    REQUIRE_EQ_NUM(0, plain_layer.num_active_l1_validators());
}

// The promotion: a network that already exists establishes its own set, with
// its owner's authorisation.
TEST(ANetworkIsPromoted) {
    auto b = backend();
    b.validator_fee_config.capacity = 4;
    pvmtest::BlsKey k1(33);
    const std::string mgr = "0xmanager";
    const security::Mode sovereign{false, security::Admission::Open, 1000, security::Manager::Contract};

    state::MemState s;
    s.set_timestamp(kChainTime);
    s.add_network(kL1);
    s.set_network_owner(kL1, mine());
    UTXO u;
    u.utxo = UtxoId{id_of(0xA0), 0};
    u.asset = kLux;
    u.out = TransferOutput{10'000'000'000, mine()};
    s.add_utxo(u);

    const std::vector<txs::NetworkValidator> vdrs = {genesis_validator(0x70, 100, 3'000'000, k1.pop())};

    auto build = [&](const Id& network) {
        auto ut = txs::ConvertNetworkTx::create(
            envelope(10'000'000'000, {out_to_me(10'000'000'000 - 3'000'000 - 1'000'000)}), network,
            kPrimaryNetworkId, kManagerChain, sovereign,
            std::vector<std::uint8_t>(mgr.begin(), mgr.end()), vdrs, txs::Auth{0});
        // Two credentials: one for the spend, one for the network owner's
        // authorisation.
        return sign(ut.value(), 2);
    };

    {
        state::Diff layer(&s);
        const auto tx = build(kL1);
        REQUIRE_OK(ex::standard_tx(b, tx, layer));
        auto v = layer.get_l1_validator(append_id(kL1, 0));
        REQUIRE_OK(v);
        REQUIRE_U64(100u, v.value().weight);
        REQUIRE(v.value().is_active());
        REQUIRE_OK(layer.network_conversion(kL1));
    }
    {  // a network nobody owns cannot be promoted by anybody
        state::Diff layer(&s);
        REQUIRE_ERR(ex::standard_tx(b, build(id_of(0x88)), layer), Err::ChainNotFound);
    }
    {  // and a network that has ALREADY converted is immutable: its own rules
       // govern it now, not the P-chain owner
        state::MemState already = s;
        already.set_network_conversion(kL1, state::NetToL1Conversion{kManagerChain, {}, id_of(1)});
        state::Diff layer(&already);
        REQUIRE_ERR(ex::standard_tx(b, build(kL1), layer), Err::NotAuthorized);
    }
}
