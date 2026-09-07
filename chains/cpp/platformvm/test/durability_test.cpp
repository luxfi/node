// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// durability_test.cpp — what the P-chain still knows after it is killed.
//
// The P-chain's state used to be maps and nothing else. A node that restarts
// having forgotten its validator set does not know who may vote; one that has
// forgotten its UTXO set has forgotten what has been spent, which is the same
// failure as a nullifier set that resets — the thing already spent can be spent
// again.
//
// So this test does not close anything politely. A second process fills the
// state, commits, prints the STATE ROOT it would have signed, then writes more
// without committing and has the kernel SIGKILL it: no destructor, no flush. A
// third process opens the same path, loads, and must fold to the same root.
//
// THE ROOT IS THE ASSERTION, not the row count. Two states that agree row by
// row but fold differently would sign different blocks, and it is the signature
// that matters. It covers the clock, the fee position, every network with its
// owner and supply, both staker sets in consensus order, the active L1
// validators, the expiries and every unspent output — so a single byte lost
// anywhere under it shows up here.
//
// It is a separate binary from the ported Go suite because it needs argv: the
// writer is this same program, re-EXECed, so it runs in a fresh address space
// and cannot be doing the store's job out of a page the parent already had.

#include "lux/core/check.hpp"
#include "lux/platformvm/state.hpp"
#include "lux/platformvm/txs.hpp"

#include <sys/wait.h>
#include <unistd.h>

#include <csignal>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

using namespace lux::platformvm;
using namespace lux::platformvm::state;
using namespace lux::core::test;

namespace {

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
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    v[0] = b;
    return v;
}

const Id kNetA = id_of(0xA1);
const Id kNetB = id_of(0xB2);

// One staker, named by its sequence so the same call makes the same staker in
// the writer and in the reader that checks what it wrote.
Staker staker(std::uint8_t seq, Priority p, std::uint64_t start) {
    Staker s;
    s.tx_id = id_of(seq);
    s.node_id = node_of(seq);
    s.chain_id = kNetA;
    s.weight = 1000 + seq;
    s.start_time = start;
    s.end_time = start + 14 * 24 * 60 * 60;
    s.potential_reward = 7 * seq;
    s.next_time = s.end_time;
    s.priority = p;
    if (seq % 2 == 0) {
        signer::PublicKeyBytes key{};
        key[0] = seq;
        key[47] = 0xEE;
        s.public_key = key;
    }
    return s;
}

l1::Validator l1_validator(std::uint8_t seq, std::uint64_t end_fee) {
    l1::Validator v;
    v.validation_id = id_of(std::uint8_t(0xC0 + seq));
    v.chain_id = kNetB;
    v.node_id = node_of(std::uint8_t(0xD0 + seq));
    v.public_key = std::vector<std::uint8_t>(96, std::uint8_t(seq));
    v.remaining_balance_owner = {1, 2, 3, seq};
    v.deactivation_owner = {9, 8, seq};
    v.start_time = 1'700'000'000 + seq;
    v.weight = 500 + seq;
    v.min_nonce = seq;
    v.end_accumulated_fee = end_fee;
    return v;
}

UTXO utxo(std::uint8_t seq, std::uint64_t amount) {
    UTXO u;
    u.utxo = UtxoId{id_of(seq), seq};
    u.asset = id_of(0x10);
    u.stake_lock = seq % 3 == 0 ? 1'800'000'000 : 0;
    u.out = TransferOutput{amount, OutputOwners{seq, 1, {short_of(seq), short_of(std::uint8_t(seq + 1))}}};
    return u;
}

// A transaction with no signatures on it: this test is about what survives, not
// about who signed, and a free-standing tx is the cheapest real one to make.
txs::Tx signed_free(std::shared_ptr<txs::UnsignedTx> u) {
    txs::Tx t;
    t.unsigned_tx = std::move(u);
    (void)t.initialize();
    return t;
}

txs::Tx a_transaction(std::uint8_t seq) {
    BaseTx b;
    b.network_id = 96369;
    b.blockchain_id = id_of(0x20);
    b.outs = {TransferableOutput{id_of(0x10), 0,
                                 TransferOutput{1'000'000 + seq, OutputOwners{0, 1, {short_of(0x30)}}}}};
    b.ins = {TransferableInput{UtxoId{id_of(0x60), seq}, id_of(0x10), 0,
                               TransferInput{1'000'042, {0}}}};
    return signed_free(txs::BaseTxUnsigned::create(b).value());
}

// fill puts a whole chain's worth of state in: everything the root folds over,
// and the families beside it that a restart must not lose either.
void fill(state::State& s) {
    s.set_timestamp(1'700'000'123);
    s.set_accrued_fees(4242);
    s.set_fee_state(gas::State{900'000, 12'345});
    s.set_l1_validator_excess(777);

    s.add_network(kNetA);
    s.add_network(kNetB);
    s.set_network_owner(kNetA, txs::Owner{OutputOwners{5, 2, {short_of(0x31), short_of(0x32)}}});
    s.set_current_supply(kNetA, 1'000'000'000);
    s.set_current_supply(kPrimaryNetworkId, 720'000'000'000'000);
    s.set_network_conversion(kNetB, state::NetToL1Conversion{id_of(0x51), {'m', 'g', 'r'}, id_of(0x52)});

    for (std::uint8_t i = 1; i <= 6; ++i) s.add_utxo(utxo(i, 1'000 * i));

    (void)s.put_current_validator(staker(0x11, Priority::PrimaryNetworkValidatorCurrent, 1000));
    (void)s.put_current_validator(staker(0x12, Priority::PrimaryNetworkValidatorCurrent, 2000));
    s.put_current_delegator(staker(0x13, Priority::PrimaryNetworkDelegatorCurrent, 1500));
    (void)s.put_pending_validator(staker(0x21, Priority::PrimaryNetworkValidatorPending, 9000));
    s.put_pending_delegator(staker(0x22, Priority::PrimaryNetworkDelegatorPermissionlessPending, 9500));

    // A delegatee ledger only exists for a validator that is IN the set, which
    // is why this comes after the puts above.
    (void)s.set_delegatee_reward(kNetA, node_of(0x11), 31337);

    (void)s.put_l1_validator(l1_validator(1, 5'000));
    (void)s.put_l1_validator(l1_validator(2, 9'000));
    // A validator with no balance left: in the set, weighing on it, not active.
    // It must survive AS inactive — a restart that woke it up would hand weight
    // to a node that cannot pay.
    (void)s.put_l1_validator(l1_validator(3, 0));

    s.put_expiry(l1::ExpiryEntry{1'800'000'000, id_of(0xE1)});
    s.put_expiry(l1::ExpiryEntry{1'800'000'050, id_of(0xE2)});

    s.add_tx(a_transaction(1), status::Status::Committed);
    s.add_tx(a_transaction(2), status::Status::Aborted);
    s.add_reward_utxo(id_of(0x77), utxo(9, 55));
    s.add_reward_utxo(id_of(0x77), utxo(10, 66));
}

// ── the writer ───────────────────────────────────────────────────────────────

int child_write(const std::string& path) {
    auto f = lux::core::store::File::open(path);
    if (!f) {
        std::fprintf(stderr, "child: %s\n", f.error().c_str());
        return 2;
    }
    state::State s(**f);
    fill(s);
    if (auto r = s.commit(); !r) {
        std::fprintf(stderr, "child: %s\n", r.error().message().c_str());
        return 2;
    }

    // What the writer would have signed.
    std::printf("%s\n", hex(state::state_root(s)).c_str());
    std::fflush(stdout);

    // And now a block that is verified and never accepted: a new validator, a
    // spent output, a moved clock. None of it is committed, so none of it may
    // come back — a node that resurrected an uncommitted staker would be
    // counting weight nobody agreed to.
    (void)s.put_current_validator(staker(0x31, Priority::PrimaryNetworkValidatorCurrent, 3000));
    s.delete_utxo(utxo(1, 1000).id());
    s.set_timestamp(1'999'999'999);

    ::raise(SIGKILL);
    return 3;  // unreachable unless the signal was blocked
}

// ── the reader ───────────────────────────────────────────────────────────────

std::string run_child(const std::string& self, const std::string& path, bool& killed) {
    killed = false;
    int fds[2];
    if (::pipe(fds) != 0) return {};
    const pid_t pid = ::fork();
    if (pid < 0) {
        ::close(fds[0]);
        ::close(fds[1]);
        return {};
    }
    if (pid == 0) {
        ::close(fds[0]);
        ::dup2(fds[1], STDOUT_FILENO);
        ::close(fds[1]);
        ::execl(self.c_str(), self.c_str(), "--write", path.c_str(), nullptr);
        ::_exit(127);
    }
    ::close(fds[1]);
    std::string out;
    char buf[256];
    ssize_t n;
    while ((n = ::read(fds[0], buf, sizeof(buf))) > 0) out.append(buf, std::size_t(n));
    ::close(fds[0]);
    int status = 0;
    ::waitpid(pid, &status, 0);
    killed = WIFSIGNALED(status) && WTERMSIG(status) == SIGKILL;
    while (!out.empty() && (out.back() == '\n' || out.back() == '\r')) out.pop_back();
    return out;
}

void the_chain_comes_back_knowing_what_it_accepted(const std::string& self) {
    const std::string path = "/tmp/lux-pvm-durability-" + std::to_string(::getpid()) + ".log";
    ::unlink(path.c_str());

    bool killed = false;
    const std::string written_root = run_child(self, path, killed);
    check(killed, "the writer was destroyed by SIGKILL, not shut down");
    check(!written_root.empty(), "and it had committed a state before it died");
    if (!killed || written_root.empty()) {
        ::unlink(path.c_str());
        return;
    }

    auto f = lux::core::store::File::open(path);
    check(f.has_value(), "a new process opens the store the dead one left");
    if (!f) {
        ::unlink(path.c_str());
        return;
    }
    state::State s(**f);
    const auto loaded = s.load();
    check(loaded.has_value(),
          loaded ? "the state loads" : "the state loads: " + loaded.error().message());
    if (!loaded) {
        ::unlink(path.c_str());
        return;
    }

    // THE assertion.
    check_eq(hex(state::state_root(s)), written_root,
             "the state root the reader folds is the one the writer signed");

    // And what the root is made of, named one family at a time, so a failure
    // says WHICH part of the chain was forgotten rather than only that some
    // byte moved.
    check(s.timestamp() == 1'700'000'123, "the clock survived");
    check(s.accrued_fees() == 4242 && s.fee_state().capacity == 900'000 &&
              s.fee_state().excess == 12'345 && s.l1_validator_excess() == 777,
          "the fee position survived");
    check(s.has_network(kNetA) && s.has_network(kNetB), "the networks survived");
    check(s.network_owner(kNetA).has_value(), "and the owner of one of them");
    check(s.current_supply(kNetA).value_or(0) == 1'000'000'000 &&
              s.current_supply(kPrimaryNetworkId).value_or(0) == 720'000'000'000'000,
          "so did the supply, the primary network's included");
    check(s.network_conversion(kNetB).has_value(), "and what the other network became");

    check(s.current_stakers().size() == 3, "every current staker came back");
    check(s.pending_stakers().size() == 2, "and every pending one");
    const auto v = s.get_current_validator(kNetA, node_of(0x11));
    check(v.has_value() && v->weight == 1000 + 0x11 && v->potential_reward == 7 * 0x11,
          "a validator came back with its weight and its reward");
    const auto keyed = s.get_current_validator(kNetA, node_of(0x12));
    check(keyed.has_value() && keyed->public_key.has_value() &&
              (*keyed->public_key)[47] == 0xEE,
          "and one with a BLS key came back with the key");
    check(s.current_delegators(kNetA, node_of(0x13)).size() +
                  s.current_delegators(kNetA, node_of(0x11)).size() >
              0,
          "a delegator is still in the set it was in");
    check(s.delegatee_reward(kNetA, node_of(0x11)).value_or(0) == 31337,
          "the rewards a validator accrued from its delegators survived");

    check(s.num_active_l1_validators() == 2, "the ACTIVE L1 validators came back active");
    check(s.l1_validators(kNetB).size() == 3, "and the inactive one came back inactive, not gone");
    check(s.expiries().size() == 2, "the expiries survived");
    check(s.utxos().size() == 6, "every unspent output survived");
    check(s.get_utxo(utxo(4, 4000).id()).has_value(), "and is reachable by its own name");

    check(s.get_tx(id_of(0)).has_value() == false, "an id nothing wrote is still absent");
    check(s.reward_utxos(id_of(0x77)).size() == 2, "the reward outputs of a transaction survived");

    // Nothing after the last commit.
    check(!s.get_current_validator(kNetA, node_of(0x31)).has_value(),
          "a staker added after the last commit never happened");
    check(s.timestamp() != 1'999'999'999, "nor did the clock move it set afterwards");

    // A second reader must agree with the first: a state that recovers
    // differently the second time recovers by accident.
    {
        auto g = lux::core::store::File::open(path);
        check(g.has_value(), "the store opens again");
        if (g) {
            state::State again(**g);
            check(again.load().has_value(), "and loads again");
            check_eq(hex(state::state_root(again)), written_root, "to the same root a third time");
        }
    }

    ::unlink(path.c_str());
}

// A commit must also be the ONLY thing that changes the disk. This is the same
// claim from the other side and it needs no kill: write, commit, write again,
// and reload without committing the second write.
void only_a_commit_changes_what_survives() {
    const std::string path = "/tmp/lux-pvm-commit-" + std::to_string(::getpid()) + ".log";
    ::unlink(path.c_str());
    {
        auto f = lux::core::store::File::open(path);
        check(f.has_value(), "open a fresh store");
        if (!f) return;
        state::State s(**f);
        fill(s);
        check(s.commit().has_value(), "commit the state");

        // A staker leaves. Committing that must make the ABSENCE durable, which
        // is the half a store that only ever appends gets wrong.
        s.delete_current_validator(staker(0x12, Priority::PrimaryNetworkValidatorCurrent, 2000));
        check(s.commit().has_value(), "commit its departure");
    }
    {
        auto f = lux::core::store::File::open(path);
        check(f.has_value(), "reopen");
        if (!f) return;
        state::State s(**f);
        check(s.load().has_value(), "load");
        check(!s.get_current_validator(kNetA, node_of(0x12)).has_value(),
              "a validator that left is still gone after a restart");
        check(s.current_stakers().size() == 2, "and the set is one smaller, not one larger");
    }
    ::unlink(path.c_str());
}

}  // namespace

int main(int argc, char** argv) {
    if (argc == 3 && std::strcmp(argv[1], "--write") == 0) return child_write(argv[2]);

    std::printf("durability\n");
    the_chain_comes_back_knowing_what_it_accepted("/proc/self/exe");
    only_a_commit_changes_what_survives();
    return report("durability");
}
