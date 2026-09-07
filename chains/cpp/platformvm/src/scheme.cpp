// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// scheme.cpp — the register of threshold schemes.
//
// It starts EMPTY, and that is the whole design: nothing here implements a
// scheme, so nothing here can accept a signature it does not understand.

#include "lux/platformvm/scheme.hpp"

#include <map>

namespace lux::platformvm::scheme {
namespace {

std::map<Id, std::shared_ptr<const Threshold>>& registry() {
    static std::map<Id, std::shared_ptr<const Threshold>> held;
    return held;
}

}  // namespace

std::string_view name(Id id) {
    switch (id) {
        case Id::Bls: return "BLS";
        case Id::Corona: return "Corona";
    }
    return "unknown";
}

Status hold(Id id, std::shared_ptr<const Threshold> impl) {
    if (impl == nullptr) return fail(Err::InvalidState, "a scheme with no implementation");
    auto& r = registry();
    if (r.count(id) != 0)
        return fail(Err::InvalidState, std::string(name(id)) + " is already held");
    r.emplace(id, std::move(impl));
    return ok();
}

bool held(Id id) { return registry().count(id) != 0; }

Result<std::shared_ptr<const Threshold>> of(Id id) {
    const auto it = registry().find(id);
    if (it == registry().end())
        return fail(Err::SchemeNotHeld, std::string(name(id)) + " is not held by this node");
    return it->second;
}

void release(Id id) { registry().erase(id); }

}  // namespace lux::platformvm::scheme
