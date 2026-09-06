// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fixtures.hpp — a chain snapshot to verify against, and the two builders the
// reference's tests use.
//
// FakeChain is a REAL ChainVerifier over an in-memory snapshot of what exists
// on-chain. It is not a stub: it answers "real" only for what was explicitly
// seeded and refuses everything else, so the refusal paths are exercised
// genuinely rather than against a verifier rigged to say yes. It is the in-test
// analogue of the production JSON-RPC / local-chain verifier.
//
// test_id replaces the reference's random ids.GenerateTestID with a hash of a
// counter. Distinctness is all the tests need from it, and determinism is worth
// having: a failure here reproduces exactly.

#pragma once

#include "lux/dexvm/manifest.hpp"
#include "lux/dexvm/registry.hpp"

#include <map>
#include <string>

namespace lux::dexvm::test {

inline Id test_id(std::uint64_t n) {
    Folder f;
    f.tag("dexvm:test:id");
    f.u64(n);
    return f.sum();
}

// addr20 builds a deterministic non-zero 20-byte token address from a seed —
// the reference's own formula, so the two suites name the same addresses.
inline Bytes addr20(std::uint8_t seed) {
    Bytes b(20);
    for (std::size_t i = 0; i < b.size(); ++i)
        b[i] = std::uint8_t(seed + std::uint8_t(i) + 1);  // +1 keeps it non-zero for seed 0
    return b;
}

inline constexpr std::uint32_t kMainnetID = 1;

class FakeChain final : public ChainVerifier {
public:
    void seed_erc20(std::uint32_t network_id, const Id& c_chain, ByteView addr,
                    std::uint8_t decimals) {
        erc20_[{network_id, c_chain, std::string(addr.begin(), addr.end())}] = decimals;
    }
    void seed_native(std::uint32_t network_id, const Id& c_chain, std::uint8_t decimals) {
        native_[{network_id, c_chain}] = decimals;
    }
    void seed_utxo(std::uint32_t network_id, const Id& source, const Id& asset_id,
                   std::uint8_t decimals) {
        utxo_[{network_id, source, asset_id}] = decimals;
    }

    // confirm_c_chain is opt-in per fixture: the reference has both a plain
    // fakeChain and one that also confirms the C-Chain identity, and the two
    // exercise different paths.
    void confirm(std::uint32_t network_id, std::uint64_t evm_chain_id, const Id& c_chain) {
        confirms_ = true;
        confirm_network_ = network_id;
        confirm_evm_ = evm_chain_id;
        confirm_chain_ = c_chain;
    }

    bool confirms_c_chain() const override { return confirms_; }
    Result<void> confirm_c_chain(std::uint32_t network_id, std::uint64_t evm_chain_id,
                                 const Id& c_chain_id) override {
        if (network_id != confirm_network_ || evm_chain_id != confirm_evm_ ||
            c_chain_id != confirm_chain_)
            return fail(kNotOnChain);
        return {};
    }

    Result<std::uint8_t> verify_erc20(std::uint32_t network_id, const Id& c_chain,
                                      ByteView addr) override {
        auto it = erc20_.find({network_id, c_chain, std::string(addr.begin(), addr.end())});
        if (it == erc20_.end()) return fail(kNotOnChain);
        return it->second;
    }
    Result<std::uint8_t> verify_evm_native(std::uint32_t network_id, const Id& c_chain) override {
        auto it = native_.find({network_id, c_chain});
        if (it == native_.end()) return fail(kNotOnChain);
        return it->second;
    }
    Result<std::uint8_t> verify_utxo_asset(std::uint32_t network_id, const Id& source,
                                           const Id& asset_id) override {
        auto it = utxo_.find({network_id, source, asset_id});
        if (it == utxo_.end()) return fail(kNotOnChain);
        return it->second;
    }

private:
    static constexpr const char* kNotOnChain = "fakechain: no such object on this network/chain";

    std::map<std::tuple<std::uint32_t, Id, std::string>, std::uint8_t> erc20_;
    std::map<std::tuple<std::uint32_t, Id>, std::uint8_t> native_;
    std::map<std::tuple<std::uint32_t, Id, Id>, std::uint8_t> utxo_;

    bool confirms_ = false;
    std::uint32_t confirm_network_ = 0;
    std::uint64_t confirm_evm_ = 0;
    Id confirm_chain_{};
};

// real_erc20 builds a verifiable ERC-20 and seeds it, so registering it
// succeeds. The reference's helper, with its 6 decimals and Tier1.
inline Asset real_erc20(FakeChain& fc, const Id& c_chain, ByteView addr, const std::string& sym) {
    fc.seed_erc20(kMainnetID, c_chain, addr, 6);
    Asset a;
    a.network_id = kMainnetID;
    a.chain_id = c_chain;
    a.kind = AssetKind::ERC20;
    a.canonical_ref = to_bytes(addr);
    a.decimals = 6;
    a.symbol = sym;
    a.name = sym + " token";
    a.enabled = true;
    a.risk_tier = RiskTier::Tier1;
    return a;
}

}  // namespace lux::dexvm::test
