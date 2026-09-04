// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// harness.cpp — run the registered cases and report honestly.

#include "harness.hpp"

#include <iostream>

namespace pvmtest {
namespace {
bool g_failed = false;
bool g_skipped = false;
std::string g_reason;
}  // namespace

std::vector<Case>& registry() {
    static std::vector<Case> r;
    return r;
}

void fail_current(const std::string& msg) {
    g_failed = true;
    g_reason = msg;
}

void skip_current(const std::string& why) {
    g_skipped = true;
    g_reason = why;
}

int run_all(std::string_view suite) {
    int passed = 0, failed = 0, skipped = 0;
    for (auto& c : registry()) {
        g_failed = false;
        g_skipped = false;
        g_reason.clear();
        c.body();
        if (g_failed) {
            ++failed;
            std::cout << "FAIL  " << c.name << "\n      " << g_reason << "\n";
        } else if (g_skipped) {
            ++skipped;
            std::cout << "skipped  " << c.name << "  (" << g_reason << ")\n";
        } else {
            ++passed;
            std::cout << "ok    " << c.name << "\n";
        }
    }
    std::cout << "--- " << suite << ": " << passed << " passed, " << failed << " failed, " << skipped
              << " skipped\n";
    return failed == 0 ? 0 : 1;
}

}  // namespace pvmtest

int main(int argc, char** argv) { return pvmtest::run_all(argc > 0 ? argv[0] : "suite"); }
