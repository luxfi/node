// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fixtures.hpp — the shared setup the ported Go tests use, rendered from
// chains/quantumvm/harness_test.go.
//
// testNetwork and testChain are the SAME on every VM a test boots, because that
// is the production shape: one chain, many nodes. Giving each test VM its own
// chain id varied the wrong field — every per-node value derived from the chain
// then differed by accident, and a bug that gave all nodes ONE identity could
// not be seen, because the test's chain ids were all different anyway.

#pragma once

#include "harness.hpp"

#include "lux/quantumvm/store.hpp"
#include "lux/quantumvm/transaction.hpp"
#include "lux/quantumvm/vm.hpp"

#include <atomic>
#include <cstring>
#include <memory>
#include <random>
#include <string>
#include <vector>

namespace qvmtest {

using namespace lux::quantumvm;

// The fixed wall clock every test VM starts at. Fixed rather than real so a
// block's timestamp is something the test states, not something the machine
// happens to have.
inline constexpr Seconds kChainTime = 1'700'000'000;
inline constexpr std::uint32_t kTestNetwork = 96369;
inline constexpr std::uint32_t kOtherNetwork = kTestNetwork + 1;

inline Id id_of_letters(const char* s) {
    Id id{};
    const std::size_t n = std::strlen(s);
    for (std::size_t i = 0; i < n && i < kIdLen; ++i) id[i] = static_cast<std::uint8_t>(s[i]);
    return id;
}

inline const Id kTestChain = id_of_letters("qchain");
inline const Id kOtherChain = id_of_letters("other");

// A distinct id per call, the way Go's ids.GenerateTestID() works.
inline Id random_id() {
    static thread_local std::mt19937_64 rng(0x51564d);  // "QVM"
    Id id{};
    for (std::size_t i = 0; i < kIdLen; i += 8) {
        const std::uint64_t v = rng();
        for (int b = 0; b < 8; ++b) id[i + static_cast<std::size_t>(b)] = static_cast<std::uint8_t>(v >> (8 * b));
    }
    return id;
}

inline std::string random_node_id() { return "node-" + hex(view(random_id())).substr(0, 20); }

inline config::Config default_config() { return config::default_config(); }

// Quantum stamping off, which makes a transaction's admission a matter of the
// pool alone. Tests about block structure use it so a signature expiring
// mid-test cannot turn a structural failure into a crypto one.
inline config::Config quiet_config() {
    config::Config cfg = config::default_config();
    cfg.quantum_stamp_enabled = false;
    cfg.corona_enabled = false;
    return cfg;
}

inline Bytes bytes_of(const std::string& s) {
    const auto* p = reinterpret_cast<const std::uint8_t*>(s.data());
    return Bytes(p, p + s.size());
}

// A VM started the way the node starts one — through initialize, over a real
// store — so a test can reopen the same bytes and see what actually survived.
struct Booted {
    std::unique_ptr<QuantumVM> vm;
    Status status;
    QuantumVM* operator->() const { return vm.get(); }
    QuantumVM& operator*() const { return *vm; }
};

inline Booted boot_vm_as(config::Config cfg, store::Store* db, std::uint32_t network_id,
                         const Id& chain_id) {
    Booted b;
    b.vm = std::make_unique<QuantumVM>(cfg);
    Init init;
    init.db = db;
    init.node_id = random_node_id();
    init.chain_id = chain_id;
    init.network_id = network_id;
    init.genesis = bytes_of("q-chain genesis");
    b.status = b.vm->initialize(init);
    if (b.status) b.vm->clock().set(from_seconds(kChainTime));
    return b;
}

inline Booted boot_vm_on(config::Config cfg, store::Store* db) {
    return boot_vm_as(cfg, db, kTestNetwork, kTestChain);
}

// A transaction carrying a signature that is present but meaningless. Enough to
// get past the pool's admission check, never enough to pass verification.
inline std::shared_ptr<BaseTransaction> stamped_tx(std::uint64_t nonce, const std::string& data) {
    auto tx = std::make_shared<BaseTransaction>(kChainTime, nonce, bytes_of(data));
    quantum::QuantumSignature sig;
    sig.signature = Bytes{1};
    sig.quantum_stamp = Bytes{1};
    tx->set_signature(std::move(sig));
    return tx;
}

// A transaction carrying a genuine ML-DSA signature over its own wire, produced
// by the VM's own signer — the shape selection expects.
inline std::shared_ptr<BaseTransaction> signed_tx(const QuantumVM& vm, std::uint64_t nonce,
                                                  const std::string& data) {
    auto tx = std::make_shared<BaseTransaction>(kChainTime, nonce, bytes_of(data));
    auto key = vm.signer().generate_key();
    if (!key) return tx;
    auto sig = vm.signer().sign(tx->bytes(), &*key);
    if (!sig) return tx;
    tx->set_signature(std::move(*sig));
    return tx;
}

// Assembles a block on a parent without going through the builder, so a test
// can state a field the builder would never produce.
inline BlockPtr block_on(QuantumVM& vm, const Block& parent, std::vector<TxPtr> txs) {
    wire::BlockFields f;
    f.timestamp = parent.timestamp();
    f.height = parent.height() + 1;
    f.parent_id = parent.id();
    f.chain_id = vm.chain_id();
    f.network_id = vm.network_id();
    f.transactions = std::move(txs);
    return std::make_shared<Block>(&vm, std::move(f));
}

inline BlockPtr block_on(QuantumVM& vm, const Block& parent) { return block_on(vm, parent, {}); }

inline BlockPtr block_on(QuantumVM& vm, const Block& parent, TxPtr tx) {
    std::vector<TxPtr> txs{std::move(tx)};
    return block_on(vm, parent, std::move(txs));
}

// A distinct transaction nonce per call, so two blocks built in one test never
// carry the same transaction — the pool refuses a duplicate, and a test that hit
// that would be measuring the pool rather than what it meant to.
inline std::uint64_t next_nonce() {
    static std::atomic<std::uint64_t> n{0};
    return ++n;
}

// A store whose batches will not write, which is what a full disk looks like
// from up here: staging succeeds, the commit does not. The refusal is
// switchable, because the interesting question is what the store holds AFTER it
// comes back.
class RefusingStore final : public store::Store {
  public:
    explicit RefusingStore(store::Store* base, bool* refuse) : base_(base), refuse_(refuse) {}

    Result<Bytes> get(ByteView key) const override { return base_->get(key); }
    Status write(const store::Batch& batch) override {
        if (*refuse_) return fail(Err::StoreUnwritable, "store refused the write");
        return base_->write(batch);
    }
    Status close() override { return base_->close(); }

  private:
    store::Store* base_;
    bool* refuse_;
};

// A store that holds everything and answers nothing: the shape of a disk that is
// failing rather than empty.
class UnreadableStore final : public store::Store {
  public:
    explicit UnreadableStore(store::Store* base) : base_(base) {}

    Result<Bytes> get(ByteView) const override {
        return fail(Err::StoreCorrupt, "store could not answer the read");
    }
    Status write(const store::Batch& batch) override { return base_->write(batch); }
    Status close() override { return base_->close(); }

  private:
    store::Store* base_;
};

// A transaction that records how many times it was applied, and can refuse.
class CountingTx final : public Transaction {
  public:
    CountingTx(std::shared_ptr<BaseTransaction> inner, int* runs, bool refuse)
        : inner_(std::move(inner)), runs_(runs), refuse_(refuse) {}

    Id id() const override { return inner_->id(); }
    ByteView bytes() const override { return inner_->bytes(); }
    Status verify() const override { return inner_->verify(); }
    Status execute() const override {
        ++*runs_;
        if (refuse_) return fail(Err::Execute, "state says no");
        return ok();
    }
    const quantum::QuantumSignature* signature() const override { return inner_->signature(); }
    Seconds timestamp() const override { return inner_->timestamp(); }
    std::uint64_t fee() const override { return inner_->fee(); }

  private:
    std::shared_ptr<BaseTransaction> inner_;
    int* runs_;
    bool refuse_;
};

inline Id tip_of(const QuantumVM& vm) {
    auto t = vm.tip();
    return t ? *t : kEmptyId;
}

inline std::uint64_t height_of(const QuantumVM& vm) {
    auto h = vm.tip_height();
    return h ? *h : 0;
}

}  // namespace qvmtest
