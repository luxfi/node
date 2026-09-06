// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// pin_test — the content-hash binding, ported from
// chains/dexvm/registry/manifest_pin_test.go.
//
// The decisive case is the middle one: an attacker edits a manifest to point an
// asset at a fabricated token address, and the edited file is STILL structurally
// valid — so shape validation alone accepts it. The pin is what refuses it. That
// matters because a node holds no EVM state of its own to re-check the token
// with; the hash is the whole binding to what CI approved.

#include "lux/dexvm/manifest.hpp"

#include "check.hpp"
#include "fixtures.hpp"

#include <string>
#include <unistd.h>

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

class TempFile {
public:
    explicit TempFile(const std::string& contents) {
        char tmpl[] = "/tmp/dexvm-pin-XXXXXX";
        const int fd = ::mkstemp(tmpl);
        path_ = tmpl;
        if (fd >= 0) {
            const ssize_t n = ::write(fd, contents.data(), contents.size());
            (void)n;
            ::close(fd);
        }
    }
    ~TempFile() { ::unlink(path_.c_str()); }
    void rewrite(const std::string& contents) const {
        std::FILE* f = std::fopen(path_.c_str(), "wb");
        if (!f) return;
        std::fwrite(contents.data(), 1, contents.size(), f);
        std::fclose(f);
    }
    const std::string& path() const { return path_; }

private:
    std::string path_;
};

Manifest one_asset(const Id& c_chain, const Bytes& ref) {
    Manifest m;
    m.network = "mainnet";
    m.network_id = 1;
    m.evm_chain_id = 96369;
    m.c_chain_id = c_chain;
    m.assets_is_null = false;
    m.assets.push_back(Asset{1, c_chain, AssetKind::ERC20, ref, 18, "WLUX", "Wrapped LUX", true,
                             RiskTier::Tier0});
    return m;
}

std::string sha256_hex(const std::string& s) {
    const Bytes raw(s.begin(), s.end());
    return hex(view(sha256(view(raw))));
}

void matching_hash_loads() {
    std::printf("a manifest matching its pin loads\n");
    const Id c_chain = test_id(200);
    const std::string bytes = one_asset(c_chain, addr20(0x4a)).encode();
    TempFile file(bytes);
    const std::string sum = sha256_hex(bytes);

    auto m = load_manifest_pinned(file.path(), sum);
    admitted(m, "the CI-approved artifact loads against its pin");
    if (m) check(m->assets.size() == 1, "with its one asset");

    // The common prefixes and casings a config carries.
    admitted(load_manifest_pinned(file.path(), "0x" + sum), "a 0x-prefixed pin");
    std::string upper = sum;
    for (char& c : upper) c = char(std::toupper(static_cast<unsigned char>(c)));
    admitted(load_manifest_pinned(file.path(), "sha256:" + upper),
             "a sha256:-prefixed uppercase pin");
}

void an_edited_manifest_is_refused() {
    std::printf("an edited manifest is refused — the decisive case\n");
    const Id c_chain = test_id(210);
    const std::string approved = one_asset(c_chain, addr20(0x4a)).encode();
    TempFile file(approved);
    const std::string ci_hash = sha256_hex(approved);

    // The attacker swaps the real token for a fabricated address. The file stays
    // structurally valid, so shape validation alone would accept it.
    const std::string tampered = one_asset(c_chain, addr20(0xEE)).encode();
    file.rewrite(tampered);
    admitted(load_manifest(file.path()),
             "the tampered file is STILL structurally valid — shape alone is not enough");

    auto pinned = load_manifest_pinned(file.path(), ci_hash);
    refused(pinned, Err::ManifestHashMismatch, "but the pin refuses it");

    // Restoring the approved bytes makes it load again, so the refusal is about
    // the content and not about the file.
    file.rewrite(approved);
    admitted(load_manifest_pinned(file.path(), ci_hash), "and the approved bytes load again");
}

void a_malformed_pin_fails_closed() {
    std::printf("you cannot pin against garbage\n");
    const Id c_chain = test_id(220);
    TempFile file(one_asset(c_chain, addr20(0x4a)).encode());
    for (const std::string& bad : {std::string("deadbeef"), std::string(64, 'z'),
                                   std::string(63, 'a'), std::string(65, 'a')}) {
        refused_any(load_manifest_pinned(file.path(), bad),
                    "a malformed pin (" + std::to_string(bad.size()) + " chars)");
    }
    refused_any(normalize_sha256("0xnothex"), "and normalising one is itself an error");
}

void an_empty_pin_is_opt_in() {
    std::printf("pinning is opt-in, so an empty pin preserves the unpinned path\n");
    const Id c_chain = test_id(230);
    TempFile file(one_asset(c_chain, addr20(0x4a)).encode());
    admitted(load_manifest_pinned(file.path(), ""), "an empty pin falls back to shape-only");
}

}  // namespace

int main() {
    matching_hash_loads();
    an_edited_manifest_is_refused();
    a_malformed_pin_fails_closed();
    an_empty_pin_is_opt_in();
    return report("pin");
}
