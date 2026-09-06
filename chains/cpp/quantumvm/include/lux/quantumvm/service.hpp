// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// service.hpp — what the chain answers when it is asked about itself.
//
// Rendered from Go chains/quantumvm/service.go, which is a JSON-RPC service
// registered under `quantumvm`. The TRANSPORT is deliberately not here: the
// node's own rpc.hpp owns routing, codecs and the lock a method runs under, and
// a chain that shipped a second HTTP server would be a second place to get all
// of that wrong. What is here is every answer the service gives, at real types
// — so the projection is testable, and mounting it is a matter of naming these
// functions in a method table.

#pragma once

#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/vm.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace lux::quantumvm::service {

struct BlockSummary {
    std::string id;
    std::string parent_id;
    std::uint64_t height = 0;
};

struct BlockReply {
    BlockSummary block;
    std::uint64_t height = 0;
    Seconds timestamp = 0;
    std::size_t tx_count = 0;
    // Blocks carry no stamp; see QuantumVM::sign_block_with_quasar for why a
    // per-block signature cannot live on the wire.
    bool quantum_sig = false;
};

struct KeyReply {
    std::string public_key;  // hex
    std::uint32_t version = 0;
    std::size_t key_size = 0;
};

struct VerifyReply {
    bool valid = false;
    std::uint32_t algorithm = 0;
};

struct TransactionSummary {
    std::string id;
    Seconds timestamp = 0;
};

struct PendingReply {
    std::vector<TransactionSummary> transactions;
    std::size_t count = 0;
};

struct HealthReply {
    bool healthy = false;
    std::string version;
    bool quantum_enabled = false;
    bool corona_enabled = false;
    std::size_t pending_tx_count = 0;
    int parallel_workers = 0;
};

struct ConfigReply {
    int max_parallel_txs = 0;
    std::uint32_t quantum_algorithm_version = 0;
    bool quantum_stamp_enabled = false;
    bool corona_enabled = false;
    int parallel_batch_size = 0;
};

Result<BlockReply> get_block(const QuantumVM& vm, const Id& block_id);
Result<KeyReply> generate_corona_key(const QuantumVM& vm);
Result<VerifyReply> verify_quantum_signature(const QuantumVM& vm, ByteView message,
                                             const quantum::QuantumSignature& sig);
PendingReply pending_transactions(const QuantumVM& vm, std::size_t limit);
HealthReply health(const QuantumVM& vm);
ConfigReply configuration(const QuantumVM& vm);

}  // namespace lux::quantumvm::service
