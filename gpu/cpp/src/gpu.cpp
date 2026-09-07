// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/gpu/gpu.hpp"

#include "plugin.hpp"

#include "lux/crypto/keccak.h"
#include "lux/crypto/secp256k1.h"
#include "ripemd160.hpp"
#include "sha256.hpp"

#include <algorithm>
#include <cstdio>
#include <cstdlib>
#include <cstring>

namespace lux::gpu {
namespace {

enum class Policy { Off, On, Verify };

Policy policy() {
    static const Policy p = [] {
        const char* v = std::getenv("LUX_GPU");
        if (!v) return Policy::On;
        const std::string s(v);
        if (s == "off" || s == "OFF" || s == "0") return Policy::Off;
        if (s == "verify" || s == "VERIFY") return Policy::Verify;
        return Policy::On;
    }();
    return p;
}

std::string hex(const Digest& d) {
    std::string s;
    char t[3];
    for (auto b : d) {
        std::snprintf(t, sizeof(t), "%02x", b);
        s += t;
    }
    return s;
}

// What LUX_GPU=verify does when it has both answers: says which byte differs
// and stops. A node that keeps going after its two backends disagree is a node
// building on a block half the network will not have.
void agree(const std::vector<Digest>& got, const std::vector<Digest>& want,
           std::span<const Bytes> inputs) {
    if (internal::same(got, want)) return;
    if (got.size() != want.size()) {
        std::fprintf(stderr, "LUX_GPU=verify: plugin returned %zu digests for a batch of %zu\n",
                     got.size(), want.size());
        std::abort();
    }
    for (std::size_t i = 0; i < want.size(); ++i) {
        if (got[i] != want[i]) {
            std::fprintf(stderr,
                         "LUX_GPU=verify: plugin and CPU disagree on keccak256 of input %zu "
                         "(%zu bytes): plugin %s vs cpu %s — this is a consensus bug, not a "
                         "performance one\n",
                         i, inputs[i].size(), hex(got[i]).c_str(), hex(want[i]).c_str());
            std::abort();
        }
    }
    std::abort();
}

}  // namespace

namespace internal {

bool same(const std::vector<Digest>& got, const std::vector<Digest>& want) {
    return got.size() == want.size() && std::equal(got.begin(), got.end(), want.begin());
}

}  // namespace internal

// ---- the CPU backend -------------------------------------------------------

namespace cpu {

Digest keccak256(std::span<const Bytes> parts) {
    std::size_t total = 0;
    for (const auto& p : parts) total += p.size();
    std::vector<std::uint8_t> joined;
    joined.reserve(total);
    for (const auto& p : parts) joined.insert(joined.end(), p.begin(), p.end());
    Digest out{};
    ::keccak256(joined.data(), joined.size(), out.data());
    return out;
}

std::vector<Digest> keccak256_batch(std::span<const Bytes> inputs) {
    std::vector<Digest> out(inputs.size());
    for (std::size_t i = 0; i < inputs.size(); ++i) {
        ::keccak256(inputs[i].data(), inputs[i].size(), out[i].data());
    }
    return out;
}

Digest sha256(Bytes data) {
    Digest out{};
    cevm::crypto::sha256(reinterpret_cast<std::byte*>(out.data()),
                         reinterpret_cast<const std::byte*>(data.data()), data.size());
    return out;
}

Address ripemd160(Bytes data) {
    Address out{};
    cevm::crypto::ripemd160(reinterpret_cast<std::byte*>(out.data()),
                            reinterpret_cast<const std::byte*>(data.data()), data.size());
    return out;
}

std::optional<CompressedKey> recover(const Digest& hash, const Signature& sig) {
    const std::uint8_t v = sig[64];
    // Four recovery ids exist; anything else is not a signature at all. Go
    // (luxfi/crypto/secp256k1 checkSignature) refuses v >= 4 and accepts all
    // four, and 2 and 3 mean R.x = r + n — a point whose x coordinate is
    // outside the scalar field. The first-party C++ curve refuses r >= n at
    // parse time and so has no path to them; it says so rather than pretending,
    // and REFUSING is the only safe answer a backend that cannot check a
    // signature can give.
    if (v > 1) return std::nullopt;
    std::uint8_t uncompressed[64];
    const secp256k1_status st =
        secp256k1_ecrecover(hash.data(), sig.data(), sig.data() + 32, v, uncompressed);
    if (st != SECP256K1_OK) return std::nullopt;
    // The COMPRESSED form is what a Lux address commits to, so the recovered
    // X||Y is compressed here rather than hashed as it comes.
    CompressedKey out{};
    out[0] = std::uint8_t(0x02 | (uncompressed[63] & 1));
    std::memcpy(out.data() + 1, uncompressed, 32);
    return out;
}

}  // namespace cpu

// ---- the seam --------------------------------------------------------------

std::string backend() {
    if (policy() == Policy::Off) return "cpu";
    if (auto n = plugin::backend_name()) return "plugin:" + *n;
    return "cpu";
}

Digest keccak256(std::span<const Bytes> parts) { return cpu::keccak256(parts); }

Digest keccak256(Bytes one) {
    const Bytes parts[1] = {one};
    return cpu::keccak256(std::span<const Bytes>(parts, 1));
}

std::vector<Digest> keccak256_batch(std::span<const Bytes> inputs) {
    switch (policy()) {
        case Policy::Off:
            return cpu::keccak256_batch(inputs);
        case Policy::On:
            if (auto got = plugin::keccak256_batch(inputs)) return *got;
            return cpu::keccak256_batch(inputs);
        case Policy::Verify: {
            auto want = cpu::keccak256_batch(inputs);
            if (auto got = plugin::keccak256_batch(inputs)) agree(*got, want, inputs);
            return want;
        }
    }
    return cpu::keccak256_batch(inputs);
}

Digest sha256(Bytes data) { return cpu::sha256(data); }

Address ripemd160(Bytes data) { return cpu::ripemd160(data); }

Address pubkey_to_address(Bytes compressed_key) {
    const Digest h = cpu::sha256(compressed_key);
    return cpu::ripemd160(Bytes(h.data(), h.size()));
}

std::optional<CompressedKey> recover(const Digest& hash, const Signature& sig) {
    return cpu::recover(hash, sig);
}

// ---- the fold --------------------------------------------------------------

Digest leaf_hash(const Digest& d) {
    const std::uint8_t tag = kLeafTag;
    const Bytes parts[2] = {Bytes(&tag, 1), Bytes(d.data(), d.size())};
    return cpu::keccak256(std::span<const Bytes>(parts, 2));
}

Digest node_hash(const Digest& l, const Digest& r) {
    const std::uint8_t tag = kNodeTag;
    const Bytes parts[3] = {Bytes(&tag, 1), Bytes(l.data(), l.size()), Bytes(r.data(), r.size())};
    return cpu::keccak256(std::span<const Bytes>(parts, 3));
}

Digest empty_root() { return cpu::keccak256(std::span<const Bytes>()); }

Digest merkle_root(const std::vector<Digest>& leaves) {
    if (leaves.empty()) return empty_root();

    // A level is a batch: every node on it is independent of every other, which
    // is the only reason a device can help at all. The preimages are built here
    // and hashed together, so the whole level makes one dispatch.
    auto hash_all = [](const std::vector<std::vector<std::uint8_t>>& preimages) {
        std::vector<Bytes> views;
        views.reserve(preimages.size());
        for (const auto& p : preimages) views.emplace_back(p.data(), p.size());
        return keccak256_batch(std::span<const Bytes>(views.data(), views.size()));
    };

    std::vector<std::vector<std::uint8_t>> preimages;
    preimages.reserve(leaves.size());
    for (const auto& d : leaves) {
        std::vector<std::uint8_t> p;
        p.reserve(33);
        p.push_back(kLeafTag);
        p.insert(p.end(), d.begin(), d.end());
        preimages.push_back(std::move(p));
    }
    std::vector<Digest> level = hash_all(preimages);

    while (level.size() > 1) {
        const std::size_t cnt = level.size();
        const std::size_t parents = (cnt + 1) / 2;
        const std::size_t pairs = cnt / 2;

        std::vector<std::vector<std::uint8_t>> next_pre;
        next_pre.reserve(pairs);
        for (std::size_t j = 0; j < pairs; ++j) {
            std::vector<std::uint8_t> p;
            p.reserve(65);
            p.push_back(kNodeTag);
            p.insert(p.end(), level[2 * j].begin(), level[2 * j].end());
            p.insert(p.end(), level[2 * j + 1].begin(), level[2 * j + 1].end());
            next_pre.push_back(std::move(p));
        }
        std::vector<Digest> next = hash_all(next_pre);
        next.resize(parents);
        // RFC 6962 lone-right promotion: an odd last node moves up unchanged
        // rather than being paired with itself, which would make two distinct
        // trees share a root.
        if (cnt % 2 == 1) next[parents - 1] = level[cnt - 1];
        level = std::move(next);
    }
    return level[0];
}

}  // namespace lux::gpu
