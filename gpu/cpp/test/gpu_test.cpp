// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gpu_test.cpp — the CPU backend against known answers, and both backends
// against each other.
//
// Run with LUX_GPU_LIB=<path to libluxgpu> to point at an installed kernel
// library. With none installed the differential says "skipped"; it does not
// print a green it did not earn.

#include "lux/gpu/gpu.hpp"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

using namespace lux::gpu;

namespace {

int failures = 0;
int checks = 0;

void check(bool ok, const std::string& what) {
    ++checks;
    if (!ok) {
        ++failures;
        std::fprintf(stderr, "FAIL %s\n", what.c_str());
    }
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

std::string hex(const Address& a) {
    std::string s;
    char t[3];
    for (auto b : a) {
        std::snprintf(t, sizeof(t), "%02x", b);
        s += t;
    }
    return s;
}

Bytes view(const std::vector<std::uint8_t>& v) { return Bytes(v.data(), v.size()); }

std::vector<std::uint8_t> str(const char* s) {
    return std::vector<std::uint8_t>(reinterpret_cast<const std::uint8_t*>(s),
                                     reinterpret_cast<const std::uint8_t*>(s) + std::strlen(s));
}

// A cheap, seeded, reproducible byte stream. A differential wants many shapes,
// not a new dependency.
struct Rng {
    std::uint64_t s;
    std::uint64_t next() {
        s += 0x9e3779b97f4a7c15ull;
        std::uint64_t z = s;
        z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9ull;
        z = (z ^ (z >> 27)) * 0x94d049bb133111ebull;
        return z ^ (z >> 31);
    }
    std::vector<std::uint8_t> bytes(std::size_t n) {
        std::vector<std::uint8_t> v(n);
        for (auto& b : v) b = std::uint8_t(next() & 0xff);
        return v;
    }
};

bool plugin_present() { return backend().rfind("plugin:", 0) == 0; }

// ---- the CPU backend is right ----------------------------------------------

void the_hashes_are_the_ones_the_chain_is_defined_over() {
    const auto abc = str("abc");
    // Keccak-256, Ethereum's 0x01 pad. SHA3-256("abc") is
    // 3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532; a
    // backend answering with that would be answering a different question,
    // which is exactly what the plugin's op_sha3_256_hash is for.
    check(hex(keccak256(view(abc))) ==
              "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45",
          "keccak256(\"abc\")");
    check(hex(empty_root()) ==
              "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
          "keccak256(\"\") — the empty root");
    check(hex(sha256(view(abc))) ==
              "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
          "sha256(\"abc\")");
    check(hex(pubkey_to_address(view(abc))) == "bb1be98c142444d7a56aa3981c3942a978e4dc33",
          "ripemd160(sha256(\"abc\"))");
}

void a_batch_answers_exactly_what_the_singles_answer() {
    Rng rng{0x5eed};
    const std::size_t sizes[] = {0, 1, 31, 32, 135, 136, 137, 271, 272, 1024, 4096};
    for (std::size_t batch : {std::size_t(1), std::size_t(2), std::size_t(3), std::size_t(7),
                              std::size_t(64), std::size_t(257)}) {
        std::vector<std::vector<std::uint8_t>> owned;
        owned.reserve(batch);
        for (std::size_t i = 0; i < batch; ++i) owned.push_back(rng.bytes(sizes[i % 11]));
        std::vector<Bytes> ins;
        ins.reserve(batch);
        for (const auto& o : owned) ins.emplace_back(o.data(), o.size());

        const auto got = keccak256_batch(std::span<const Bytes>(ins.data(), ins.size()));
        check(got.size() == batch, "batch size");
        for (std::size_t i = 0; i < batch; ++i) {
            check(got[i] == keccak256(ins[i]),
                  "batch " + std::to_string(batch) + " element " + std::to_string(i));
        }
    }
}

void a_batch_of_nothing_but_empty_inputs_still_agrees() {
    const std::vector<std::uint8_t> empty;
    for (std::size_t batch : {std::size_t(1), std::size_t(8), std::size_t(100)}) {
        std::vector<Bytes> ins(batch, Bytes(empty.data(), empty.size()));
        const auto span = std::span<const Bytes>(ins.data(), ins.size());
        check(keccak256_batch(span) == cpu::keccak256_batch(span),
              "empty-input batch of " + std::to_string(batch));
    }
    check(keccak256_batch(std::span<const Bytes>()).empty(), "a batch of nothing is nothing");
}

// ---- the fold ---------------------------------------------------------------

// The fold written the obvious way, with no batching and no dispatch. It is the
// thing the seam's version has to keep agreeing with.
Digest scalar_root(const std::vector<Digest>& leaves) {
    if (leaves.empty()) return empty_root();
    std::vector<Digest> level;
    level.reserve(leaves.size());
    for (const auto& d : leaves) level.push_back(leaf_hash(d));
    while (level.size() > 1) {
        const std::size_t cnt = level.size();
        const std::size_t parents = (cnt + 1) / 2;
        std::vector<Digest> next(parents);
        for (std::size_t j = 0; j < cnt / 2; ++j) next[j] = node_hash(level[2 * j], level[2 * j + 1]);
        if (cnt % 2 == 1) next[parents - 1] = level[cnt - 1];
        level = next;
    }
    return level[0];
}

void the_batched_fold_equals_the_scalar_fold_at_every_size() {
    Rng rng{0xf01d};
    for (std::size_t n : {std::size_t(0), std::size_t(1), std::size_t(2), std::size_t(3),
                          std::size_t(5), std::size_t(8), std::size_t(17), std::size_t(64),
                          std::size_t(129), std::size_t(1000)}) {
        std::vector<Digest> leaves(n);
        for (auto& d : leaves) {
            const auto b = rng.bytes(32);
            std::copy(b.begin(), b.end(), d.begin());
        }
        check(merkle_root(leaves) == scalar_root(leaves), "merkle_root n=" + std::to_string(n));
    }
}

void a_single_leaf_root_is_the_tagged_leaf_itself() {
    Digest d{};
    d.fill(1);
    check(merkle_root({d}) == leaf_hash(d), "a single leaf");
}

void an_odd_level_promotes_the_last_node_unchanged() {
    std::vector<Digest> d(3);
    for (std::uint8_t i = 0; i < 3; ++i) d[i].fill(i);
    const Digest want = node_hash(node_hash(leaf_hash(d[0]), leaf_hash(d[1])), leaf_hash(d[2]));
    check(merkle_root(d) == want, "lone-right promotion");
}

// ---- recovery ---------------------------------------------------------------

void a_signature_that_is_not_one_recovers_nothing() {
    Digest h{};
    h.fill(7);
    Signature sig{};
    sig.fill(0);
    // r = s = 0 is not a signature; and v = 4 is not one of the four recovery
    // ids the curve defines.
    check(!recover(h, sig).has_value(), "zero signature recovers nothing");
    sig[64] = 4;
    check(!recover(h, sig).has_value(), "recovery id 4 recovers nothing");
}

// ---- the comparison LUX_GPU=verify makes ------------------------------------

void the_verify_comparison_can_fail() {
    const auto a = str("a");
    const auto bb = str("bb");
    const Bytes ins[2] = {view(a), view(bb)};
    const auto want = cpu::keccak256_batch(std::span<const Bytes>(ins, 2));

    check(internal::same(want, want), "verify accepts two identical answers");

    auto got = want;
    got[1][31] ^= 1;
    check(!internal::same(got, want), "verify refuses two different answers");

    std::vector<Digest> shorter(want.begin(), want.begin() + 1);
    check(!internal::same(shorter, want), "verify refuses an answer of the wrong length");
}

// ---- CPU against plugin ------------------------------------------------------

void both_backends_give_the_same_answer() {
    if (!plugin_present()) {
        std::fprintf(stderr,
                     "skipped: no kernel library installed (backend=%s). "
                     "Set LUX_GPU_LIB to run the differential.\n",
                     backend().c_str());
        return;
    }
    std::fprintf(stderr, "differential against %s\n", backend().c_str());

    Rng rng{0xd1ff};
    const std::size_t sizes[] = {0, 1, 31, 32, 135, 136, 137, 271, 272, 1024, 4096};
    for (std::size_t batch : {std::size_t(1), std::size_t(2), std::size_t(3), std::size_t(7),
                              std::size_t(64), std::size_t(257)}) {
        std::vector<std::vector<std::uint8_t>> owned;
        for (std::size_t i = 0; i < batch; ++i) owned.push_back(rng.bytes(sizes[i % 11]));
        std::vector<Bytes> ins;
        for (const auto& o : owned) ins.emplace_back(o.data(), o.size());
        const auto span = std::span<const Bytes>(ins.data(), ins.size());

        const auto want = cpu::keccak256_batch(span);
        const auto got = keccak256_batch(span);
        check(got == want, "cpu vs plugin, batch of " + std::to_string(batch));
    }
}

}  // namespace

int main() {
    the_hashes_are_the_ones_the_chain_is_defined_over();
    a_batch_answers_exactly_what_the_singles_answer();
    a_batch_of_nothing_but_empty_inputs_still_agrees();
    the_batched_fold_equals_the_scalar_fold_at_every_size();
    a_single_leaf_root_is_the_tagged_leaf_itself();
    an_odd_level_promotes_the_last_node_unchanged();
    a_signature_that_is_not_one_recovers_nothing();
    the_verify_comparison_can_fail();
    both_backends_give_the_same_answer();

    std::fprintf(stderr, "backend=%s  %d checks, %d failed\n", backend().c_str(), checks, failures);
    return failures == 0 ? 0 : 1;
}
