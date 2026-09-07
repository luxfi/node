// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// durability_test.cpp — what the X-Chain still knows after it is killed.
//
// vm_test already restarts this chain, but it restarts it POLITELY: the Vm and
// the File are destroyed, so every destructor runs and every buffer is flushed
// on the way out. That proves the chain can be shut down and reopened. It does
// not prove durability, because the failure durability exists for is the one
// where nothing gets to run: the power goes, the machine is fenced, the process
// is killed.
//
// So here the writer is a second process, and it is destroyed with SIGKILL the
// instant after it accepts a block. A third process opens the same file and
// must find:
//
//   1. the same last accepted block, at the same height, carrying the same
//      execution root — the root is the assertion, because that is what the
//      node signed and what a peer will compare against;
//   2. the output that block SPENT still spent. This is the whole point. A
//      chain that came back having forgotten a spend would accept the same
//      transaction again, and the same money would be spent twice;
//   3. nothing from the transaction that was issued and never made it into an
//      accepted block.
//
// Then it spends the same output again and is refused, which is the guard
// stated as behaviour rather than as an absence in a map.

#include "lux/core/check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/vm.hpp"

#include <sys/wait.h>
#include <unistd.h>

#include <csignal>
#include <cstdio>
#include <cstring>
#include <string>

using namespace lux::xvm;
using namespace lux::core::test;
using namespace lux::xvm::test;

namespace {

VmConfig config(const Id& asset) {
    VmConfig cfg;
    cfg.network_id = kNetworkID;
    cfg.chain_id = chain_id();
    cfg.net_id = id(0x0A);
    cfg.fee_asset_id = asset;
    return cfg;
}

// spend builds a transaction spending genesis output `index` into one output of
// `amount`, signed by the key that owns it.
std::shared_ptr<txs::Tx> spend(const Id& asset, std::uint32_t index, std::uint64_t amount) {
    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->base.ins.push_back(
        txs::TransferableInput{txs::UTXOID{asset, index, false}, asset, tin(kStartingBalance)});
    utx->base.outs.push_back(txs::TransferableOutput{asset, tout(amount)});

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
    tx->creds.push_back(cred);
    (void)tx->initialize();
    return tx;
}

// ── the writer: accept a block, then die where it stands ─────────────────────

int child_write(const std::string& path) {
    auto f = store::File::open(path);
    if (!f) {
        std::fprintf(stderr, "child: %s\n", f.error().c_str());
        return 2;
    }
    auto genesis = genesis_asset(4);
    const Id asset = genesis->id();

    Vm vm(config(asset), the_fxs(), **f);
    if (auto r = vm.initialize({genesis}, kGenesisTime); !r) {
        std::fprintf(stderr, "child: %s\n", r.error().c_str());
        return 2;
    }
    vm.set_bootstrapped(true);
    vm.set_now(kGenesisTime + 1);

    auto tx = spend(asset, 0, 100);
    if (auto r = vm.issue(tx); !r) {
        std::fprintf(stderr, "child: %s\n", r.error().c_str());
        return 2;
    }
    auto blk = vm.build();
    if (blk == nullptr) {
        std::fprintf(stderr, "child: %s\n", vm.last_error().c_str());
        return 2;
    }
    blk->accept();

    // What this node decided, in the words a peer would compare: the block, its
    // height, and the execution root it signed.
    std::printf("%s %llu %s\n", hex(blk->id()).c_str(),
                static_cast<unsigned long long>(vm.last_accepted_height()),
                hex(blk->root()).c_str());
    std::fflush(stdout);

    // Issued, verified, never accepted. It must leave nothing behind: a chain
    // that came back holding it would be holding a block nobody agreed to.
    auto orphan = spend(asset, 1, 200);
    (void)vm.issue(orphan);
    auto never = vm.build();
    (void)never;  // built, and deliberately NOT accepted

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

void a_killed_chain_still_knows_what_it_spent(const std::string& self) {
    const std::string path = "/tmp/xvm-durability-" + std::to_string(::getpid());
    ::unlink(path.c_str());

    bool killed = false;
    const std::string said = run_child(self, path, killed);
    check(killed, "the writer was destroyed by SIGKILL, not shut down");
    check(!said.empty(), "and it had accepted a block before it died");
    if (!killed || said.empty()) {
        ::unlink(path.c_str());
        return;
    }

    std::string want_id, want_root;
    unsigned long long want_height = 0;
    {
        char id_buf[128] = {}, root_buf[128] = {};
        const int got = std::sscanf(said.c_str(), "%127s %llu %127s", id_buf, &want_height, root_buf);
        check(got == 3, "the writer said which block, at which height, with which root");
        if (got != 3) {
            ::unlink(path.c_str());
            return;
        }
        want_id = id_buf;
        want_root = root_buf;
    }

    auto f = store::File::open(path);
    check(f.has_value(), "a new process opens the store the dead one left");
    if (!f) {
        ::unlink(path.c_str());
        return;
    }

    auto genesis = genesis_asset(4);
    const Id asset = genesis->id();
    Vm vm(config(asset), the_fxs(), **f);
    // Genesis is offered again, as a host offers it on every boot. The store
    // already holds a chain, so it must not be installed a second time.
    auto r = vm.initialize({genesis}, kGenesisTime);
    check(r.has_value(), r ? "the chain comes back up" : "coming back up: " + r.error());
    if (!r) {
        ::unlink(path.c_str());
        return;
    }
    vm.set_bootstrapped(true);
    vm.set_now(kGenesisTime + 2);

    check_eq(hex(vm.last_accepted()), want_id, "it remembers the block it accepted");
    check(vm.last_accepted_height() == want_height, "at the height that block closed");

    auto blk = vm.get(vm.last_accepted());
    check(blk != nullptr, "the block itself is still there");
    if (blk != nullptr)
        check_eq(hex(blk->root()), want_root, "carrying the execution root it was signed with");

    // The claim this whole exercise exists for.
    const Id spent = txs::UTXOID{asset, 0, false}.input_id();
    check(!vm.chain_state().get_utxo(spent).has_value(),
          "the output that block spent is still spent");

    const Id orphan_out = txs::UTXOID{asset, 1, false}.input_id();
    check(vm.chain_state().get_utxo(orphan_out).has_value(),
          "and the output of the block that was never accepted is untouched");
    check(vm.chain_state().utxo_count() == 4,
          "the occupied set is exactly what the accepted history produced");

    // Stated as behaviour: offer the same spend again and be refused.
    auto again = spend(asset, 0, 100);
    const auto refused = vm.issue(again);
    check(!refused.has_value(),
          refused.has_value() ? "a restarted chain REFUSES to spend it twice"
                              : "a restarted chain refuses to spend it twice");

    // And it does not merely remember, it continues: the next block extends the
    // one that was on disk when the process died.
    auto next_tx = spend(asset, 1, 200);
    check(vm.issue(next_tx).has_value(), "a fresh transaction is admitted");
    auto next = vm.build();
    check(next != nullptr, next ? "the restarted chain builds" : "builds: " + vm.last_error());
    if (next != nullptr) {
        check(hex(next->parent()) == want_id, "on top of what was on disk");
        check(next->height() == want_height + 1, "at the next height");
        next->accept();
        check(vm.last_accepted_height() == want_height + 1, "and the chain moves on");
    }

    ::unlink(path.c_str());
}

}  // namespace

int main(int argc, char** argv) {
    if (argc == 3 && std::strcmp(argv[1], "--write") == 0) return child_write(argv[2]);

    std::printf("durability\n");
    a_killed_chain_still_knows_what_it_spent("/proc/self/exe");
    return report("durability");
}
