// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/forbidden.hpp"

#include <algorithm>
#include <array>
#include <string>

namespace lux::dexvm {
namespace {

// A synthetic/mock liquidity source: fabricated depth that does not correspond
// to real on-chain reserves. Any asset label carrying one is refused.
constexpr std::array<std::string_view, 9> kMockLiquidityTokens{
    "mock",     "synthetic",     "fake",     "phantom", "placeholder",
    "testliquidity", "mockliquidity",
    "d-native",  // the explicitly-forbidden "D-native asset" class
    "dnative",
};

// Off-network white-label universes. A source chain that labels itself one of
// these — or the upstream partner lineage — is refused at boot: the DEX serves
// the Lux primary network and its L1s, never a foreign branded universe.
constexpr std::array<std::string_view, 2> kForbiddenUniverseLabels{
    "liquid",   // catches the whole white-label family (all contain "liquid")
    "partner",  // upstream lineage
};

std::string lower(std::string_view s) {
    std::string out(s);
    std::transform(out.begin(), out.end(), out.begin(),
                   [](unsigned char c) { return char(std::tolower(c)); });
    return out;
}

bool contains_any(std::string_view label, std::span<const std::string_view> needles) {
    const std::string l = lower(label);
    for (std::string_view t : needles) {
        if (l.find(t) != std::string::npos) return true;
    }
    return false;
}

// is_upper_tickerish reports whether s is composed only of A-Z, 0-9 and market
// separators — the alphabet of a ticker or pair id — with at least one letter. A
// human name like "USD Coin" has a space and lowercase, so it is not tickerish;
// a pair id like "LUX/USDC" is.
bool is_upper_tickerish(std::string_view s) {
    bool has_letter = false;
    for (char c : s) {
        if (c >= 'A' && c <= 'Z') {
            has_letter = true;
        } else if (c >= '0' && c <= '9') {
            // a digit is part of the alphabet but is not itself a letter
        } else if (c == '/' || c == '-' || c == ':' || c == '@' || c == '_') {
            // a market separator
        } else {
            return false;
        }
    }
    return has_letter;
}

}  // namespace

bool is_forbidden_universe_label(std::string_view label) {
    return contains_any(label, kForbiddenUniverseLabels);
}

bool is_mock_liquidity_ref(std::string_view label) {
    return contains_any(label, kMockLiquidityTokens);
}

bool looks_like_ascii_ticker_id(std::string_view s) {
    if (s.empty()) return false;
    // A pair-shaped symbol is an id masquerading as a label: a real per-asset
    // symbol never names two assets. The separator alone is not enough — a
    // normal name may hold a hyphen — so the all-caps ticker shape decides.
    for (std::string_view sep : {"/", "-", ":", "@", "_"}) {
        if (s.find(sep) != std::string_view::npos) {
            if (is_upper_tickerish(s)) return true;
        }
    }
    return false;
}

Result<void> assert_no_forbidden_asset_refs(const Asset& a, std::string_view chain_label) {
    if (is_forbidden_universe_label(chain_label))
        return fail("registry: asset source chain \"" + std::string(chain_label) +
                    "\" is a forbidden off-network white-label universe");
    if (is_mock_liquidity_ref(a.symbol) || is_mock_liquidity_ref(a.name))
        return fail("registry: asset symbol/name names mock/synthetic/phantom liquidity");
    if (looks_like_ascii_ticker_id(a.symbol))
        return fail("registry: asset symbol \"" + a.symbol +
                    "\" is an ASCII-ticker id; assets are keyed by AssetID");
    return {};
}

}  // namespace lux::dexvm
