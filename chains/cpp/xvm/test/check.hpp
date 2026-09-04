// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// check.hpp — the whole test harness: a counter, a line per assertion, and a
// non-zero exit when something failed. A test that cannot run prints SKIP and
// still fails the run, because a silent skip reads as a pass.

#pragma once

#include "lux/xvm/id.hpp"

#include <cstdio>
#include <string>

namespace lux::xvm::test {

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
inline std::string hex_of(ByteView b) {
    std::string s;
    s.reserve(b.size() * 2);
    char t[3];
    for (auto c : b) {
        std::snprintf(t, sizeof(t), "%02x", c);
        s += t;
    }
    return s;
}
inline std::string hex_of(const Bytes& b) { return hex_of(view(b)); }
inline std::string hex_of(const Id& b) { return hex_of(view(b)); }

// from_hex turns a golden vector back into bytes, so a test can feed the GO
// bytes into the C++ parser — which is the other half of byte fidelity.
inline Bytes from_hex(const std::string& s) {
    Bytes out;
    out.reserve(s.size() / 2);
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    for (std::size_t i = 0; i + 1 < s.size(); i += 2) {
        int hi = nib(s[i]), lo = nib(s[i + 1]);
        if (hi < 0 || lo < 0) return {};
        out.push_back(std::uint8_t((hi << 4) | lo));
    }
    return out;
}

}  // namespace lux::xvm::test
