// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// service.hpp — the F-Chain's read surface.
//
// Mutating operations are submitted as CLIENT-SIGNED transactions and take
// effect only through fee-settled consensus blocks. Everything here is a
// read-only query of PUBLIC state. There is no call that decrypts, none that
// returns a ciphertext body, and no "fee" integer in any request — a fee is
// never an unbacked number a caller writes down, it is gas metered and burned
// from the payer's on-chain balance inside consensus.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/vm.hpp"

#include <string>
#include <vector>

namespace lux::fhevm {

// CiphertextView is the PUBLIC view of a registered encrypted value. It carries
// the digest of the ciphertext body so a client can check a body it fetched
// from off-chain storage; it does not carry the body, because F never has it.
struct CiphertextView {
    std::string handle;
    std::string owner;
    std::string scheme;
    std::string digest;
    std::uint8_t type = 0;
    std::int64_t level = 0;
    std::uint64_t epoch = 0;
    std::uint32_t size = 0;
    std::int64_t registered_at = 0;
    std::string chain_id;
};

// PermitView is the PUBLIC view of a capability grant.
struct PermitView {
    std::string permit_id;
    std::string handle;
    std::string grantor;
    std::string grantee;
    std::uint32_t operations = 0;
    std::int64_t expiry = 0;
    std::string status;
    std::int64_t created_at = 0;
    std::string chain_id;
};

struct AttestationView {
    std::string member;
    std::string value;
};

// DecryptView is the PUBLIC view of a threshold-decryption request. It has no
// plaintext field: the plaintext goes to the requester's callback off-chain,
// and F records only the handle the committee agreed the decryption produced.
struct DecryptView {
    std::string request_id;
    std::string handle;
    std::string requester;
    std::string permit_id;
    std::string callback;
    std::string selector;
    std::uint64_t epoch = 0;
    std::string status;
    std::string result_handle;  // empty unless the request completed
    std::vector<AttestationView> attestations;
    std::int64_t threshold = 0;
    std::int64_t expiry = 0;
    std::int64_t created_at = 0;
    std::int64_t completed_at = 0;
    std::string source_chain;
};

// CommitteeMemberView names one seat as a client reads it.
struct CommitteeMemberView {
    std::string node_id;
    std::string public_key;
    std::uint64_t weight = 0;
    std::int64_t index = 0;
};

struct CommitteeView {
    std::uint64_t epoch = 0;
    std::int64_t threshold = 0;
    std::vector<CommitteeMemberView> members;
};

// FeeScheduleEntry prices one (operation, scheme) pair.
struct FeeScheduleEntry {
    std::string operation;
    std::string scheme;
    std::uint64_t gas = 0;
    std::uint64_t fee_nlux = 0;
};

struct FeeSchedule {
    std::uint64_t gas_price = 0;
    std::vector<FeeScheduleEntry> entries;
};

class Service {
public:
    explicit Service(VM& vm) : vm_(&vm) {}

    // submit_transaction parses, authenticates, admission-checks and enqueues a
    // signed transaction. The client builds and signs it offline with its own
    // ML-DSA-65 key; F only verifies the signature and settles the fee.
    Result<Id> submit_transaction(ByteView raw) const;

    Result<CiphertextView> ciphertext(std::string_view handle_hex) const;
    // ciphertexts lists registrations, optionally filtered by owner and scheme.
    // An empty filter matches everything; a filter that does not DECODE is a
    // refusal, not an empty listing — an empty listing reads as "this owner has
    // nothing", which is a different and wrong answer.
    Result<std::vector<CiphertextView>> ciphertexts(std::string_view owner_hex,
                                                    std::string_view scheme) const;
    Result<PermitView> permit(std::string_view permit_id_hex) const;
    Result<DecryptView> decrypt(std::string_view request_id_hex) const;
    Result<CommitteeView> committee(std::uint64_t epoch) const;
    CommitteeView current_committee() const;

    // balance answers with the account's spendable nLUX and the chain's burned
    // supply, which is the pair an operator actually reads together.
    struct Balance {
        std::uint64_t balance_nlux = 0;
        std::uint64_t burned_nlux = 0;
    };
    Result<Balance> balance(std::string_view address_hex) const;

    // health reports what consensus actually did — and refuses rather than
    // answering zero when it cannot read the ledger.
    Result<HealthReport> health() const;

    // PublicParams are the FHE parameters this network encrypts under, together
    // with the seated epoch's threshold and network public key.
    //
    // log_n, log_qp and log_scale come from the FHE RUNTIME's threshold
    // configuration, not from this chain: F coordinates confidential compute
    // and does not perform it, so it reports the runtime's numbers rather than
    // deriving its own. The Go chain reads them from the live runtime
    // (fhe.DefaultThresholdConfig); this port states them, because the runtime
    // itself has no C++ port yet. That is a real seam and it is written down
    // here rather than hidden: when the runtime is ported, these read from it.
    struct PublicParams {
        std::uint64_t epoch = 0;
        std::int64_t log_n = 0;
        std::int64_t log_qp = 0;
        std::int64_t log_scale = 0;
        std::int64_t threshold = 0;
        std::string public_key;
        std::string chain_id;
    };
    PublicParams public_params() const;

    // fee_schedule is the per-operation, per-scheme schedule, so a client can
    // compute the exact burn before submitting a transaction.
    FeeSchedule fee_schedule() const;

    // operation_name is the one place an operation's public name is written
    // down.
    static std::string_view operation_name(std::uint8_t type);

private:
    VM* vm_;
};

// account_from_hex parses a 20-byte hex address.
Result<Account> account_from_hex(std::string_view s);
// hash32 decodes a hex-encoded 32-byte identifier — a handle, a permit id, a
// request id. One decoder for all three, so none of them can drift into
// accepting a length the others reject.
Result<Id> hash32(std::string_view s);

}  // namespace lux::fhevm
