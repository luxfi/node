// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// persist_test.cpp — the state, on a disk, and what a kill does to it.
//
// Three things a restarted P-chain cannot do without, each proved twice: once
// against a store that is only opened and closed, and once against a store
// whose writer was KILLED where a clean shutdown would have been.
//
//   the state       every family, and the root over it. A state that comes back
//                   looking right and hashing differently is a node voting
//                   against its own network.
//   the position    which block was last accepted. A node that forgot would
//                   accept a second block at the same height.
//   the history     the validator set at every past height. A signature made at
//                   height H is checked against the set at H, so a node that
//                   lost those sets can check nothing older than its own last
//                   block.
//
// The kill is a real one: a forked child writes, commits, appends the first half
// of a further record — what a process killed inside write leaves behind — and
// is then SIGKILLed. No destructor runs and nothing is flushed on the way out.

#include "harness.hpp"
#include "signing.hpp"
#include "lux/platformvm/persist.hpp"
#include "lux/platformvm/validators.hpp"

#include <fcntl.h>
#include <signal.h>
#include <sys/wait.h>
#include <unistd.h>

#include <functional>
#include <map>
#include <string>

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

ShortId addr_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

const signer::ProofOfPossession& pop(std::uint8_t seed) {
    static std::map<std::uint8_t, pvmtest::BlsKey> keys;
    auto it = keys.find(seed);
    if (it == keys.end()) it = keys.emplace(seed, pvmtest::BlsKey(seed)).first;
    return it->second.pop();
}

struct Scratch {
    std::string path;
    explicit Scratch(const std::string& name) {
        path = "/tmp/lux-pvm-persist-" + std::to_string(::getpid()) + "-" + name;
        ::unlink(path.c_str());
    }
    ~Scratch() {
        ::unlink(path.c_str());
        ::unlink((path + ".compact").c_str());
    }
};

txs::Owner owner_of(std::uint8_t n) {
    txs::Owner o;
    o.locktime = n;
    o.threshold = 1;
    o.addrs = {addr_of(n), addr_of(static_cast<std::uint8_t>(n + 40))};
    return o;
}

UTXO utxo_of(std::uint8_t tx, std::uint32_t index, std::uint64_t amount) {
    UTXO u;
    u.utxo.tx_id = id_of(tx);
    u.utxo.output_index = index;
    u.asset = id_of(0xA0);
    u.stake_lock = 0;
    u.out.amt = amount;
    u.out.owners = owner_of(tx);
    return u;
}

// `node` is separate from `n` because a delegator backs SOMEONE ELSE'S node —
// which is exactly the case a naive round trip loses.
state::Staker staker_of(std::uint8_t n, const Id& chain, std::uint64_t weight,
                        std::optional<signer::PublicKeyBytes> key, txs::Priority p,
                        std::uint8_t node = 0) {
    state::Staker s;
    s.tx_id = id_of(n);
    s.node_id = node_of(node == 0 ? n : node);
    s.chain_id = chain;
    s.weight = weight;
    s.public_key = std::move(key);
    s.start_time = 10;
    s.end_time = 1'000'000 + n;
    s.potential_reward = 7 * n;
    s.next_time = s.end_time;
    s.priority = p;
    return s;
}

l1::Validator l1_of(std::uint8_t n, std::uint64_t weight) {
    l1::Validator v;
    v.validation_id = id_of(static_cast<std::uint8_t>(0xB0 + n));
    v.chain_id = id_of(0x50);
    v.node_id = node_of(static_cast<std::uint8_t>(0x70 + n));
    v.public_key = std::vector<std::uint8_t>(96, static_cast<std::uint8_t>(n));
    v.remaining_balance_owner = {1, 2, 3};
    v.deactivation_owner = {4, 5};
    v.start_time = 100 + n;
    v.weight = weight;
    v.min_nonce = n;
    v.end_accumulated_fee = 1;
    return v;
}

// A state with something in every family, so a family left unwritten shows up as
// a difference rather than as a passing test.
state::MemState populated() {
    state::MemState s;
    s.set_timestamp(1'712'345'678);
    s.set_accrued_fees(4321);
    s.set_fee_state(gas::State{5000, 90});
    s.set_l1_validator_excess(17);

    (void)s.put_current_validator(staker_of(2, kPrimaryNetworkId, 2000, pop(2).public_key,
                                            txs::Priority::PrimaryNetworkValidatorCurrent));
    (void)s.put_current_validator(staker_of(4, id_of(0x50), 4000, pop(4).public_key,
                                            txs::Priority::ChainPermissionedValidatorCurrent));
    // A delegator behind the validator at node 2: many may share a node, and
    // which staker was seated there is not recoverable from the staker alone.
    s.put_current_delegator(staker_of(5, kPrimaryNetworkId, 500, std::nullopt,
                                      txs::Priority::PrimaryNetworkDelegatorCurrent, 2));
    (void)s.put_pending_validator(staker_of(6, kPrimaryNetworkId, 6000, pop(6).public_key,
                                            txs::Priority::PrimaryNetworkValidatorPending));
    s.put_pending_delegator(staker_of(7, kPrimaryNetworkId, 700, std::nullopt,
                                      txs::Priority::PrimaryNetworkDelegatorLegacyPending, 6));

    for (std::uint8_t i = 1; i <= 4; ++i) s.add_utxo(utxo_of(i, i, 100u * i));
    // A spend, so the disk has to have recorded a removal and not just adds.
    s.delete_utxo(utxo_of(3, 3, 300).id());

    s.set_current_supply(kPrimaryNetworkId, 720'000'000);
    s.set_current_supply(id_of(0x50), 42);
    s.add_network(id_of(0x50));
    s.set_network_owner(id_of(0x50), owner_of(9));
    state::NetToL1Conversion c;
    c.chain_id = id_of(0x51);
    c.validation_id = id_of(0x52);
    c.addr = {9, 8, 7, 6};
    s.set_network_conversion(id_of(0x50), c);

    (void)s.put_l1_validator(l1_of(1, 1500));
    (void)s.put_l1_validator(l1_of(2, 2500));
    s.put_expiry(l1::ExpiryEntry{900, id_of(0xE1)});
    s.put_expiry(l1::ExpiryEntry{950, id_of(0xE2)});

    (void)s.set_delegatee_reward(kPrimaryNetworkId, node_of(2), 55);
    s.add_reward_utxo(id_of(0x20), utxo_of(8, 0, 800));
    s.add_reward_utxo(id_of(0x20), utxo_of(8, 1, 801));
    return s;
}

// The state the writer was part-way through when it died. It must NOT come back.
state::MemState uncommitted() {
    state::MemState s = populated();
    s.add_utxo(utxo_of(9, 9, 9000));
    s.set_timestamp(1'799'999'999);
    return s;
}

persist::Position position_of() {
    persist::Position p;
    for (std::uint64_t h = 1; h <= 4; ++h) {
        const Id id = id_of(static_cast<std::uint8_t>(0xC0 + h));
        p.blocks[id] = std::vector<std::uint8_t>(16 + h, static_cast<std::uint8_t>(h));
        p.by_height[h] = id;
        p.last_accepted = id;
        p.height = h;
    }
    return p;
}

// The validator set at each height, from one to three. Height zero is the empty
// set the chain started from.
std::vector<std::map<NodeId, validators::Validator>> heights(state::MemState& s,
                                                             validators::History& h) {
    std::vector<std::map<NodeId, validators::Validator>> out;
    out.push_back({});  // height 0: nothing

    const auto settle = [&](std::uint64_t height, const std::function<void(state::Diff&)>& layer) {
        state::Diff d(&s);
        layer(d);
        auto c = validators::changes(d, s);
        if (!c) return false;
        if (auto st = h.record(height, c.value()); !st) return false;
        if (auto st = d.apply(s); !st) return false;
        out.push_back(validators::current_set(s, kPrimaryNetworkId).value());
        return true;
    };

    if (!settle(1, [](state::Diff& d) {
            (void)d.put_current_validator(staker_of(11, kPrimaryNetworkId, 100, pop(11).public_key,
                                                    txs::Priority::PrimaryNetworkValidatorCurrent));
        }))
        return {};
    if (!settle(2, [](state::Diff& d) {
            (void)d.put_current_validator(staker_of(12, kPrimaryNetworkId, 200, pop(12).public_key,
                                                    txs::Priority::PrimaryNetworkValidatorCurrent));
        }))
        return {};
    if (!settle(3, [](state::Diff& d) {
            d.delete_current_validator(staker_of(11, kPrimaryNetworkId, 100, pop(11).public_key,
                                                 txs::Priority::PrimaryNetworkValidatorCurrent));
        }))
        return {};
    return out;
}

// Append raw bytes, the way a killed process leaves them.
bool append_raw(const std::string& path, const store::Bytes& b) {
    const int fd = ::open(path.c_str(), O_WRONLY | O_APPEND);
    if (fd < 0) return false;
    const bool ok = ::write(fd, b.data(), b.size()) == static_cast<ssize_t>(b.size());
    ::close(fd);
    return ok;
}

std::size_t file_size(const std::string& path) {
    const int fd = ::open(path.c_str(), O_RDONLY);
    if (fd < 0) return 0;
    const off_t n = ::lseek(fd, 0, SEEK_END);
    ::close(fd);
    return n < 0 ? 0 : static_cast<std::size_t>(n);
}

}  // namespace

// ── the state, over a store that is merely closed and opened

TEST(AStateWrittenToMemoryComesBackTheSame) {
    const state::MemState s = populated();
    store::Memory disk;
    REQUIRE_OK(persist::save(s, disk));
    auto back = persist::load(disk);
    REQUIRE(back.has_value());
    REQUIRE(state::state_root(back.value()) == state::state_root(s));
    REQUIRE_EQ_NUM(s.utxos().size(), back.value().utxos().size());
    REQUIRE(back.value().utxos() == s.utxos());
    REQUIRE(back.value().current_stakers() == s.current_stakers());
    REQUIRE(back.value().pending_stakers() == s.pending_stakers());
    REQUIRE(back.value().l1_validators() == s.l1_validators());
    REQUIRE(back.value().expiries() == s.expiries());
    REQUIRE(back.value().reward_utxos() == s.reward_utxos());
    REQUIRE(back.value().delegatee_rewards() == s.delegatee_rewards());
    REQUIRE(back.value().conversions() == s.conversions());
    REQUIRE(back.value().network_owners() == s.network_owners());
    REQUIRE(back.value().supplies() == s.supplies());
    REQUIRE_U64(s.accrued_fees(), back.value().accrued_fees());
    REQUIRE(back.value().fee_state() == s.fee_state());
    REQUIRE_U64(s.l1_validator_excess(), back.value().l1_validator_excess());
}

// The distinction a naive round trip loses: a delegator sharing a node with its
// validator must not come back as a second validator.
TEST(ADelegatorDoesNotComeBackAsAValidator) {
    const state::MemState s = populated();
    store::Memory disk;
    REQUIRE_OK(persist::save(s, disk));
    auto back = persist::load(disk);
    REQUIRE(back.has_value());

    auto seated = back.value().get_current_validator(kPrimaryNetworkId, node_of(2));
    REQUIRE(seated.has_value());
    REQUIRE(seated.value().tx_id == id_of(2));
    REQUIRE_EQ_NUM(1, back.value().current_delegators(kPrimaryNetworkId, node_of(2)).size());
    // And its weight still counts toward the validator it backs.
    REQUIRE(validators::current_set(back.value(), kPrimaryNetworkId).value() ==
            validators::current_set(s, kPrimaryNetworkId).value());
}

TEST(AStateWrittenToAFileComesBackAfterTheFileIsReopened) {
    Scratch sc("reopen");
    const state::MemState s = populated();
    {
        auto disk = store::File::open(sc.path);
        REQUIRE(disk.has_value());
        REQUIRE_OK(persist::save(s, *disk.value()));
    }
    auto disk = store::File::open(sc.path);
    REQUIRE(disk.has_value());
    auto back = persist::load(*disk.value());
    REQUIRE(back.has_value());
    REQUIRE(state::state_root(back.value()) == state::state_root(s));
    REQUIRE(back.value().utxos() == s.utxos());
}

TEST(SavingTwiceWithNoChangeWritesNothing) {
    Scratch sc("idempotent");
    const state::MemState s = populated();
    auto disk = store::File::open(sc.path);
    REQUIRE(disk.has_value());
    REQUIRE_OK(persist::save(s, *disk.value()));
    const std::size_t after_first = disk.value()->log_bytes();
    REQUIRE_OK(persist::save(s, *disk.value()));
    REQUIRE_EQ_NUM(after_first, disk.value()->log_bytes());
}

TEST(WhereTheChainGotToComesBack) {
    Scratch sc("position");
    const persist::Position p = position_of();
    {
        auto disk = store::File::open(sc.path);
        REQUIRE(disk.has_value());
        REQUIRE_OK(persist::save_position(p, *disk.value()));
    }
    auto disk = store::File::open(sc.path);
    REQUIRE(disk.has_value());
    auto back = persist::load_position(*disk.value());
    REQUIRE(back.has_value());
    REQUIRE(back.value() == p);
}

TEST(APastValidatorSetComesBackFromTheDisk) {
    Scratch sc("history");
    state::MemState s;
    validators::History h;
    const auto sets = heights(s, h);
    REQUIRE_EQ_NUM(4, sets.size());
    {
        auto disk = store::File::open(sc.path);
        REQUIRE(disk.has_value());
        REQUIRE_OK(h.save(*disk.value()));
    }
    auto disk = store::File::open(sc.path);
    REQUIRE(disk.has_value());
    auto back = validators::History::load(*disk.value());
    REQUIRE(back.has_value());

    const std::uint64_t now = sets.size() - 1;
    for (std::uint64_t target = 0; target <= now; ++target) {
        auto set = sets[now];
        REQUIRE_OK(back.value().rewind(set, kPrimaryNetworkId, now, target));
        REQUIRE_MSG(set == sets[target],
                    "the set at height " + std::to_string(target) + " came back wrong");
    }
}

// ── and the same three, over a store whose writer was killed

TEST(TheStateSurvivesTheProcessThatWroteItBeingKilled) {
    Scratch sc("crash");
    const state::MemState committed = populated();
    const persist::Position pos = position_of();

    state::MemState walked;
    validators::History history;
    const auto sets = heights(walked, history);
    REQUIRE_EQ_NUM(4, sets.size());

    const pid_t child = ::fork();
    REQUIRE(child >= 0);
    if (child == 0) {
        // The writer. It does not return.
        auto disk = store::File::open(sc.path);
        if (!disk) ::_exit(70);
        if (!persist::save(committed, *disk.value())) ::_exit(71);
        if (!persist::save_position(pos, *disk.value())) ::_exit(72);
        if (!history.save(*disk.value())) ::_exit(73);

        // A further commit, cut off partway — exactly the bytes a process killed
        // inside write leaves behind.
        store::Batch torn;
        const std::string k = "\x02torn-row-that-never-landed";
        torn[store::Bytes(k.begin(), k.end())] = store::Bytes(64, 0xAB);
        const store::Bytes record = store::encode_batch(torn);
        if (!append_raw(sc.path, store::Bytes(record.begin(),
                                              record.begin() + record.size() / 2)))
            ::_exit(74);

        // Where a clean shutdown would have been. Nothing is flushed, no
        // destructor runs, the file is closed by the kernel and nothing else.
        ::kill(::getpid(), SIGKILL);
        ::_exit(75);
    }

    int status = 0;
    REQUIRE(::waitpid(child, &status, 0) == child);
    // It must have DIED, not exited. A child that returned would mean this test
    // proved nothing at all.
    REQUIRE_MSG(WIFSIGNALED(status), "the writer exited instead of dying");
    REQUIRE_EQ_NUM(SIGKILL, WTERMSIG(status));

    auto disk = store::File::open(sc.path);
    REQUIRE(disk.has_value());

    auto back = persist::load(*disk.value());
    REQUIRE(back.has_value());
    REQUIRE_MSG(state::state_root(back.value()) == state::state_root(committed),
                "the state came back with a different root");
    REQUIRE_MSG(!(state::state_root(back.value()) == state::state_root(uncommitted())),
                "the state that was never committed came back");
    REQUIRE(back.value().utxos() == committed.utxos());
    REQUIRE(back.value().current_stakers() == committed.current_stakers());
    REQUIRE(back.value().pending_stakers() == committed.pending_stakers());
    REQUIRE(back.value().l1_validators() == committed.l1_validators());
    REQUIRE(back.value().reward_utxos() == committed.reward_utxos());

    auto where = persist::load_position(*disk.value());
    REQUIRE(where.has_value());
    REQUIRE_MSG(where.value() == pos, "the chain came back not knowing its last block");

    auto past = validators::History::load(*disk.value());
    REQUIRE(past.has_value());
    const std::uint64_t now = sets.size() - 1;
    for (std::uint64_t target = 0; target <= now; ++target) {
        auto set = sets[now];
        REQUIRE_OK(past.value().rewind(set, kPrimaryNetworkId, now, target));
        REQUIRE_MSG(set == sets[target],
                    "the set at height " + std::to_string(target) + " did not survive the kill");
    }

    // And the torn record never happened.
    const std::string k = "\x02torn-row-that-never-landed";
    REQUIRE(!disk.value()
                 ->get(store::ByteView(reinterpret_cast<const std::uint8_t*>(k.data()), k.size()))
                 .has_value());
    REQUIRE_EQ_NUM(disk.value()->log_bytes(), file_size(sc.path));
}
