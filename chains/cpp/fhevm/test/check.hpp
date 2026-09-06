// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// check.hpp — the whole test harness: a counter, a line per assertion, and a
// non-zero exit when something failed. A test that cannot run prints SKIP and
// still fails the run, because a silent skip reads as a pass.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/id.hpp"

#include <cstdio>
#include <string>

namespace lux::fhevm::test {

inline int g_fail = 0;
inline int g_pass = 0;

inline void check(bool ok, const std::string& what) {
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (ok) {
        ++g_pass;
    } else {
        ++g_fail;
    }
}

// check_eq prints BOTH sides on a failure — a byte-fidelity test whose failure
// message says only "not equal" costs an hour to diagnose.
inline void check_eq(const std::string& got, const std::string& want, const std::string& what) {
    bool ok = got == want;
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (!ok) {
        std::printf("        got  %s\n        want %s\n", got.c_str(), want.c_str());
        ++g_fail;
    } else {
        ++g_pass;
    }
}

inline void check_eq(std::uint64_t got, std::uint64_t want, const std::string& what) {
    check_eq(std::to_string(got), std::to_string(want), what);
}

inline void skip(const std::string& what, const std::string& why) {
    std::printf("  SKIP  %s (%s)\n", what.c_str(), why.c_str());
    ++g_fail;  // a skip is not a pass
}

inline int report(const char* suite) {
    std::printf("\n%s: %d passed, %d failed\n", suite, g_pass, g_fail);
    return g_fail == 0 ? 0 : 1;
}

// hex_of renders a byte buffer the way the golden vectors are written: bare
// lowercase hex, no 0x.
inline std::string hex_of(ByteView b) { return hex(b); }
inline std::string hex_of(const Bytes& b) { return hex(view(b)); }
inline std::string hex_of(const Id& b) { return hex(view(b)); }
inline std::string hex_of(const Account& b) { return hex(view(b)); }

// unhex turns a golden vector back into bytes, so a test can feed the GO bytes
// into the C++ parser — which is the other half of byte fidelity.
inline Bytes unhex(const std::string& s) {
    Bytes out;
    if (!from_hex(s, &out)) return {};
    return out;
}

inline Id id_of(const std::string& hex_str) {
    Id id{};
    Bytes b = unhex(hex_str);
    if (b.size() == id.size()) std::copy(b.begin(), b.end(), id.begin());
    return id;
}

inline Account account_of(const std::string& hex_str) {
    Account a{};
    Bytes b = unhex(hex_str);
    if (b.size() == a.size()) std::copy(b.begin(), b.end(), a.begin());
    return a;
}

// refused asserts a Result failed for exactly this reason — the shape Go's
// tests spell require.ErrorIs. It prints the refusal that DID come back, so a
// wrong-reason failure names both.
template <class T>
inline void refused(const Result<T>& r, Err want, const std::string& what) {
    if (r.has_value()) {
        std::printf("  FAIL  %s\n        got  accepted\n        want %s\n", what.c_str(),
                    std::string(name(want)).c_str());
        ++g_fail;
        return;
    }
    check_eq(std::string(name(r.error().code)), std::string(name(want)), what);
}

template <class T>
inline void accepted(const Result<T>& r, const std::string& what) {
    if (!r.has_value()) {
        std::printf("  FAIL  %s\n        refused: %s\n", what.c_str(),
                    r.error().message().c_str());
        ++g_fail;
        return;
    }
    check(true, what);
}

}  // namespace lux::fhevm::test
