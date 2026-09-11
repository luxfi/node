// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "plugin.hpp"

#include <dlfcn.h>

#include <algorithm>
#include <cstdlib>
#include <memory>
#include <mutex>

namespace lux::gpu::plugin {
namespace {

// LuxError: 0 is success. Any other value means "I did not answer".
constexpr int kLuxOk = 0;

using Create = void* (*)();
using Destroy = void (*)(void*);
using BackendName = const char* (*)(void*);
using Keccak256Batch = int (*)(void*, const std::uint8_t*, std::uint8_t*, const std::size_t*,
                               std::size_t);

struct Library {
    void* ctx = nullptr;
    Destroy destroy = nullptr;
    std::string name;
    Keccak256Batch keccak256_batch = nullptr;

    ~Library() {
        if (ctx && destroy) destroy(ctx);
    }
};

// The library's file name. LUX_GPU_LIB names an explicit path; otherwise the
// platform soname is handed to the dynamic loader, which is the only
// installed-software question this repository is entitled to ask.
std::vector<std::string> candidates() {
    std::vector<std::string> v;
    if (const char* p = std::getenv("LUX_GPU_LIB"); p && *p) v.emplace_back(p);
#if defined(__APPLE__)
    v.emplace_back("libluxgpu.dylib");
#else
    v.emplace_back("libluxgpu.so");
#endif
    return v;
}

std::unique_ptr<Library> open() {
    for (const auto& path : candidates()) {
        void* h = ::dlopen(path.c_str(), RTLD_NOW | RTLD_LOCAL);
        if (!h) continue;

        auto create = reinterpret_cast<Create>(::dlsym(h, "lux_gpu_create"));
        auto destroy = reinterpret_cast<Destroy>(::dlsym(h, "lux_gpu_destroy"));
        auto name_of = reinterpret_cast<BackendName>(::dlsym(h, "lux_gpu_backend_name"));
        auto keccak = reinterpret_cast<Keccak256Batch>(::dlsym(h, "lux_gpu_keccak256_batch"));
        // A library that answers to the name but not to the ABI is not a
        // backend. Refusing it keeps a half-loaded plugin from looking like a
        // working one.
        if (!create || !destroy || !name_of || !keccak) continue;

        void* ctx = create();
        if (!ctx) continue;

        auto lib = std::make_unique<Library>();
        lib->ctx = ctx;
        lib->destroy = destroy;
        lib->keccak256_batch = keccak;
        const char* n = name_of(ctx);
        lib->name = n ? n : "unknown";
        return lib;
    }
    return nullptr;
}

// One library, one context, opened once. Every call holds the mutex: the
// context's thread-safety is the plugin's business, not ours to assume.
Library* library() {
    static std::unique_ptr<Library> lib = open();
    return lib.get();
}

std::mutex& lock() {
    static std::mutex m;
    return m;
}

}  // namespace

std::optional<std::string> backend_name() {
    std::lock_guard<std::mutex> g(lock());
    Library* lib = library();
    if (!lib) return std::nullopt;
    return lib->name;
}

std::optional<std::vector<Digest>> keccak256_batch(std::span<const Bytes> inputs) {
    std::lock_guard<std::mutex> g(lock());
    Library* lib = library();
    if (!lib) return std::nullopt;

    // A zero-length batch has no answer to ask for, and the C side is entitled
    // to reject the pointers an empty batch would hand it.
    if (inputs.empty()) return std::vector<Digest>{};

    std::size_t total = 0;
    for (const auto& in : inputs) total += in.size();

    // A batch of nothing but empty inputs is legal — keccak of the empty string
    // is a real value — so the buffer still has to be a pointer the C side can
    // hold, even though it will read no bytes through it.
    std::vector<std::uint8_t> flat;
    flat.reserve(total ? total : 1);
    std::vector<std::size_t> lens;
    lens.reserve(inputs.size());
    for (const auto& in : inputs) {
        flat.insert(flat.end(), in.begin(), in.end());
        lens.push_back(in.size());
    }
    if (flat.empty()) flat.push_back(0);

    std::vector<std::uint8_t> out(inputs.size() * 32);
    const int rc =
        lib->keccak256_batch(lib->ctx, flat.data(), out.data(), lens.data(), inputs.size());
    if (rc != kLuxOk) return std::nullopt;

    std::vector<Digest> digests(inputs.size());
    for (std::size_t i = 0; i < inputs.size(); ++i) {
        std::copy_n(out.begin() + std::ptrdiff_t(i * 32), 32, digests[i].begin());
    }
    return digests;
}

}  // namespace lux::gpu::plugin
