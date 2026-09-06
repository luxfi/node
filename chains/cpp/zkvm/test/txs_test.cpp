// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_test.cpp — the identity a transaction is decided by, and the shape check
// in front of it.
//
// Consensus decides between blocks by id and the pool keys on it, so any field a
// transaction can be changed in WITHOUT changing its id is a field an attacker
// can change for free.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/txs.hpp"

#include <functional>
#include <vector>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

void id_commits_to_everything_the_transaction_means() {
    std::printf("the id commits to everything the transaction means\n");

    struct Case {
        const char* name;
        std::function<void(Transaction&)> alter;
    };
    const std::vector<Case> cases = {
        {"type", [](Transaction& t) { t.type = TxType::Unshield; }},
        {"version", [](Transaction& t) { t.version = 2; }},
        {"fee", [](Transaction& t) { t.fee = 8; }},
        {"expiry", [](Transaction& t) { t.expiry = 1; }},
        {"memo", [](Transaction& t) { t.memo = b("m"); }},
        {"nullifier", [](Transaction& t) { t.nullifiers = {b("m")}; }},
        {"commitment", [](Transaction& t) { t.outputs[0].commitment = b("d"); }},
        {"encrypted note", [](Transaction& t) { t.outputs[0].encrypted_note = b("e"); }},
        {"ephemeral key", [](Transaction& t) { t.outputs[0].ephemeral_pubkey = b("k"); }},
        {"output proof", [](Transaction& t) { t.outputs[0].output_proof = b("r"); }},
        {"proof type", [](Transaction& t) { t.proof->proof_type = "plonk"; }},
        {"proof bytes", [](Transaction& t) { t.proof->proof_data = b("q"); }},
        {"public inputs", [](Transaction& t) { t.proof->public_inputs = {b("i")}; }},
        {"payer", [](Transaction& t) { t.transparent_inputs[0].address = b("other"); }},
        {"input amount", [](Transaction& t) { t.transparent_inputs[0].amount = 999; }},
        {"input source", [](Transaction& t) { t.transparent_inputs[0].tx_id = id_of(0xAB); }},
        {"input index", [](Transaction& t) { t.transparent_inputs[0].output_idx = 1; }},
        {"payee", [](Transaction& t) { t.transparent_outputs[0].address = b("thief"); }},
        {"output amount", [](Transaction& t) { t.transparent_outputs[0].amount = 999; }},
        {"asset", [](Transaction& t) { t.transparent_outputs[0].asset_id = id_of(0xCD); }},
        {"dropped input", [](Transaction& t) { t.transparent_inputs.clear(); }},
        {"dropped proof", [](Transaction& t) { t.proof.reset(); }},
    };

    for (const auto& c : cases) {
        Transaction altered = shield_tx();
        c.alter(altered);
        check(shield_tx().compute_id() != altered.compute_id(),
              std::string("changing the ") + c.name + " changes the id");
    }
}

void the_id_cannot_be_forged_by_moving_a_byte() {
    std::printf("\nno byte can move between fields without the id noticing\n");

    // Variable-length fields written one after another with no length between
    // them let a byte move from the end of one to the start of the next:
    // ["ab","c"] and ["a","bc"] are the same bytes. An attacker who can produce
    // two transactions with one id chooses which one the network keeps after the
    // other has been accepted.
    Transaction split = shield_tx();
    split.nullifiers = {b("ab"), b("c")};
    Transaction moved = shield_tx();
    moved.nullifiers = {b("a"), b("bc")};
    check(split.compute_id() != moved.compute_id(),
          "two different nullifier sets do not hash the same");

    // The same across a field boundary rather than within one list.
    Transaction longer = shield_tx();
    longer.outputs = {{b("cd"), {}, {}, {}}};
    Transaction shorter = shield_tx();
    shorter.outputs = {{b("c"), b("d"), {}, {}}};
    check(longer.compute_id() != shorter.compute_id(),
          "a byte moved from the commitment into the note changes the id");
}

void the_id_is_stable() {
    std::printf("\nthe identity is stable\n");
    check(shield_tx().compute_id() == shield_tx().compute_id(),
          "asking twice gives the same answer");
}

void validate_basic_refusals() {
    std::printf("\nthe shape check, refusal by refusal\n");

    // A transfer with everything it needs is the positive control.
    Transaction ok = shielded_transfer(1, 1);
    check_ok(ok.validate_basic(), "a complete transfer passes");

    Transaction bad_type = ok;
    bad_type.type = static_cast<TxType>(9);
    check_err(bad_type.validate_basic(), kErrInvalidTxType, "a type out of range is refused");

    Transaction no_inputs = ok;
    no_inputs.nullifiers.clear();
    check_err(no_inputs.validate_basic(), kErrNoInputs, "spending nothing is refused");

    Transaction no_outputs = ok;
    no_outputs.outputs.clear();
    check_err(no_outputs.validate_basic(), kErrNoOutputs, "creating nothing is refused");

    Transaction no_proof = ok;
    no_proof.proof.reset();
    check_err(no_proof.validate_basic(), kErrMissingProof, "a missing proof is refused");

    // A transaction that names no expiry sits in a bounded pool forever: it can
    // never enter a block, nothing evicts it, and once the pool is full of them
    // every honest arrival paying the same floor is refused.
    Transaction no_expiry = ok;
    no_expiry.expiry = 0;
    check_err(no_expiry.validate_basic(), kErrNoExpiry, "naming no expiry is refused");

    Transaction transfer_without_shielded = ok;
    transfer_without_shielded.nullifiers.clear();
    transfer_without_shielded.transparent_inputs = {{id_of(1), 0, 1, b("a")}};
    check_err(transfer_without_shielded.validate_basic(), kErrInvalidTransfer,
              "a transfer with no shielded input is refused");

    Transaction shield = shield_tx();
    shield.expiry = 10;
    check_ok(shield.validate_basic(), "a complete shield passes");
    Transaction shield_no_transparent = shield;
    shield_no_transparent.transparent_inputs.clear();
    check_err(shield_no_transparent.validate_basic(), kErrInvalidShield,
              "a shield with no transparent input is refused");

    Transaction unshield = shield;
    unshield.type = TxType::Unshield;
    unshield.transparent_outputs.clear();
    check_err(unshield.validate_basic(), kErrInvalidUnshield,
              "an unshield with no transparent output is refused");
}

void the_circuit_key_is_the_type_byte() {
    std::printf("\na circuit is keyed by the type BYTE, as the reference keys it\n");
    check(circuit_key(TxType::Transfer) == std::string(1, '\0'), "transfer is 0x00");
    check(circuit_key(TxType::Shield) == std::string(1, '\x03'), "shield is 0x03");
    check(circuit_key(TxType::Unshield) == std::string(1, '\x04'), "unshield is 0x04");
    check(circuit_key(TxType::Transfer) != circuit_key(TxType::Shield),
          "and two circuits never share a key");
}

}  // namespace

int main() {
    id_commits_to_everything_the_transaction_means();
    the_id_cannot_be_forged_by_moving_a_byte();
    the_id_is_stable();
    validate_basic_refusals();
    the_circuit_key_is_the_type_byte();
    return report("txs");
}
