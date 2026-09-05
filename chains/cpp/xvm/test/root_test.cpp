// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// root_test.cpp — the execution root, against the cross-language KAT.
//
// A port of state/xvmroot/xvmroot_test.go, whose fixture is itself a byte-for-
// byte port of the deterministic input every one of the seven GPU backends and
// the CPU oracle hash. The expected values here are therefore not this port's
// opinion — they are what Go, the CPU oracle, and every accelerator all produce,
// and a C++ node that computed anything else could not be in a quorum with any
// of them.

#include "lux/core/check.hpp"

#include "lux/xvm/root.hpp"

using namespace lux::xvm;
using namespace lux::core::test;

namespace {

constexpr std::uint8_t kKatParentByte0 = 0xEE;
constexpr std::uint64_t kKatHeight = 100;
constexpr std::uint32_t kStatusAccepted = 1;
constexpr std::uint32_t kStatusRejected = 2;

// 8 UTXO slots; 0..3 occupied, 4..7 left zero (unoccupied, and therefore
// skipped by the fold while still consuming their slot index).
std::vector<root::UTXOLeaf> kat_utxos() {
    std::vector<root::UTXOLeaf> us(8);
    for (int i = 0; i < 4; ++i) {
        auto& u = us[std::size_t(i)];
        for (int k = 0; k < 32; ++k) {
            u.utxo_id[std::size_t(k)] =
                std::uint8_t((std::uint32_t(i) * 11 + std::uint32_t(k)) ^ 0x71u);
            u.asset_id[std::size_t(k)] = std::uint8_t(0xA0 + (k & 0xF));
            u.owner_root[std::size_t(k)] = std::uint8_t(0xC0 + (k & 0xF));
        }
        u.amount_lo = std::uint64_t(1000 + i);
        u.amount_hi = 0;
        u.locktime = 0;
        u.threshold = 1;
        u.status = root::kUTXOOccupied;
    }
    return us;
}

std::vector<root::AssetLeaf> kat_assets() {
    std::vector<root::AssetLeaf> as(4);  // slot 3 left zero
    for (int i = 0; i < 3; ++i) {
        auto& a = as[std::size_t(i)];
        for (int k = 0; k < 32; ++k) {
            a.asset_id[std::size_t(k)] =
                std::uint8_t((std::uint32_t(i) * 7 + std::uint32_t(k)) ^ 0xC3u);
            a.mint_authority_root[std::size_t(k)] = std::uint8_t(0xD0 + (k & 0xF));
        }
        a.total_supply_lo = std::uint64_t(1000000 + i);
        a.total_supply_hi = 0;
        a.freeze_flag = 1;
        a.denomination = 8;
        a.occupied = 1;
    }
    return as;
}

std::vector<root::TxLeaf> kat_txs() {
    std::vector<root::TxLeaf> ts(4);
    for (int i = 0; i < 4; ++i) {
        auto& t = ts[std::size_t(i)];
        for (int k = 0; k < 32; ++k) {
            t.tx_id[std::size_t(k)] = std::uint8_t(i + k);
            t.proof_digest[std::size_t(k)] = std::uint8_t(i * 5 + k);
        }
        t.kind = std::uint32_t(i);
        switch (i % 3) {
            case 0:
                t.status = kStatusAccepted;
                break;
            case 1:
                t.status = kStatusRejected;
                t.reject_reason = 42;
                break;
            default:
                break;
        }
    }
    return ts;
}

root::Digest kat_parent() {
    root::Digest p{};
    for (std::size_t k = 0; k < p.size(); ++k) p[k] = std::uint8_t(kKatParentByte0 + k);
    return p;
}

}  // namespace

int main() {
    std::printf("xvm — the execution root, against the cross-language KAT\n\n");

    const auto utxo = root::utxo_root(kat_utxos());
    const auto asset = root::asset_root(kat_assets());
    const auto tx = root::tx_root(kat_txs());
    const auto exec = root::compose(kat_parent(), utxo, asset, tx, kKatHeight);

    // The full execution root every backend produces.
    check_eq(hex_of(exec), "4f144ef76dd14d4447ccf9c746d747c21dd4a6b0945e2180bfac489f21af2d77",
             "execution_root over the KAT fixture");

    // The three sub-roots, by the prefixes the KAT documents.
    check_eq(hex_of(utxo).substr(0, 8), "16663d25", "utxo_root prefix");
    check_eq(hex_of(asset).substr(0, 8), "12f47742", "asset_root prefix");
    check_eq(hex_of(tx).substr(0, 8), "c210388d", "tx_root prefix");

    // compose is the FIXED-SHAPE un-tagged keccak256, not a Merkle node. A
    // regression to a tagged node hash would move these bytes.
    check_eq(hex_of(root::compose(kat_parent(), root::utxo_root(kat_utxos()),
                                  root::asset_root(kat_assets()), root::tx_root(kat_txs()),
                                  kKatHeight)),
             "4f144ef76dd14d4447ccf9c746d747c21dd4a6b0945e2180bfac489f21af2d77",
             "compose is un-tagged keccak256 over parent‖utxo‖asset‖tx‖height_le");

    // The empty set folds to keccak256(""), NOT to zero — an all-empty family
    // must be distinguishable from an absent commitment.
    const std::string empty_keccak =
        "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470";
    check_eq(hex_of(root::utxo_root({})), empty_keccak, "empty utxo_root is keccak256(\"\")");
    check_eq(hex_of(root::asset_root({})), empty_keccak, "empty asset_root is keccak256(\"\")");
    check_eq(hex_of(root::tx_root({})), empty_keccak, "empty tx_root is keccak256(\"\")");
    check_eq(hex_of(root::utxo_root(std::vector<root::UTXOLeaf>(4))), empty_keccak,
             "an all-unoccupied UTXO slate also folds to the empty root");

    // A single leaf is its LeafHash, with no internal node above it — the
    // RFC-6962 rule that keeps a one-element tree distinct from its element.
    {
        root::Digest d{};
        d.fill(0x11);
        check(root::merkle_root({d}) == root::leaf_hash(d),
              "a one-leaf tree is LeafHash(d), not NodeHash(d, d)");
        check(root::merkle_root({d}) != d, "…and never the bare digest");
    }

    // Lone-right promotion: with three leaves the odd one moves up unchanged.
    {
        root::Digest a{}, b{}, c{};
        a.fill(1);
        b.fill(2);
        c.fill(3);
        const auto want = root::node_hash(
            root::node_hash(root::leaf_hash(a), root::leaf_hash(b)), root::leaf_hash(c));
        check(root::merkle_root({a, b, c}) == want,
              "a lone right node is promoted, then paired at the next level");
    }

    return report("root");
}
