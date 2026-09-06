// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// check.hpp — the whole test harness: a counter, a line per assertion, and a
// non-zero exit when something failed. A test that cannot run prints SKIP and
// still fails the run, because a silent skip reads as a pass.

#pragma once

#include "lux/dexvm/id.hpp"

#include <cstdio>
#include <string>

namespace lux::dexvm::test {

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

inline void skip(const std::string& what, const std::string& why) {
    std::printf("  SKIP  %s (%s)\n", what.c_str(), why.c_str());
    ++g_fail;  // a skip is not a pass
}

inline int report(const char* suite) {
    std::printf("\n%s: %d passed, %d failed\n", suite, g_pass, g_fail);
    return g_fail == 0 ? 0 : 1;
}

// refused is the shape almost every assertion here takes: the reference answers
// errors.Is(err, ErrX), so a port that only checked "something went wrong" would
// pass while refusing for the wrong reason.
template <class T>
inline void refused(const Result<T>& r, Err want, const std::string& what) {
    if (r.has_value()) {
        std::printf("  FAIL  %s\n        got  admitted, want a refusal\n", what.c_str());
        ++g_fail;
        return;
    }
    if (!r.error().is(want)) {
        std::printf("  FAIL  %s\n        got  %s\n        want the sentinel refusal\n",
                    what.c_str(), r.error().text.c_str());
        ++g_fail;
        return;
    }
    std::printf("  ok    %s\n", what.c_str());
    ++g_pass;
}

// refused_any is for the reference's unwrapped fmt.Errorf refusals, which carry
// no sentinel to match — the assertion is then that it refused at all.
template <class T>
inline void refused_any(const Result<T>& r, const std::string& what) {
    const bool ok = !r.has_value();
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (ok) {
        ++g_pass;
    } else {
        std::printf("        got  admitted, want a refusal\n");
        ++g_fail;
    }
}

template <class T>
inline void admitted(const Result<T>& r, const std::string& what) {
    const bool ok = r.has_value();
    std::printf("  %s  %s\n", ok ? "ok  " : "FAIL", what.c_str());
    if (ok) {
        ++g_pass;
    } else {
        std::printf("        got  %s\n        want it admitted\n", r.error().text.c_str());
        ++g_fail;
    }
}

}  // namespace lux::dexvm::test
