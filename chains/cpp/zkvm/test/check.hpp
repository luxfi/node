// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// check.hpp — the whole test harness: a counter, a line per assertion, and a
// non-zero exit when something failed. A test that cannot run prints SKIP and
// still fails the run, because a silent skip reads as a pass.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/wire.hpp"

#include <cstdio>
#include <string>

namespace lux::zkvm::test {

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
    const bool ok = got == want;
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (!ok) {
        std::printf("        got  %s\n        want %s\n", got.c_str(), want.c_str());
        ++g_fail;
    } else {
        ++g_pass;
    }
}

// check_err asserts a refusal AND names it: a test that only asserts "an error"
// passes when the code refuses for an entirely different reason.
template <class T>
inline void check_err(const wire::Result<T>& r, const std::string& contains,
                      const std::string& what) {
    if (r.has_value()) {
        std::printf("  FAIL  %s\n        got no error, want one containing %s\n", what.c_str(),
                    contains.c_str());
        ++g_fail;
        return;
    }
    const bool ok = r.error().find(contains) != std::string::npos;
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (!ok) {
        std::printf("        got  %s\n        want it to contain %s\n", r.error().c_str(),
                    contains.c_str());
        ++g_fail;
    } else {
        ++g_pass;
    }
}

template <class T>
inline void check_ok(const wire::Result<T>& r, const std::string& what) {
    const bool ok = r.has_value();
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (!ok) {
        std::printf("        got error: %s\n", r.error().c_str());
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
inline std::string hex_of(ByteView b) { return hex(b); }
inline std::string hex_of(const Bytes& b) { return hex(view(b)); }
inline std::string hex_of(const Id& b) { return hex(view(b)); }

// from_hex turns a golden vector back into bytes, so a test can feed the GO
// bytes into the C++ parser — which is the other half of byte fidelity.
inline Bytes from_hex(const std::string& s) { return unhex(s); }

}  // namespace lux::zkvm::test
