// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// forbidden.hpp — the deny half of admission.
//
// Reality (ChainVerifier) is the positive gate. These are the negative one: a
// set of cheap, total predicates that recognise the SHAPES of what the DEX must
// never carry, so the boot gate can refuse them by name — before, or instead of,
// a chain lookup. A branded universe is refused even when the token behind it is
// genuinely on-chain, because reality proves existence and says nothing about
// provenance.

#pragma once

#include "lux/dexvm/registry.hpp"

#include <string_view>

namespace lux::dexvm {

// is_forbidden_universe_label reports whether a chain's human label names an
// off-network white-label universe a Lux-native DEX must not carry assets from.
// Case-insensitive substring match — no Lux chain label contains these tokens,
// so the match is precise to the forbidden brands.
bool is_forbidden_universe_label(std::string_view label);

// is_mock_liquidity_ref reports whether a label names mock/synthetic/phantom
// liquidity: fabricated depth with no real on-chain reserves behind it. The DEX
// quotes real reserves only. Case-insensitive.
bool is_mock_liquidity_ref(std::string_view label);

// looks_like_ascii_ticker_id reports whether a string is being used as an asset
// IDENTITY rather than a display symbol. An AssetID is a 32-byte hash; a bare
// ticker where an id is expected is the anti-pattern.
//
// It deliberately does NOT reject normal symbols — a symbol may BE "LUX". It
// rejects the specific pair-shaped, all-caps token ("LUX/USDC", "LUX-USDC@venue")
// that names two assets, because a per-asset symbol never does.
bool looks_like_ascii_ticker_id(std::string_view s);

// assert_no_forbidden_asset_refs is the per-asset deny-gate: it refuses an asset
// whose source chain is labeled a forbidden off-network universe, whose
// symbol/name names mock liquidity, or whose symbol is an ASCII-ticker id.
Result<void> assert_no_forbidden_asset_refs(const Asset& a, std::string_view chain_label);

}  // namespace lux::dexvm
