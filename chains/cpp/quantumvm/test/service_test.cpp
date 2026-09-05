// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// service_test.cpp — ported from Go chains/quantumvm/service_test.go and
// service_secret_test.go, at the projection rather than over HTTP: the Go tests
// drive a JSON-RPC server, and what they are ASSERTING is what each method
// answers about the chain.

#include "fixtures.hpp"

#include "lux/quantumvm/service.hpp"

using namespace qvmtest;
namespace svc = lux::quantumvm::service;

// A block reads back over the service as the block the chain holds.
TEST(GetBlockReportsTheBlock) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));
    auto built = vm->build_block();
    REQUIRE_OK(built);
    REQUIRE_OK((*built)->accept());

    auto reply = svc::get_block(*vm.vm, (*built)->id());
    REQUIRE_OK(reply);
    REQUIRE_EQ(text((*built)->id()), reply->block.id);
    REQUIRE_EQ(text((*built)->parent_id()), reply->block.parent_id);
    REQUIRE_EQ((*built)->height(), reply->block.height);
    REQUIRE_EQ((*built)->timestamp(), reply->timestamp);
    REQUIRE_EQ(std::size_t{1}, reply->tx_count);
    // Blocks carry no stamp: a per-block signature on the wire would make the id
    // depend on who signed.
    REQUIRE(!reply->quantum_sig);

    REQUIRE_FAILS(svc::get_block(*vm.vm, random_id()));
}

// The key the service hands out is a real ML-DSA key of the configured width,
// and it is refused outright when the chain is not configured to use one.
TEST(GenerateCoronaKeyIsGatedOnTheConfig) {
    store::Memory on_db, off_db;
    Booted on = boot_vm_on(config::default_config(), &on_db);  // corona ON
    REQUIRE_OK(on.status);

    auto key = svc::generate_corona_key(*on.vm);
    REQUIRE_OK(key);
    REQUIRE_EQ(on->signer().public_key_size(), key->key_size);
    REQUIRE_EQ(key->key_size * 2, key->public_key.size());  // rendered as hex
    REQUIRE_EQ(on->configuration().quantum_algorithm_version, key->version);

    Booted off = boot_vm_on(quiet_config(), &off_db);  // corona OFF
    REQUIRE_OK(off.status);
    REQUIRE_ERR(svc::generate_corona_key(*off.vm), Err::NotConfigured);
}

// The secret NEVER leaves the node. What the service reports is the public half
// and its width, and nothing in the reply is derived from the private key.
TEST(GenerateCoronaKeyDoesNotReportTheSecret) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);
    REQUIRE_OK(vm.status);

    auto reply = svc::generate_corona_key(*vm.vm);
    REQUIRE_OK(reply);
    // The reply is exactly the public key, in hex. A secret of the same width
    // would be twice as long and would not be this string.
    REQUIRE_EQ(vm->signer().public_key_size() * 2, reply->public_key.size());
    REQUIRE_MSG(reply->public_key.find_first_not_of("0123456789abcdef") == std::string::npos,
                "the reply is not the hex rendering it claims to be");
}

// A signature checks out over the message it signs and no other, and the arm is
// refused outright when the chain is not checking signatures at all.
TEST(VerifyQuantumSignatureChecksTheMessage) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);
    REQUIRE_OK(vm.status);

    const Bytes msg = bytes_of("a round digest");
    auto key = vm->signer().generate_key();
    REQUIRE_OK(key);
    auto sig = vm->signer().sign(view(msg), &*key);
    REQUIRE_OK(sig);

    auto good = svc::verify_quantum_signature(*vm.vm, view(msg), *sig);
    REQUIRE_OK(good);
    REQUIRE(good->valid);
    REQUIRE_EQ(vm->configuration().quantum_algorithm_version, good->algorithm);

    auto bad = svc::verify_quantum_signature(*vm.vm, view(bytes_of("another digest")), *sig);
    REQUIRE_OK(bad);
    REQUIRE_MSG(!bad->valid, "a signature was reported valid for a message it does not sign");

    store::Memory off_db;
    Booted off = boot_vm_on(quiet_config(), &off_db);
    REQUIRE_OK(off.status);
    REQUIRE_ERR(svc::verify_quantum_signature(*off.vm, view(msg), *sig), Err::NotConfigured);
}

// The pending list is what the pool holds, in arrival order, bounded.
TEST(PendingTransactionsReportsThePool) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    REQUIRE(svc::pending_transactions(*vm.vm, 0).transactions.empty());

    const auto first = stamped_tx(1, "one");
    const auto second = stamped_tx(2, "two");
    REQUIRE_OK(vm->pool().add(first));
    REQUIRE_OK(vm->pool().add(second));

    const svc::PendingReply all = svc::pending_transactions(*vm.vm, 0);
    REQUIRE_EQ(std::size_t{2}, all.count);
    REQUIRE_EQ(text(first->id()), all.transactions[0].id);
    REQUIRE_EQ(first->timestamp(), all.transactions[0].timestamp);
    REQUIRE_EQ(text(second->id()), all.transactions[1].id);

    const svc::PendingReply one = svc::pending_transactions(*vm.vm, 1);
    REQUIRE_EQ(std::size_t{1}, one.count);
}

// Health and config report what actually governs the chain — and no fee
// schedule, because Q-Chain charges none (LP-0130 §6).
TEST(HealthAndConfigReportWhatGovernsTheChain) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));

    const svc::HealthReply h = svc::health(*vm.vm);
    REQUIRE(h.healthy);
    REQUIRE_EQ(std::string(kVersion), h.version);
    REQUIRE(h.quantum_enabled);
    REQUIRE(h.corona_enabled);
    REQUIRE_EQ(std::size_t{1}, h.pending_tx_count);
    REQUIRE_EQ(vm->configuration().max_parallel_txs, h.parallel_workers);

    const svc::ConfigReply c = svc::configuration(*vm.vm);
    REQUIRE_EQ(vm->configuration().max_parallel_txs, c.max_parallel_txs);
    REQUIRE_EQ(vm->configuration().quantum_algorithm_version, c.quantum_algorithm_version);
    REQUIRE(c.quantum_stamp_enabled);
    REQUIRE(c.corona_enabled);
    REQUIRE_EQ(vm->configuration().parallel_batch_size, c.parallel_batch_size);

    REQUIRE_OK(vm->shutdown());
    REQUIRE_MSG(!svc::health(*vm.vm).healthy, "a shut-down chain reported itself healthy");
}
