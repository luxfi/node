// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// adopt.cpp — the register of networks Lux did not create.
//
// Rendered from Go vms/platformvm/adopt/adopt.go and registry.go (LP-1021).

#include "lux/platformvm/adopt.hpp"

namespace lux::platformvm::adopt {

std::string_view anchor_name(Anchor a) {
    switch (a) {
        case Anchor::Declared: return "declared";
        case Anchor::Attested: return "attested";
        case Anchor::Proven: return "proven";
    }
    return "unknown anchor";
}

std::string_view holding_name(Holding h) {
    switch (h) {
        case Holding::Gateway: return "gateway";
        case Holding::Address: return "address";
    }
    return "unknown holding";
}

Status Record::valid() const {
    if (chain_id == 0) return fail(Err::NoChainId, "a network with no chain id");
    if (identity == kEmptyId) return fail(Err::NoIdentity, "a chain id is not an identity");
    if (anchor < Anchor::Declared || anchor > Anchor::Proven)
        return fail(Err::BadAnchor, "a basis for belief nobody defined");
    if (holding < Holding::Gateway || holding > Holding::Address)
        return fail(Err::BadHolding, "a way of holding value nobody defined");
    if (endpoints.empty()) return fail(Err::NoEndpoints, "a network nobody can reach");
    if (parent == identity) return fail(Err::SelfParent, "a network cannot take security from itself");

    // The rule the whole permissionless path rests on. A declared anchor is a
    // record that the estate agreed, and agreement is not evidence — so it may
    // not hold a key. An anchor that may hold value and names no key is a
    // record promising something it cannot do.
    if (custodial(anchor) && custody.empty())
        return fail(Err::NoCustody, std::string(anchor_name(anchor)) + " names no custody key");
    if (!custodial(anchor) && !custody.empty())
        return fail(Err::CustodyUnheld, std::string(anchor_name(anchor)) + " may not carry custody");
    return ok();
}

std::optional<Record> Registry::get(const Id& id) const {
    const auto it = by_id_.find(id);
    if (it == by_id_.end()) return std::nullopt;
    return it->second;
}

Status Registry::adopt(const Record& r) {
    if (auto st = r.valid(); !st) return st;
    if (by_id_.count(r.key()) != 0) return fail(Err::AlreadyAdopted, r.key().hex());
    if (!r.sovereign() && by_id_.count(r.parent) == 0)
        return fail(Err::NoParent, r.parent.hex() + " is not adopted");
    by_id_[r.key()] = r;
    return ok();
}

Status Registry::revise(const Record& r) {
    if (auto st = r.valid(); !st) return st;
    const auto it = by_id_.find(r.key());
    if (it == by_id_.end()) return fail(Err::NotAdopted, r.key().hex());
    if (!(r.parent == it->second.parent))
        return fail(Err::SourceNotRevisable,
                    "where " + r.key().hex() + " takes its security from does not change");
    if (r.anchor < it->second.anchor)
        return fail(Err::WeakerAnchor, std::string(anchor_name(it->second.anchor)) + " -> " +
                                           std::string(anchor_name(r.anchor)));
    it->second = r;
    return ok();
}

Status Registry::weaken(const Id& id, Anchor to) {
    const auto it = by_id_.find(id);
    if (it == by_id_.end()) return fail(Err::NotAdopted, id.hex());
    if (to < Anchor::Declared || to > Anchor::Proven)
        return fail(Err::BadAnchor, "a basis for belief nobody defined");
    if (to >= it->second.anchor)
        return fail(Err::NotWeaker, std::string(anchor_name(to)) + " is not weaker than " +
                                        std::string(anchor_name(it->second.anchor)));
    it->second.anchor = to;
    // Dropping below the custodial line drops the key with it. A custody key on
    // a record that may no longer hold value is a record contradicting itself,
    // and the whole point of the anchor is that downstream reads one field to
    // know what it may do.
    if (!custodial(to)) it->second.custody.clear();
    return ok();
}

Status Registry::release(const Id& id) {
    if (by_id_.count(id) == 0) return fail(Err::NotAdopted, id.hex());
    for (const auto& [key, r] : by_id_) {
        (void)key;
        if (r.parent == id) return fail(Err::ParentHeld, r.key().hex() + " still takes security from it");
    }
    by_id_.erase(id);
    return ok();
}

bool Registry::may_hold(const Id& id) const {
    const auto it = by_id_.find(id);
    return it != by_id_.end() && custodial(it->second.anchor);
}

}  // namespace lux::platformvm::adopt
