// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// harness.hpp — the smallest thing that can run a ported Go test honestly.
//
// The Go suite says `require.Equal(expected, got)` and `require.ErrorIs(err,
// ErrX)`; the ported test says the same, so the two can be read side by side.
// A test that cannot run PRINTS `skipped` and says why — a skip is never
// reported as a pass, and the binary's exit code is nonzero only for real
// failures, never for a skip that was declared.

#pragma once

#include "lux/platformvm/error.hpp"

#include <cstdio>
#include <cstdlib>
#include <functional>
#include <string>
#include <string_view>
#include <vector>

namespace pvmtest {

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

}  // namespace pvmtest

#define PVM_CONCAT_(a, b) a##b
#define PVM_CONCAT(a, b) PVM_CONCAT_(a, b)

// TEST(Name) { ... } — one ported Go test function.
#define TEST(name)                                                          \
    static void PVM_CONCAT(pvm_case_, name)();                              \
    static ::pvmtest::Register PVM_CONCAT(pvm_reg_, name)(#name,            \
                                                          PVM_CONCAT(pvm_case_, name)); \
    static void PVM_CONCAT(pvm_case_, name)()

#define REQUIRE(cond)                                                                        \
    do {                                                                                     \
        if (!(cond)) {                                                                       \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +   \
                                    "  require(" #cond ")");                                 \
            return;                                                                          \
        }                                                                                    \
    } while (0)

#define REQUIRE_MSG(cond, msg)                                                             \
    do {                                                                                   \
        if (!(cond)) {                                                                     \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) + \
                                    "  " + (msg));                                         \
            return;                                                                        \
        }                                                                                  \
    } while (0)

#define REQUIRE_EQ(want, got)                                                                     \
    do {                                                                                          \
        const auto& w_ = (want);                                                                  \
        const auto& g_ = (got);                                                                   \
        if (!(w_ == g_)) {                                                                        \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +        \
                                    "  expected " #want " == " #got);                             \
            return;                                                                                \
        }                                                                                          \
    } while (0)

// Numeric equality with both values printed — the common case where seeing the
// two numbers is the whole diagnosis.
#define REQUIRE_EQ_NUM(want, got)                                                                  \
    do {                                                                                           \
        const auto w_ = static_cast<long long>(want);                                              \
        const auto g_ = static_cast<long long>(got);                                               \
        if (w_ != g_) {                                                                            \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +         \
                                    "  expected " + std::to_string(w_) + ", got " +                \
                                    std::to_string(g_));                                           \
            return;                                                                                \
        }                                                                                          \
    } while (0)

#define REQUIRE_U64(want, got)                                                                     \
    do {                                                                                           \
        const std::uint64_t w_ = (want);                                                           \
        const std::uint64_t g_ = (got);                                                            \
        if (w_ != g_) {                                                                            \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +         \
                                    "  expected " + std::to_string(w_) + ", got " +                \
                                    std::to_string(g_));                                           \
            return;                                                                                \
        }                                                                                          \
    } while (0)

// require.NoError
#define REQUIRE_OK(expr)                                                                          \
    do {                                                                                          \
        auto r_ = (expr);                                                                         \
        if (!r_.has_value()) {                                                                    \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +        \
                                    "  unexpected error: " + r_.error().message());               \
            return;                                                                               \
        }                                                                                         \
    } while (0)

// require.ErrorIs
#define REQUIRE_ERR(expr, want)                                                                   \
    do {                                                                                          \
        auto r_ = (expr);                                                                         \
        const ::lux::platformvm::Err w_ = (want);                                                 \
        if (r_.has_value()) {                                                                     \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +        \
                                    "  expected error " +                                         \
                                    std::string(::lux::platformvm::err_name(w_)) + ", got success"); \
            return;                                                                               \
        }                                                                                         \
        if (r_.error().code != w_) {                                                              \
            ::pvmtest::fail_current(std::string(__FILE__ ":") + std::to_string(__LINE__) +        \
                                    "  expected error " +                                         \
                                    std::string(::lux::platformvm::err_name(w_)) + ", got " +     \
                                    r_.error().message());                                        \
            return;                                                                               \
        }                                                                                         \
    } while (0)

#define SKIP(why)                       \
    do {                                \
        ::pvmtest::skip_current(why);   \
        return;                         \
    } while (0)
