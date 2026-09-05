// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "harness.hpp"

#include <cstdio>

namespace qvmtest {
namespace {

struct Current {
    bool failed = false;
    bool skipped = false;
    std::string why;
};

Current& current() {
    static Current c;
    return c;
}

}  // namespace

std::vector<Case>& registry() {
    static std::vector<Case> cases;
    return cases;
}

void fail_current(const std::string& msg) {
    current().failed = true;
    current().why = msg;
}

void skip_current(const std::string& why) {
    current().skipped = true;
    current().why = why;
}

int run_all(std::string_view suite) {
    int passed = 0, failed = 0, skipped = 0;
    std::printf("== %.*s\n", static_cast<int>(suite.size()), suite.data());

    for (const auto& c : registry()) {
        current() = Current{};
        c.body();
        if (current().skipped) {
            ++skipped;
            std::printf("  skipped  %s (%s)\n", c.name.c_str(), current().why.c_str());
        } else if (current().failed) {
            ++failed;
            std::printf("  FAIL     %s\n           %s\n", c.name.c_str(), current().why.c_str());
        } else {
            ++passed;
            std::printf("  ok       %s\n", c.name.c_str());
        }
    }

    std::printf("%.*s: %d passed, %d failed, %d skipped\n", static_cast<int>(suite.size()),
                suite.data(), passed, failed, skipped);
    return failed == 0 ? 0 : 1;
}

}  // namespace qvmtest

int main(int argc, char** argv) { return qvmtest::run_all(argc > 0 ? argv[0] : "suite"); }
