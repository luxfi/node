// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/service.hpp"

namespace lux::quantumvm::service {

Result<BlockReply> get_block(const QuantumVM& vm, const Id& block_id) {
    auto blk = vm.block(block_id);
    if (!blk) return std::unexpected(blk.error());

    BlockReply r;
    r.block.id = text((*blk)->id());
    r.block.parent_id = text((*blk)->parent_id());
    r.block.height = (*blk)->height();
    r.height = (*blk)->height();
    r.timestamp = (*blk)->timestamp();
    r.tx_count = (*blk)->transactions().size();
    r.quantum_sig = false;
    return r;
}

Result<KeyReply> generate_corona_key(const QuantumVM& vm) {
    if (!vm.configuration().corona_enabled)
        return fail(Err::NotConfigured, "corona keys are not enabled");

    auto key = vm.signer().generate_key();
    if (!key) return std::unexpected(key.error());

    KeyReply r;
    r.public_key = hex(view(key->public_key));
    r.version = key->version;
    r.key_size = key->public_key.size();
    return r;
}

Result<VerifyReply> verify_quantum_signature(const QuantumVM& vm, ByteView message,
                                             const quantum::QuantumSignature& sig) {
    if (!vm.configuration().quantum_stamp_enabled)
        return fail(Err::NotConfigured, "quantum signatures are not enabled");

    VerifyReply r;
    r.valid = vm.signer().verify(message, &sig).has_value();
    r.algorithm = sig.algorithm;
    return r;
}

PendingReply pending_transactions(const QuantumVM& vm, std::size_t limit) {
    if (limit == 0 || limit > 100) limit = 100;

    PendingReply r;
    for (const auto& tx : vm.pool().pending(limit))
        r.transactions.push_back(TransactionSummary{text(tx->id()), tx->timestamp()});
    r.count = r.transactions.size();
    return r;
}

HealthReply health(const QuantumVM& vm) {
    const Health h = vm.health();
    HealthReply r;
    r.healthy = h.healthy;
    r.version = h.version;
    r.quantum_enabled = vm.configuration().quantum_stamp_enabled;
    r.corona_enabled = vm.configuration().corona_enabled;
    r.pending_tx_count = vm.pool().count();
    r.parallel_workers = vm.configuration().max_parallel_txs;
    return r;
}

ConfigReply configuration(const QuantumVM& vm) {
    const config::Config& c = vm.configuration();
    ConfigReply r;
    r.max_parallel_txs = c.max_parallel_txs;
    r.quantum_algorithm_version = c.quantum_algorithm_version;
    r.quantum_stamp_enabled = c.quantum_stamp_enabled;
    r.corona_enabled = c.corona_enabled;
    r.parallel_batch_size = c.parallel_batch_size;
    return r;
}

}  // namespace lux::quantumvm::service
