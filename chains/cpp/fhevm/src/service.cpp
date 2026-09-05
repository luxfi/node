// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/service.hpp"

namespace lux::fhevm {
namespace {

CiphertextView to_view(const CiphertextRecord& r) {
    CiphertextView v;
    v.handle = hex(view(r.handle));
    v.owner = account_string(r.owner);
    v.scheme = r.scheme;
    v.digest = hex(view(r.digest));
    v.type = r.type;
    v.level = r.level;
    v.epoch = r.epoch;
    v.size = r.size;
    v.registered_at = r.registered_at;
    v.chain_id = id_string(r.chain_id);
    return v;
}

}  // namespace

Result<Account> account_from_hex(std::string_view s) {
    Bytes b;
    if (!from_hex(s, &b)) return fail(Err::InvalidPayload, "address is not hex");
    Account a{};
    if (b.size() != a.size()) return fail(Err::InvalidPayload, "address is the wrong width");
    std::copy(b.begin(), b.end(), a.begin());
    return a;
}

Result<Id> hash32(std::string_view s) {
    Bytes b;
    if (!from_hex(s, &b)) return fail(Err::InvalidPayload, "identifier is not hex");
    Id id{};
    if (b.size() != id.size()) return fail(Err::InvalidPayload, "identifier is the wrong width");
    std::copy(b.begin(), b.end(), id.begin());
    return id;
}

std::string_view Service::operation_name(std::uint8_t type) {
    switch (type) {
        case kTxRegisterCiphertext: return "registerCiphertext";
        case kTxGrantPermit: return "grantPermit";
        case kTxRevokePermit: return "revokePermit";
        case kTxRequestDecrypt: return "requestDecrypt";
        case kTxFulfillDecrypt: return "fulfillDecrypt";
        case kTxAdvanceEpoch: return "advanceEpoch";
        default: return "";
    }
}

Result<Id> Service::submit_transaction(ByteView raw) const {
    auto tx = parse_transaction(raw);
    if (!tx) return std::unexpected(tx.error());
    return vm_->submit_tx(*tx);
}

Result<CiphertextView> Service::ciphertext(std::string_view handle_hex) const {
    auto h = hash32(handle_hex);
    if (!h) return std::unexpected(h.error());
    const CiphertextRecord* rec = vm_->ciphertext(*h);
    if (rec == nullptr) return fail(Err::CiphertextNotFound);
    return to_view(*rec);
}

Result<std::vector<CiphertextView>> Service::ciphertexts(std::string_view owner_hex,
                                                         std::string_view scheme) const {
    std::vector<CiphertextView> out;
    Account owner{};
    bool by_owner = false;
    if (!owner_hex.empty()) {
        auto a = account_from_hex(owner_hex);
        if (!a) return std::unexpected(a.error());
        owner = *a;
        by_owner = true;
    }
    for (const CiphertextRecord* rec : vm_->ciphertexts()) {
        if (by_owner && rec->owner != owner) continue;
        if (!scheme.empty() && rec->scheme != scheme) continue;
        out.push_back(to_view(*rec));
    }
    return out;
}

Result<PermitView> Service::permit(std::string_view permit_id_hex) const {
    auto id = hash32(permit_id_hex);
    if (!id) return std::unexpected(id.error());
    const PermitRecord* rec = vm_->permit(*id);
    if (rec == nullptr) return fail(Err::PermitNotFound);
    PermitView v;
    v.permit_id = hex(view(rec->permit_id));
    v.handle = hex(view(rec->handle));
    v.grantor = account_string(rec->grantor);
    v.grantee = account_string(rec->grantee);
    v.operations = rec->operations;
    v.expiry = rec->expiry;
    v.status = rec->status;
    v.created_at = rec->created_at;
    v.chain_id = id_string(rec->chain_id);
    return v;
}

Result<DecryptView> Service::decrypt(std::string_view request_id_hex) const {
    auto id = hash32(request_id_hex);
    if (!id) return std::unexpected(id.error());
    const DecryptRecord* rec = vm_->decrypt(*id);
    if (rec == nullptr) return fail(Err::RequestNotFound);

    // A pending request past its expiry can no longer be answered — check_auth
    // refuses every attestation to it — so it is reported expired. The STORED
    // status stays pending because nothing ever answered it; expiry is a fact
    // about the clock, and this is the one place that reads the clock to say so.
    RequestStatus status = rec->status;
    if (status == RequestStatus::Pending && rec->expiry != 0 &&
        vm_->clock().now() > rec->expiry) {
        status = RequestStatus::Expired;
    }

    DecryptView v;
    v.request_id = hex(view(rec->request_id));
    v.handle = hex(view(rec->ciphertext_handle));
    v.requester = account_string(rec->requester);
    v.permit_id = hex(view(rec->permit_id));
    v.callback = hex(view(rec->callback));
    v.selector = hex(view(rec->callback_selector));
    v.epoch = rec->epoch;
    v.status = std::string(request_status_string(status));
    v.expiry = rec->expiry;
    v.created_at = rec->created_at;
    v.completed_at = rec->completed_at;
    v.source_chain = id_string(rec->source_chain);
    if (rec->status == RequestStatus::Completed) {
        v.result_handle = hex(view(rec->result_handle));
    }
    if (const EpochRecord* ep = vm_->epoch(rec->epoch)) v.threshold = ep->threshold;
    for (const auto& a : rec->attestations) {
        v.attestations.push_back(AttestationView{account_string(a.member), hex(view(a.value))});
    }
    return v;
}

Result<CommitteeView> Service::committee(std::uint64_t epoch) const {
    const EpochRecord* rec = vm_->epoch(epoch);
    if (rec == nullptr) return fail(Err::EpochNotFound);
    CommitteeView v;
    v.epoch = rec->epoch;
    v.threshold = rec->threshold;
    for (const auto& m : rec->committee) {
        v.members.push_back(CommitteeMemberView{node_id_string(m.node_id),
                                                hex(view(m.public_key)), m.weight, m.index});
    }
    return v;
}

CommitteeView Service::current_committee() const {
    auto v = committee(vm_->current_epoch_number());
    if (!v) return CommitteeView{vm_->current_epoch_number(), 0, {}};
    return *v;
}

Result<Service::Balance> Service::balance(std::string_view address_hex) const {
    auto acct = account_from_hex(address_hex);
    if (!acct) return std::unexpected(acct.error());
    auto bal = vm_->balance(*acct);
    if (!bal) return std::unexpected(bal.error());
    auto burned = vm_->burned();
    if (!burned) return std::unexpected(burned.error());
    return Balance{*bal, *burned};
}

Result<HealthReport> Service::health() const { return vm_->health(); }

Service::PublicParams Service::public_params() const {
    // lattigo's ExampleParameters128BitLogN14LogQP438 at the runtime's default
    // threshold configuration: ring dimension 2^14, log(QP) 435, default scale
    // 2^45. Pinned here, and pinned again in the test, so a change to the
    // runtime's parameters shows up as a failure rather than as two networks
    // encrypting under different moduli.
    PublicParams p;
    p.epoch = vm_->current_epoch_number();
    p.log_n = 14;
    p.log_qp = 435;
    p.log_scale = 45;
    p.chain_id = id_string(vm_->chain_id());
    if (const EpochRecord* ep = vm_->epoch(p.epoch)) {
        p.threshold = ep->threshold;
        p.public_key = hex(view(ep->public_key));
    }
    return p;
}

FeeSchedule Service::fee_schedule() const {
    FeeSchedule out;
    out.gas_price = kGasPrice;
    for (std::uint8_t op : {kTxRegisterCiphertext, kTxGrantPermit, kTxRevokePermit,
                            kTxRequestDecrypt, kTxFulfillDecrypt, kTxAdvanceEpoch}) {
        if (uses_scheme(op)) {
            for (std::string_view scheme : schemes()) {
                Transaction tx;
                tx.type = op;
                tx.scheme = std::string(scheme);
                auto g = gas_for(tx);
                auto f = fee_for(tx);
                out.entries.push_back(FeeScheduleEntry{std::string(operation_name(op)),
                                                       std::string(scheme), g ? *g : 0,
                                                       f ? *f : 0});
            }
            continue;
        }
        Transaction tx;
        tx.type = op;
        auto g = gas_for(tx);
        auto f = fee_for(tx);
        out.entries.push_back(
            FeeScheduleEntry{std::string(operation_name(op)), "", g ? *g : 0, f ? *f : 0});
    }
    return out;
}

}  // namespace lux::fhevm
