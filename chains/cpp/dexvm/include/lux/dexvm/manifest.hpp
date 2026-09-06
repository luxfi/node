// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// manifest.hpp — the per-network declaration of what trades, and the one routine
// that turns it into a live, gated registry.
//
// There is exactly one manifest per network. It is the SINGLE source of truth
// for the real assets and markets the DEX admits there, and it does NOT carry
// the derived AssetIDs — those are computed from the canonical fields, so the
// file cannot disagree with the identity.
//
// Two proofs stand behind an admitted manifest, and they are deliberately
// different proofs:
//
//   CI proves each token EXISTS on the live target net before the artifact
//   ships. It catches a fabricated or typo'd address.
//
//   The NODE proves chain-IDENTITY binding, structure and policy at boot, with
//   no remote RPC in the path, so a validator starts even when a remote endpoint
//   is briefly unreachable. It catches a wrong-net or wrong-chain manifest.
//
// A pinned content hash binds the two: the node refuses any manifest whose bytes
// are not the artifact CI approved. That is what stops a locally edited file —
// one real token swapped for a fabricated address — from loading, and it matters
// because a node holds no EVM state of its own to re-check the token with.

#pragma once

#include "lux/dexvm/gate.hpp"
#include "lux/dexvm/registry.hpp"

#include <functional>
#include <map>
#include <string>
#include <vector>

namespace lux::dexvm {

struct Manifest {
    // The canonical network name. It must match the deploy target: you cannot
    // ship the testnet manifest to mainnet.
    std::string network;
    // The Lux networkID every asset and market here must declare. A per-entry
    // id that disagrees is rejected.
    std::uint32_t network_id = 0;
    // The C-Chain's EVM chainID (eth_chainId): the RPC-checkable identity CI
    // confirms before admitting any ERC-20 or native entry, so a manifest can
    // never be validated against the wrong chain.
    std::uint64_t evm_chain_id = 0;
    // The C-Chain's CONSENSUS id, used in the AssetID preimage so a derived
    // AssetID lives in the same identity space as the on-chain atomic objects.
    // EVM_NATIVE and ERC20 entries are rooted here.
    Id c_chain_id{};
    // Source chain id (hex, as ids.ID.Hex writes it) to human label, consumed by
    // the forbidden-reference deny-scan. Optional: an unlabeled chain simply has
    // no brand to match.
    std::map<std::string, std::string> chain_labels;
    std::vector<Asset> assets;
    std::vector<Market> markets;
    // Go writes a nil slice as null and an empty one as [], and the pin is over
    // bytes, so a manifest has to remember which one it holds. A default-built
    // manifest holds neither list, which is nil — the same as Go's zero value.
    bool assets_is_null = true;
    bool markets_is_null = true;

    // validate_shape checks internal consistency before any chain I/O: a network
    // name, every asset and market on the manifest's network, every C-Chain
    // asset rooted at the manifest's own C-Chain, and every asset structurally
    // valid.
    Result<void> validate_shape() const;

    // chain_label_for is the deny-scan label lookup for this manifest's declared
    // labels.
    std::function<std::string(const Id&)> chain_label_for() const;

    // admit_into registers every asset (each proven real against v) and creates
    // every market (each pinned to two registered assets), WITHOUT running the
    // boot gate. It is the admission half, separated so a caller that owns the
    // gate — the node, which runs it once with its own class and policy — does
    // not run it twice.
    Result<void> admit_into(Registry& reg, ChainVerifier& v) const;

    // apply_to is admit_into followed by the boot gate: the ONE routine that
    // turns a manifest into a live, gated registry, used identically by CI with
    // an RPC verifier and by the node with the runtime verifier. On failure reg
    // is left partially populated and the caller discards it.
    Result<void> apply_to(Registry& reg, ChainVerifier& v, const DexAssetPolicy& policy) const;

    // validate is the full CI check for one manifest against one verifier,
    // using a fresh registry under the canonical locked-down policy. It returns
    // what was admitted so a caller can report it.
    Result<std::unique_ptr<Registry>> validate(ChainVerifier& v) const;

    // encode reproduces Go's MarshalIndent(m, "", "  ") for this manifest, byte
    // for byte. It is what makes a pin checkable from this side: a manifest
    // written here hashes to the same digest Go's writer would produce.
    std::string encode() const;
};

// decode_manifest_bytes content-hashes and decodes raw manifest bytes, returning
// the manifest and the bytes' SHA-256 (lowercase hex). It is the SINGLE decoder,
// so a file load and an embedded load share the same shape validation, the same
// unknown-field refusal, and the same hash discipline. `label` is error context.
struct DecodedManifest {
    Manifest manifest;
    std::string sha256_hex;
};
Result<DecodedManifest> decode_manifest_bytes(ByteView raw, std::string_view label);

// load_manifest reads and decodes a manifest file. It does NOT verify against
// chain state — that is apply_to, which needs a verifier — it only parses and
// structurally validates.
Result<Manifest> load_manifest(const std::string& path);

// load_manifest_pinned reads a manifest and REFUSES it unless its content
// SHA-256 equals expected_sha256 (lowercase hex, with or without a "0x" or
// "sha256:" prefix, case-insensitive).
//
// An empty pin means none is configured and falls back to shape-only loading —
// pinning is opt-in per deployment. But once a hash is set the file must match
// it exactly, and a MALFORMED expected hash is itself an error: you cannot pin
// against garbage.
Result<Manifest> load_manifest_pinned(const std::string& path, std::string_view expected_sha256);

// normalize_sha256 lowercases and strips an optional "0x" or "sha256:" prefix,
// then checks the value is a 32-byte digest. Exposed because the pin's own
// well-formedness is a decision the caller may want to make early.
Result<std::string> normalize_sha256(std::string_view h);

}  // namespace lux::dexvm
