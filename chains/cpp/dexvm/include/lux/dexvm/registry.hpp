// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// registry.hpp — the set of admitted real assets and the markets over them, and
// the ONE door an asset enters through.
//
// The registry is the authority every admission decision consults. An asset's
// AssetID is DERIVED from its canonical fields, never supplied, so the identity
// and the description can never disagree. Registration proves three things in
// order and refuses on the first that fails: the record is well-formed, its kind
// is in the active policy, and it is REAL on its target network.
//
// Reality is proven by an injected ChainVerifier, which is what lets the same
// logic back the offline CI validator and the node's boot gate. It is never a
// verifier rigged to say yes: the tests inject one backed by an in-memory chain
// snapshot, so the refusals are genuine refusals.

#pragma once

#include "lux/dexvm/asset.hpp"
#include "lux/dexvm/id.hpp"

#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <set>
#include <shared_mutex>
#include <string>
#include <vector>

namespace lux::dexvm {

// RiskTier is an operator-assigned classification. It is metadata: it does not
// gate admissibility — only REALITY does — and the venue uses it to set
// conservative caps on newer assets. Tier0 is the safest.
enum class RiskTier : std::uint8_t {
    Tier0 = 0,  // primary-network native + canonical stables
    Tier1 = 1,  // established, audited tokens
    Tier2 = 2,  // newer / lower-liquidity tokens
    Tier3 = 3,  // experimental — tightest caps
};

inline bool valid(RiskTier t) { return std::uint8_t(t) <= std::uint8_t(RiskTier::Tier3); }

// Asset is a single registered, real, on-chain asset.
struct Asset {
    // The Lux network this asset lives on (1 mainnet, 2 testnet, ...).
    std::uint32_t network_id = 0;
    // The SOURCE chain: the C-Chain for EVM_NATIVE/ERC20, the UTXO source chain
    // for UTXO.
    Id chain_id{};
    AssetKind kind = AssetKind::Invalid;
    // The on-chain reference: the 20-byte ERC-20 address, the 20-byte native
    // marker, or the 32-byte UTXO assetID. Hex in JSON.
    Bytes canonical_ref;
    // The on-chain decimal precision. Cross-checked against chain state.
    std::uint8_t decimals = 0;
    // Display metadata. NOT identity — the AssetID does not hash them — and NOT
    // a ticker-id: an asset is keyed by AssetID, never by symbol.
    std::string symbol;
    std::string name;
    // Whether this asset (and markets over it) may trade. A disabled asset stays
    // registered, and so auditable, but admits no markets.
    bool enabled = false;
    RiskTier risk_tier = RiskTier::Tier0;

    // id derives this record's canonical AssetID from its real fields.
    Result<Id> id() const;

    // validate_shape checks the record is internally well-formed BEFORE any
    // chain I/O: valid kind, valid ref shape, valid tier, non-empty chain, and a
    // symbol that is a label rather than an id. A record that fails this can
    // never be real, so it is refused early and cheaply.
    Result<void> validate_shape() const;
};

// ChainVerifier proves an asset is REAL by reading live chain state. It is
// injected so the admission logic is identical offline in CI (backed by
// JSON-RPC against the target net) and at node boot (backed by the node's own
// running chain ids). An error means the object is not there, and the asset MUST
// be refused.
struct ChainVerifier {
    virtual ~ChainVerifier() = default;

    // Confirms a contract exists at addr on the given C-Chain of the given
    // network and returns its on-chain decimals().
    virtual Result<std::uint8_t> verify_erc20(std::uint32_t network_id, const Id& c_chain_id,
                                              ByteView addr) = 0;
    // Confirms the C-Chain is the expected native chain for the network and
    // returns the native decimals.
    virtual Result<std::uint8_t> verify_evm_native(std::uint32_t network_id,
                                                   const Id& c_chain_id) = 0;
    // Confirms a UTXO assetID exists on the given source chain of the given
    // network and returns its denomination.
    virtual Result<std::uint8_t> verify_utxo_asset(std::uint32_t network_id,
                                                   const Id& source_chain_id,
                                                   const Id& asset_id) = 0;

    // The manifest-level identity check a verifier MAY implement (Go's
    // CChainConfirmer): before any asset lookup, confirm the C-Chain being
    // talked to is the one the manifest declares. A verifier that cannot answer
    // leaves this at the default, which admits — the check is then simply not
    // part of that verifier's proof, exactly as in Go where the interface is
    // type-asserted and skipped when absent.
    virtual bool confirms_c_chain() const { return false; }
    virtual Result<void> confirm_c_chain(std::uint32_t /*network_id*/,
                                         std::uint64_t /*evm_chain_id*/,
                                         const Id& /*c_chain_id*/) {
        return {};
    }
};

// verify_on_chain proves the asset is real against live chain state via v, and
// that its declared decimals match what the chain reports. This is the gate that
// makes "synthetic asset" unrepresentable: a synthetic asset has nothing for the
// verifier to find.
Result<void> verify_on_chain(const Asset& a, ChainVerifier& v);

// Market is an admitted trading pair. Both sides are pinned to registered, real,
// enabled assets by construction: a market cannot be created unless both sides
// resolve. Its id is DERIVED from the two AssetIDs and the venue config, never
// supplied, so a market's identity is structurally bound to its real assets.
struct Market {
    // Must match both assets' networks — a market does not span networks.
    std::uint32_t network_id = 0;
    Id base_asset_id{};
    Id quote_asset_id{};
    // The canonical serialization of the venue parameters (tick, lot, fee tier)
    // that distinguish two venues on one pair. Hex in JSON.
    Bytes venue_config;
    // Whether the market trades. A disabled market is still pinned to real
    // assets; it simply admits no orders.
    bool enabled = false;

    Id id() const { return market_id(network_id, base_asset_id, quote_asset_id, view(venue_config)); }
};

// Registry is the in-memory set of admitted real assets, keyed by canonical
// AssetID, plus the markets over them. It is concurrency-safe; the hot path
// (resolve) is a read under a shared lock.
//
// Iteration is in ascending id order rather than Go's randomised map order. That
// is a strengthening, not a divergence: it makes which of several bad records is
// reported first deterministic, and the decisions themselves do not depend on
// order.
class Registry {
public:
    // Constructs an empty registry permitting the given kinds. With no kinds it
    // admits nothing — fail-closed. The canonical production policy is all three.
    explicit Registry(std::vector<AssetKind> allowed = {});

    bool allows_kind(AssetKind k) const;

    // register_asset admits a single asset after proving it is well-formed, of
    // an allowed kind, and REAL on its target network. It is the ONLY way an
    // asset enters — there is no path that admits an unverified asset. The
    // derived AssetID is returned so callers can pin markets to it.
    Result<Id> register_asset(const Asset& a, ChainVerifier& v);

    // resolve returns the registered asset for an id. Absent for any
    // unregistered (i.e. synthetic) id — the predicate the market gate and the
    // startup gate use to refuse synthetic references.
    std::optional<Asset> resolve(const Id& id) const;

    // must_resolve_enabled is the strict resolver the market gate uses: an error
    // for an id that is unknown (synthetic) or disabled.
    Result<Asset> must_resolve_enabled(const Id& id) const;

    std::size_t len() const;
    void each(const std::function<void(const Id&, const Asset&)>& fn) const;

    // create_market admits a market ONLY if BOTH sides resolve to a registered,
    // enabled, real asset on the SAME network. This is the structural
    // enforcement of "no synthetic market": there is no AssetID for a synthetic
    // asset, so a market over one cannot resolve, so it cannot be created.
    //
    // The check order is deliberate and fail-closed: resolve base, resolve
    // quote, reject self-pair, reject network mismatch, reject duplicate. Any
    // failure leaves the registry unchanged.
    Result<Id> create_market(const Market& m);

    std::optional<Market> resolve_market(const Id& id) const;
    void each_market(const std::function<void(const Id&, const Market&)>& fn) const;
    std::size_t market_len() const;

    // copy returns an independent registry holding the same already-admitted
    // records and the same policy. It is not an admission path: nothing enters
    // through it that did not enter through register_asset or create_market
    // first. It exists so a block can be EXECUTED against a set without touching
    // the live one — a node that mutated its live registry while deciding
    // whether to vote would have already changed its mind by the time it voted.
    std::unique_ptr<Registry> copy() const;

    // restore_asset / restore_market write a record under a given id WITHOUT the
    // admission checks. There is exactly one legitimate caller — reloading the
    // durable set at boot, where every row was admitted at an earlier height and
    // re-proving it would ask a boot-time verifier to re-decide a decided past.
    //
    // Because it is the one way in that skips admission, it is also what a test
    // uses to stand in for a corrupted or forced config, and that is the point:
    // the boot gate must catch a set that arrived this way, or it is a
    // restatement of the check it is supposed to back up rather than a second
    // line of defence.
    void restore_asset(const Id& id, const Asset& a);
    void restore_market(const Id& id, const Market& m);

private:
    mutable std::shared_mutex mu_;
    std::set<AssetKind> allowed_kinds_;
    std::map<Id, Asset> by_id_;
    std::map<Id, Market> markets_;
};

}  // namespace lux::dexvm
