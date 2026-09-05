// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// differential_test.cpp — the load-bearing test of the whole port.
//
// Every constant in golden.hpp came out of the GO F-Chain (chains/fhevm, via
// test/golden_gen_test.go). This file builds the SAME value in C++ and compares
// it. If a byte moves, the two implementations have forked the chain, and that
// is what fails here rather than in production.
//
// Both directions, everywhere it is possible: C++ writes what Go writes, AND
// C++ reads what Go wrote back into the same values. The signatures are the one
// place only one direction exists — ML-DSA's signing is randomized, so a C++
// signature is not expected to equal a Go one — and that is exactly why the
// GO-produced signature is verified here: it proves the two agree on FIPS 204's
// pure variant with an empty context, rather than assuming it.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/fhevm/auth.hpp"
#include "lux/fhevm/service.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

Id golden_chain() { return id_of(fhevm_golden::kChainId); }

Bytes bytes_of(const std::string& hex_str) { return unhex(hex_str); }

std::vector<CommitteeMember> golden_committee() {
    // The committee travels inside the epoch-advance payload, so the payload Go
    // wrote is the authority on what it contains: read it back rather than
    // building a second copy here that could quietly differ.
    auto p = decode_advance(view(std::string_view(fhevm_golden::kAdvancePayload)));
    if (!p) return {};
    return p->committee;
}

// round_trip_tx checks that Go's wire bytes parse here into a transaction whose
// own re-serialization is byte-identical, and that its id and effect match.
void round_trip_tx(const char* name, const char* wire_hex, const char* id_hex,
                   const char* effect_hex, const char* content_hex, const char* signing_hex,
                   std::uint64_t gas, std::uint64_t fee_nlux) {
    Bytes wire = bytes_of(wire_hex);
    auto tx = parse_transaction(view(wire));
    if (!tx) {
        std::printf("  FAIL  %s: Go's bytes do not parse (%s)\n", name,
                    tx.error().message().c_str());
        ++g_fail;
        return;
    }
    check_eq(hex_of(tx->bytes()), wire_hex, std::string(name) + ": wire bytes round-trip");
    check_eq(hex_of(tx->content()), content_hex, std::string(name) + ": signing object");
    check_eq(hex_of(tx->signing_bytes(golden_chain())), signing_hex,
             std::string(name) + ": preimage bound to the chain");
    check_eq(hex_of(tx->id()), id_hex, std::string(name) + ": id");
    check_eq(hex_of(tx->effect()), effect_hex, std::string(name) + ": effect");

    auto g = gas_for(*tx);
    check_eq(g ? *g : 0, gas, std::string(name) + ": metered gas");
    auto f = fee_for(*tx);
    check_eq(f ? *f : 0, fee_nlux, std::string(name) + ": settled fee");

    // The signature Go produced verifies here, over the preimage this side
    // derives — which is the whole cross-language authentication claim.
    accepted(tx->authenticate(golden_chain()), std::string(name) + ": Go's signature verifies");
    // And it does NOT verify for another chain, because the chain id is bound
    // into the preimage rather than carried by the transaction.
    refused(tx->authenticate(id_of(fhevm_golden::kForeignChainId)), Err::BadSignature,
            std::string(name) + ": refused on another chain");
    accepted(tx->syntactic_verify(), std::string(name) + ": syntactically valid");
}

}  // namespace

int main() {
    std::printf("fhevm — every value the Go F-Chain produced, reproduced here\n\n");

    // ---- identities: the address derivation is the chain's own -------------
    check_eq(hex_of(address_of(view(bytes_of(fhevm_golden::kPayerPub)))),
             fhevm_golden::kPayerAddr, "address_of(payer key)");
    check_eq(hex_of(address_of(view(bytes_of(fhevm_golden::kGranteePub)))),
             fhevm_golden::kGranteeAddr, "address_of(grantee key)");
    check_eq(hex_of(address_of(view(bytes_of(fhevm_golden::kM0Pub)))), fhevm_golden::kM0Addr,
             "address_of(member 0 key)");

    // ---- the renderings a record persists ----------------------------------
    {
        NodeId n{};
        Bytes b = bytes_of(fhevm_golden::kMember0NodeId);
        std::copy(b.begin(), b.end(), n.begin());
        check_eq(node_id_string(n), fhevm_golden::kMember0NodeIdString, "NodeID renders as Go's");
        NodeId back{};
        check(node_id_from_string(fhevm_golden::kMember0NodeIdString, &back) && back == n,
              "and reads back");
    }

    // ---- content derivations ------------------------------------------------
    Id digest_a = id_of(fhevm_golden::kDigestA);
    check_eq(hex_of(digest_a), hex_of(sha256(view(std::string_view("a")))),
             "the digest under test is sha256(\"a\")");
    check_eq(hex_of(derive_handle(digest_a, "")), fhevm_golden::kHandleA_NoScheme,
             "derive_handle, no scheme");
    check_eq(hex_of(derive_handle(digest_a, "ckks-n14")), fhevm_golden::kHandleA_ckks_n14,
             "derive_handle, ckks-n14");
    check_eq(hex_of(derive_handle(digest_a, "tfhe-n10")), fhevm_golden::kHandleA_tfhe_n10,
             "derive_handle, tfhe-n10");

    Account payer = account_of(fhevm_golden::kPayerAddr);
    Account grantee = account_of(fhevm_golden::kGranteeAddr);
    Id handle = id_of(fhevm_golden::kHandleA_ckks_n14);
    check_eq(hex_of(derive_permit_id(handle, payer, grantee, 1, 0, 1)), fhevm_golden::kPermitIdA,
             "derive_permit_id");
    check_eq(hex_of(derive_permit_id(handle, payer, grantee, 5, 1234567, 9)),
             fhevm_golden::kPermitIdB, "derive_permit_id, other arguments");
    check_eq(hex_of(derive_request_id(handle, grantee, 2)), fhevm_golden::kRequestIdA,
             "derive_request_id");

    // ---- the committee digest, over the members Go proposed -----------------
    {
        std::vector<CommitteeMember> c = golden_committee();
        check(c.size() == 3, "the golden epoch proposal carries three members");
        Bytes pk = bytes_of(fhevm_golden::kNetworkPublicKey);
        check_eq(hex_of(committee_digest(0, 2, view(pk), c)),
                 fhevm_golden::kCommitteeDigestEpoch0T2, "committee_digest, epoch 0");
        check_eq(hex_of(committee_digest(1, 2, view(pk), c)),
                 fhevm_golden::kCommitteeDigestEpoch1T2, "committee_digest, epoch 1");
        check_eq(hex_of(committee_digest(7, 3, ByteView{}, {})),
                 fhevm_golden::kCommitteeDigestEmpty, "committee_digest, empty");
        accepted(validate_committee(c, 2, view(pk)), "the golden committee is installable");
    }

    // ---- payload encodings: written here exactly as Go writes them ----------
    {
        RegisterPayload p;
        p.digest = digest_a;
        p.type = 4;
        p.level = 3;
        p.size = 4096;
        check_eq(marshal(p), fhevm_golden::kRegisterPayload, "register payload");
        auto back = decode_register(view(std::string_view(fhevm_golden::kRegisterPayload)));
        check(back && back->digest == p.digest && back->type == 4 && back->level == 3 &&
                  back->size == 4096,
              "register payload reads back");
    }
    {
        GrantPayload p;
        p.grantee = grantee;
        p.operations = kPermitOpDecrypt;
        p.expiry = 0;
        check_eq(marshal(p), fhevm_golden::kGrantPayload, "grant payload");
        auto back = decode_grant(view(std::string_view(fhevm_golden::kGrantPayload)));
        check(back && back->grantee == grantee && back->operations == kPermitOpDecrypt,
              "grant payload reads back — including the cb58 account");
    }
    {
        RevokePayload p;
        p.reason = "no longer sanctioned";
        check_eq(marshal(p), fhevm_golden::kRevokePayload, "revoke payload");
    }
    {
        RequestPayload p;
        p.permit_id = id_of(fhevm_golden::kPermitIdA);
        p.callback[0] = 0xca;
        p.callback[1] = 0x11;
        p.selector = {1, 2, 3, 4};
        check_eq(marshal(p), fhevm_golden::kRequestPayload, "request payload");
        auto back = decode_request(view(std::string_view(fhevm_golden::kRequestPayload)));
        check(back && back->permit_id == p.permit_id && back->callback == p.callback &&
                  back->selector == p.selector,
              "request payload reads back");
    }
    {
        FulfillPayload p;
        p.result = id_of(fhevm_golden::kResultHandle);
        check_eq(marshal(p), fhevm_golden::kFulfillPayload, "fulfill payload");
    }
    {
        AdvancePayload p;
        p.epoch = 1;
        p.committee = golden_committee();
        p.committee_nil = false;
        p.threshold = 2;
        p.public_key = bytes_of(fhevm_golden::kNetworkPublicKey);
        p.public_key_nil = false;
        check_eq(marshal(p), fhevm_golden::kAdvancePayload,
                 "advance payload — base64 key, NodeID words and all");
    }

    // ---- the six transactions, whole ----------------------------------------
    round_trip_tx("register", fhevm_golden::kTxRegisterBytes, fhevm_golden::kTxRegisterId,
                  fhevm_golden::kTxRegisterEffect, fhevm_golden::kTxRegisterContent,
                  fhevm_golden::kTxRegisterSigning, fhevm_golden::kTxRegisterGas,
                  fhevm_golden::kTxRegisterFee);
    round_trip_tx("grant", fhevm_golden::kTxGrantBytes, fhevm_golden::kTxGrantId,
                  fhevm_golden::kTxGrantEffect, fhevm_golden::kTxGrantContent,
                  fhevm_golden::kTxGrantSigning, fhevm_golden::kTxGrantGas,
                  fhevm_golden::kTxGrantFee);
    round_trip_tx("revoke", fhevm_golden::kTxRevokeBytes, fhevm_golden::kTxRevokeId,
                  fhevm_golden::kTxRevokeEffect, fhevm_golden::kTxRevokeContent,
                  fhevm_golden::kTxRevokeSigning, fhevm_golden::kTxRevokeGas,
                  fhevm_golden::kTxRevokeFee);
    round_trip_tx("request", fhevm_golden::kTxRequestBytes, fhevm_golden::kTxRequestId,
                  fhevm_golden::kTxRequestEffect, fhevm_golden::kTxRequestContent,
                  fhevm_golden::kTxRequestSigning, fhevm_golden::kTxRequestGas,
                  fhevm_golden::kTxRequestFee);
    round_trip_tx("fulfill", fhevm_golden::kTxFulfillBytes, fhevm_golden::kTxFulfillId,
                  fhevm_golden::kTxFulfillEffect, fhevm_golden::kTxFulfillContent,
                  fhevm_golden::kTxFulfillSigning, fhevm_golden::kTxFulfillGas,
                  fhevm_golden::kTxFulfillFee);
    round_trip_tx("advance", fhevm_golden::kTxAdvanceBytes, fhevm_golden::kTxAdvanceId,
                  fhevm_golden::kTxAdvanceEffect, fhevm_golden::kTxAdvanceContent,
                  fhevm_golden::kTxAdvanceSigning, fhevm_golden::kTxAdvanceGas,
                  fhevm_golden::kTxAdvanceFee);

    // A transaction Go signed for ANOTHER chain must not authenticate here.
    {
        Bytes wire = bytes_of(fhevm_golden::kTxForeignBytes);
        auto tx = parse_transaction(view(wire));
        accepted(tx, "a foreign-chain transaction still parses");
        if (tx) {
            refused(tx->authenticate(golden_chain()), Err::BadSignature,
                    "a transaction signed for another chain is refused here");
            accepted(tx->authenticate(id_of(fhevm_golden::kForeignChainId)),
                     "and accepted on the chain it was signed for");
        }
    }

    // The request id the golden request transaction creates.
    check_eq(hex_of(derive_request_id(handle, grantee, 1)),
             fhevm_golden::kRequestIdOfRequestTx, "the request the golden decrypt ask creates");

    // ---- a block over two of those transactions -----------------------------
    {
        Bytes wire = bytes_of(fhevm_golden::kBlockBytes);
        auto h = parse_block_bytes(view(wire));
        accepted(h, "Go's block parses");
        if (h) {
            check_eq(hex_of(h->parent), fhevm_golden::kBlockParent, "block parent");
            check_eq(h->height, fhevm_golden::kBlockHeight, "block height");
            check_eq(std::uint64_t(h->timestamp), std::uint64_t(fhevm_golden::kBlockTime),
                     "block timestamp");
            check(h->transactions.size() == 2, "block carries two transactions");
            check_eq(hex_of(block_bytes(h->parent, h->height, h->timestamp, h->transactions)),
                     fhevm_golden::kBlockBytes, "block bytes round-trip");

            Memory store;
            VM vm(&store, VM::Config{96369, golden_chain(), "F"});
            auto blk = std::make_shared<Block>(&vm, h->parent, h->height, h->timestamp,
                                               h->transactions);
            check_eq(hex_of(blk->compute_id()), fhevm_golden::kBlockId,
                     "block id, over the chain it belongs to");
        }
        check_eq(empty_block_size(), std::uint64_t(fhevm_golden::kEmptyBlockSize),
                 "an empty block weighs what Go's does");
        check_eq(kTxEntry, std::uint64_t(fhevm_golden::kTxEntry),
                 "and one transaction costs the same beyond its own bytes");
    }

    // ---- the gas schedule ----------------------------------------------------
    check_eq(kGasPrice, fhevm_golden::kGasPrice, "gas price");
    check_eq(kGasPerByte, fhevm_golden::kGasPerByte, "gas per byte");
    check_eq(min_scheduled_fee(), fhevm_golden::kMinScheduledFee, "cheapest scheduled fee");
    {
        std::vector<std::string_view> got = schemes();
        std::size_t want_n = sizeof(fhevm_golden::kSchemes) / sizeof(fhevm_golden::kSchemes[0]);
        check(got.size() == want_n, "the schedule prices the same schemes Go prices");
        for (std::size_t i = 0; i < got.size() && i < want_n; ++i) {
            check_eq(std::string(got[i]), fhevm_golden::kSchemes[i], "scheme listing");
        }
    }
    {
        int mismatches = 0;
        for (const auto& c : fhevm_golden::kGasCases) {
            Transaction tx;
            tx.type = c.type;
            tx.scheme = c.scheme;
            tx.payload.assign(std::size_t(c.payload_len), 0x20);
            auto g = gas_for(tx);
            auto f = fee_for(tx);
            if (!g || *g != c.gas || !f || *f != c.fee) {
                std::printf("        type=%u scheme=%s len=%d got gas=%llu fee=%llu want %llu/%llu\n",
                            unsigned(c.type), c.scheme, c.payload_len,
                            (unsigned long long)(g ? *g : 0), (unsigned long long)(f ? *f : 0),
                            (unsigned long long)c.gas, (unsigned long long)c.fee);
                ++mismatches;
            }
        }
        check(mismatches == 0, "every (operation, scheme, payload length) prices as Go prices it");
    }

    // ---- the four persisted records -------------------------------------------
    {
        CiphertextRecord r;
        r.handle = handle;
        r.owner = payer;
        r.type = 4;
        r.level = 3;
        r.epoch = 0;
        r.registered_at = fhevm_golden::kGenesisTime;
        r.size = 4096;
        r.chain_id = golden_chain();
        r.scheme = "ckks-n14";
        r.digest = digest_a;
        check_eq(marshal(r), fhevm_golden::kCiphertextRecordJSON, "ciphertext record");
        CiphertextRecord back;
        std::string err;
        check(unmarshal(fhevm_golden::kCiphertextRecordJSON, &back, &err) && back == r,
              "ciphertext record reads back identically");
    }
    {
        PermitRecord r;
        r.permit_id = id_of(fhevm_golden::kPermitIdA);
        r.handle = handle;
        r.grantee = grantee;
        r.grantor = payer;
        r.operations = kPermitOpDecrypt;
        r.expiry = 0;
        r.created_at = fhevm_golden::kGenesisTime;
        r.chain_id = golden_chain();
        r.status = std::string(kStatusActive);
        check_eq(marshal(r), fhevm_golden::kPermitRecordJSON, "permit record");
        PermitRecord back;
        std::string err;
        check(unmarshal(fhevm_golden::kPermitRecordJSON, &back, &err) && back == r,
              "permit record reads back identically");
    }
    {
        DecryptRecord r;
        r.request_id = id_of(fhevm_golden::kRequestIdOfRequestTx);
        r.ciphertext_handle = handle;
        r.requester = grantee;
        r.callback[0] = 0xca;
        r.callback[1] = 0x11;
        r.callback_selector = {1, 2, 3, 4};
        r.source_chain = golden_chain();
        r.epoch = 0;
        r.nonce = 1;
        r.expiry = fhevm_golden::kGenesisTime + kDefaultRequestWindow;
        r.status = RequestStatus::Pending;
        r.created_at = fhevm_golden::kGenesisTime;
        r.permit_id = id_of(fhevm_golden::kPermitIdA);
        r.attestations.push_back(
            Attestation{account_of(fhevm_golden::kM0Addr), id_of(fhevm_golden::kResultHandle)});
        check_eq(marshal(r), fhevm_golden::kDecryptRecordJSON, "decrypt record");
        DecryptRecord back;
        std::string err;
        check(unmarshal(fhevm_golden::kDecryptRecordJSON, &back, &err) && back == r,
              "decrypt record reads back identically");
    }
    {
        EpochRecord r;
        r.epoch = 0;
        r.start_time = fhevm_golden::kGenesisTime;
        r.committee = golden_committee();
        r.committee_nil = false;
        r.threshold = 2;
        r.public_key = bytes_of(fhevm_golden::kNetworkPublicKey);
        r.public_key_nil = false;
        r.status = EpochStatus::Active;
        check_eq(marshal(r), fhevm_golden::kEpochRecordJSON, "epoch record");
        EpochRecord back;
        std::string err;
        bool ok = unmarshal(fhevm_golden::kEpochRecordJSON, &back, &err);
        check(ok && back.epoch == r.epoch && back.committee == r.committee &&
                  back.threshold == r.threshold && back.public_key == r.public_key,
              "epoch record reads back identically");
    }

    // ---- genesis, and the block it names ---------------------------------------
    {
        auto g = parse_genesis(fhevm_golden::kGenesisJSON);
        accepted(g, "Go's genesis document parses");
        if (g) {
            check_eq(marshal(*g), fhevm_golden::kGenesisJSON, "genesis re-marshals identically");
            Memory store;
            VM vm(&store, VM::Config{96369, golden_chain(), "F"});
            accepted(vm.initialize(fhevm_golden::kGenesisJSON), "a chain boots on it");
            check_eq(hex_of(Id(vm.last_accepted())), fhevm_golden::kGenesisBlockId,
                     "and its genesis block is the one Go names");
            auto bal = vm.balance(payer);
            check(bal && *bal == 10'000'000'000ULL, "the allocation is credited");
            const EpochRecord* ep = vm.epoch(0);
            check(ep != nullptr && ep->committee.size() == 3, "and epoch 0 is seated");
        }
    }

    return report("differential");
}
