// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fixtures.hpp — the shared bench the F-Chain tests work on: identities that
// can actually sign, a chain seeded the way a real one is, and one builder per
// operation.
//
// These render the Go package's own test helpers, so a case ported from there
// reads the same here and a divergence shows up as a behaviour difference
// rather than as a difference in how the two were set up.

#pragma once

#include "check.hpp"

#include "signer.hpp"

#include "lux/fhevm/service.hpp"
#include "lux/fhevm/vm.hpp"

#include <algorithm>
#include <memory>
#include <string>
#include <vector>

namespace lux::fhevm::test {

// kTestGas is a ceiling above every scheduled operation, so a test that is not
// about metering never trips the limit.
inline constexpr std::uint64_t kTestGas = 500'000;
// kTestFund is enough for many operations at the scheduled prices.
inline constexpr std::uint64_t kTestFund = 10'000'000'000ULL;
// kTestScheme is the scheme most tests price against.
inline constexpr std::string_view kTestScheme = "ckks-n14";

// kTestChainID is THE chain id. Production runs ONE F-Chain per network, and
// every validator of it — proposer and follower alike — holds the same id: it
// is what a payer signs over and what a block id commits to. So the harness
// holds it constant too. Giving each node its own would make a proposer and a
// follower two different chains, which is not a shape production ever has, and
// would quietly excuse the very binding these tests exist to check. A test that
// is genuinely ABOUT two chains names its second one itself.
inline Id test_chain_id() {
    Id id{};
    constexpr std::string_view w = "fchain-test";
    std::copy(w.begin(), w.end(), id.begin());
    return id;
}

// kTestGenesisTime fixes genesis, for the same reason: every validator of one
// chain reads one genesis, so its id is one value.
inline constexpr std::int64_t kTestGenesisTime = 1'700'000'000;

// TestKey is an external identity: a payer, a grantee, or a committee member.
// The ML-DSA-65 private key lives ONLY in the test, exercising the public-key
// authentication path on the VM side.
struct TestKey {
    Bytes pub;
    Bytes sec;
    Account addr{};

    std::string hex_addr() const { return hex(view(addr)); }

    // sign attaches the payer's public key and a valid signature over the
    // transaction's signing bytes for THE chain under test, then clears the
    // cached id so it recomputes.
    void sign(Transaction& tx) const { sign_for(tx, test_chain_id()); }

    // sign_for signs for a named chain. Only a test that is about more than one
    // chain needs it.
    void sign_for(Transaction& tx, const Id& chain) const {
        tx.auth = pub;
        Bytes s;
        if (!make_signature(view(sec), view(tx.signing_bytes(chain)), &s)) {
            std::printf("  FAIL  the harness could not sign\n");
            ++g_fail;
        }
        tx.sig = std::move(s);
        tx.invalidate_id();
    }
};

inline TestKey new_test_key() {
    TestKey k;
    if (!make_keypair(&k.pub, &k.sec)) {
        std::printf("  FAIL  the harness could not generate a key\n");
        ++g_fail;
    }
    k.addr = address_of(view(k.pub));
    return k;
}

// new_committee builds n members in canonical node-id order, each carrying a
// real ML-DSA-65 public key, and returns the members alongside the keys that
// can sign for them (index-aligned).
struct Committee {
    std::vector<CommitteeMember> members;
    std::vector<TestKey> keys;
};

inline Committee new_committee(std::size_t n) {
    struct Pair {
        CommitteeMember m;
        TestKey k;
    };
    std::vector<Pair> pairs;
    pairs.reserve(n);
    for (std::size_t i = 0; i < n; ++i) {
        TestKey k = new_test_key();
        CommitteeMember m;
        // The node id is derived from the key, so it is distinct per member and
        // stable within a run without a random source of its own.
        Id h = sha256(view(k.pub));
        std::copy(h.begin(), h.begin() + std::int64_t(m.node_id.size()), m.node_id.begin());
        m.public_key = k.pub;
        m.public_key_nil = false;
        m.weight = 1;
        pairs.push_back(Pair{std::move(m), std::move(k)});
    }
    std::sort(pairs.begin(), pairs.end(),
              [](const Pair& a, const Pair& b) { return a.m.node_id < b.m.node_id; });
    Committee c;
    for (std::size_t i = 0; i < pairs.size(); ++i) {
        pairs[i].m.index = std::int64_t(i);
        c.members.push_back(pairs[i].m);
        c.keys.push_back(pairs[i].k);
    }
    return c;
}

inline Bytes network_public_key() {
    constexpr std::string_view w = "network-fhe-public-key";
    return Bytes(w.begin(), w.end());
}

// Chain owns a store and the VM over it, because the VM does not own its store
// and a test that let one outlive the other would be testing its own harness.
// A caller that brings its own store — one that can be made to fail — hands it
// in instead, and `owned` stays empty.
struct Chain {
    std::unique_ptr<Memory> owned;
    Store* store = nullptr;
    std::unique_ptr<VM> vm;

    VM* operator->() const { return vm.get(); }
    VM& operator*() const { return *vm; }
};

inline Genesis genesis_for(const std::map<std::string, std::uint64_t>& alloc,
                           const std::vector<CommitteeMember>& committee,
                           std::int64_t threshold) {
    Genesis g;
    g.version = 1;
    g.timestamp = kTestGenesisTime;
    g.alloc = alloc;
    g.alloc_nil = alloc.empty();
    g.committee = committee;
    g.committee_nil = committee.empty();
    g.threshold = threshold;
    if (!committee.empty()) {
        g.public_key = network_public_key();
        g.public_key_nil = false;
    }
    return g;
}

// new_test_vm initializes an in-memory F-Chain seeded with the given funding
// allocation (hex address -> nLUX) and epoch-0 committee.
inline Chain new_test_vm(const std::map<std::string, std::uint64_t>& alloc,
                         const std::vector<CommitteeMember>& committee, std::int64_t threshold,
                         Store* over = nullptr, const Id& chain = test_chain_id()) {
    Chain c;
    if (over == nullptr) {
        c.owned = std::make_unique<Memory>();
        over = c.owned.get();
    }
    c.store = over;
    c.vm = std::make_unique<VM>(over, VM::Config{96369, chain, "F"});
    auto ok = c.vm->initialize(marshal(genesis_for(alloc, committee, threshold)));
    if (!ok) {
        std::printf("  FAIL  the harness could not initialize a chain: %s\n",
                    ok.error().message().c_str());
        ++g_fail;
    }
    // Chain time is driven, not read: every expiry these tests exercise is
    // measured from genesis, and a wall clock would make them depend on when
    // they ran.
    c.vm->clock().set(kTestGenesisTime);
    return c;
}

// fund_all builds a genesis allocation funding every given key.
inline std::map<std::string, std::uint64_t> fund_all(const std::vector<TestKey>& keys) {
    std::map<std::string, std::uint64_t> alloc;
    for (const auto& k : keys) alloc[k.hex_addr()] = kTestFund;
    return alloc;
}

inline Id digest_of(std::string_view s) { return sha256(view(s)); }

// ---- one builder per operation ------------------------------------------------

inline Transaction register_tx(const TestKey& k, std::string_view scheme, const Id& digest,
                               std::uint64_t nonce) {
    Transaction tx;
    tx.type = kTxRegisterCiphertext;
    tx.scheme = std::string(scheme);
    tx.payer = k.addr;
    tx.subject = derive_handle(digest, scheme);
    tx.gas_limit = kTestGas;
    tx.nonce = nonce;
    RegisterPayload p;
    p.digest = digest;
    p.type = 4;
    p.level = 3;
    p.size = 4096;
    std::string j = marshal(p);
    tx.payload = Bytes(j.begin(), j.end());
    k.sign(tx);
    return tx;
}

inline Transaction grant_tx(const TestKey& owner, const Id& handle, const Account& grantee,
                            std::uint32_t ops, std::int64_t expiry, std::uint64_t nonce) {
    Transaction tx;
    tx.type = kTxGrantPermit;
    tx.payer = owner.addr;
    tx.subject = handle;
    tx.gas_limit = kTestGas;
    tx.nonce = nonce;
    GrantPayload p;
    p.grantee = grantee;
    p.operations = ops;
    p.expiry = expiry;
    std::string j = marshal(p);
    tx.payload = Bytes(j.begin(), j.end());
    owner.sign(tx);
    return tx;
}

inline Transaction revoke_tx(const TestKey& k, const Id& permit_id, std::uint64_t nonce) {
    Transaction tx;
    tx.type = kTxRevokePermit;
    tx.payer = k.addr;
    tx.subject = permit_id;
    tx.gas_limit = kTestGas;
    tx.nonce = nonce;
    RevokePayload p;
    p.reason = "no longer sanctioned";
    std::string j = marshal(p);
    tx.payload = Bytes(j.begin(), j.end());
    k.sign(tx);
    return tx;
}

inline Transaction request_tx(const TestKey& k, std::string_view scheme, const Id& handle,
                              const Id& permit_id, std::int64_t expiry, std::uint64_t nonce) {
    Transaction tx;
    tx.type = kTxRequestDecrypt;
    tx.scheme = std::string(scheme);
    tx.payer = k.addr;
    tx.subject = handle;
    tx.gas_limit = kTestGas;
    tx.nonce = nonce;
    RequestPayload p;
    p.permit_id = permit_id;
    p.callback[0] = 0xca;
    p.callback[1] = 0x11;
    p.selector = {1, 2, 3, 4};
    p.expiry = expiry;
    std::string j = marshal(p);
    tx.payload = Bytes(j.begin(), j.end());
    k.sign(tx);
    return tx;
}

inline Transaction fulfill_tx(const TestKey& k, const Id& request_id, const Id& result,
                              std::uint64_t nonce) {
    Transaction tx;
    tx.type = kTxFulfillDecrypt;
    tx.payer = k.addr;
    tx.subject = request_id;
    tx.gas_limit = kTestGas;
    tx.nonce = nonce;
    FulfillPayload p;
    p.result = result;
    std::string j = marshal(p);
    tx.payload = Bytes(j.begin(), j.end());
    k.sign(tx);
    return tx;
}

inline Transaction advance_tx(const TestKey& k, std::uint64_t epoch,
                              const std::vector<CommitteeMember>& committee,
                              std::int64_t threshold, const Bytes& pk, std::uint64_t nonce) {
    Transaction tx;
    tx.type = kTxAdvanceEpoch;
    tx.payer = k.addr;
    tx.subject = committee_digest(epoch, threshold, view(pk), committee);
    tx.gas_limit = kTestGas;
    tx.nonce = nonce;
    AdvancePayload p;
    p.epoch = epoch;
    p.committee = committee;
    p.committee_nil = committee.empty();
    p.threshold = threshold;
    p.public_key = pk;
    p.public_key_nil = pk.empty();
    std::string j = marshal(p);
    tx.payload = Bytes(j.begin(), j.end());
    k.sign(tx);
    return tx;
}

// accept_queued builds, verifies and accepts whatever is queued.
inline std::shared_ptr<Block> accept_queued(Chain& c, const std::string& what) {
    auto blk = c.vm->build_block();
    if (!blk) {
        std::printf("  FAIL  %s: could not build: %s\n", what.c_str(),
                    blk.error().message().c_str());
        ++g_fail;
        return nullptr;
    }
    auto ok = (*blk)->check();
    if (!ok) {
        std::printf("  FAIL  %s: could not verify: %s\n", what.c_str(),
                    ok.error().message().c_str());
        ++g_fail;
        return nullptr;
    }
    (*blk)->verify();
    ok = (*blk)->accept_block();
    if (!ok) {
        std::printf("  FAIL  %s: could not accept: %s\n", what.c_str(),
                    ok.error().message().c_str());
        ++g_fail;
        return nullptr;
    }
    return *blk;
}

// force_block builds a block directly from the given transactions, bypassing
// the mempool — the way a peer's proposal arrives. Its timestamp is the first
// one a proposer could legally choose: chain time never runs backwards, so a
// block on a tip the local clock has not caught up with still steps forward.
inline std::shared_ptr<Block> force_block(Chain& c, std::vector<Transaction> txs) {
    std::int64_t ts = c.vm->clock().now();
    auto parent = c.vm->get_block(Id(c.vm->last_accepted()));
    if (parent && ts <= (*parent)->timestamp()) ts = (*parent)->timestamp() + 1;
    return std::make_shared<Block>(c.vm.get(), Id(c.vm->last_accepted()), c.vm->height() + 1, ts,
                                   std::move(txs));
}

// accept_one submits, builds, verifies and accepts a single-transaction block.
inline std::shared_ptr<Block> accept_one(Chain& c, const Transaction& tx,
                                         const std::string& what) {
    auto id = c.vm->submit_tx(tx);
    if (!id) {
        std::printf("  FAIL  %s: could not submit: %s\n", what.c_str(),
                    id.error().message().c_str());
        ++g_fail;
        return nullptr;
    }
    return accept_queued(c, what);
}

}  // namespace lux::fhevm::test
