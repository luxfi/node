// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// harness.hpp — the smallest thing that can run a ported Go test honestly.
//
// The Go suite says `require.Equal(expected, got)` and `require.ErrorIs(err,
// errX)`; the ported test says the same, so the two can be read side by side. A
// test that cannot run PRINTS `skipped` and says why — a skip is never reported
// as a pass, and the binary's exit code is nonzero only for real failures,
// never for a skip that was declared.

#pragma once

#include "lux/quantumvm/error.hpp"

#include <cstdio>
#include <functional>
#include <string>
#include <string_view>
#include <vector>

namespace qvmtest {

struct Case {
    std::string name;
    std::function<void()> body;
};

std::vector<Case>& registry();
void fail_current(const std::string& msg);
void skip_current(const std::string& why);
int run_all(std::string_view suite);

struct Register {
    Register(const char* name, std::function<void()> body) { registry().push_back({name, std::move(body)}); }
};

}  // namespace qvmtest

#define QVM_CONCAT_(a, b) a##b
#define QVM_CONCAT(a, b) QVM_CONCAT_(a, b)

// TEST(Name) { ... } — one ported Go test function.
#define TEST(name)                                                                       \
    static void QVM_CONCAT(qvm_case_, name)();                                           \
    static ::qvmtest::Register QVM_CONCAT(qvm_reg_, name)(#name, QVM_CONCAT(qvm_case_, name)); \
    static void QVM_CONCAT(qvm_case_, name)()

#define REQUIRE(cond)                                                                      \
    do {                                                                                   \
        if (!(cond)) {                                                                     \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) + \
                                    "  require(" #cond ")");                               \
            return;                                                                        \
        }                                                                                  \
    } while (0)

#define REQUIRE_MSG(cond, msg)                                                             \
    do {                                                                                   \
        if (!(cond)) {                                                                     \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) + \
                                    "  " + (msg));                                         \
            return;                                                                        \
        }                                                                                  \
    } while (0)

#define REQUIRE_EQ(want, got)                                                                  \
    do {                                                                                       \
        const auto& _w = (want);                                                               \
        const auto& _g = (got);                                                                \
        if (!(_w == _g)) {                                                                     \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +     \
                                    "  " #want " != " #got);                                   \
            return;                                                                            \
        }                                                                                      \
    } while (0)

// REQUIRE_ERR(expr, code) — the Go suite's require.ErrorIs: this input is
// refused for THIS reason, not merely refused.
#define REQUIRE_ERR(expr, want_code)                                                            \
    do {                                                                                        \
        auto&& _r = (expr);                                                                       \
        if (_r) {                                                                               \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +      \
                                    "  " #expr " succeeded, wanted " #want_code);               \
            return;                                                                             \
        }                                                                                       \
        if (_r.error().code != (want_code)) {                                                   \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +      \
                                    "  " #expr " failed with " + _r.error().message() +         \
                                    ", wanted " #want_code);                                    \
            return;                                                                             \
        }                                                                                       \
    } while (0)

#define REQUIRE_FAILS(expr)                                                                \
    do {                                                                                   \
        auto&& _r = (expr);                                                                  \
        if (_r) {                                                                          \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) + \
                                    "  " #expr " succeeded, wanted a refusal");            \
            return;                                                                        \
        }                                                                                  \
    } while (0)

#define REQUIRE_OK(expr)                                                                   \
    do {                                                                                   \
        auto&& _r = (expr);                                                                  \
        if (!_r) {                                                                         \
            ::qvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) + \
                                    "  " #expr ": " + _r.error().message());               \
            return;                                                                        \
        }                                                                                  \
    } while (0)
