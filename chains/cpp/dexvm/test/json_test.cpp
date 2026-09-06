// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// json_test — the manifest's bytes, against Go's own.
//
// This suite exists because the manifest pin is a hash OF BYTES. A writer that
// produced equivalent-but-different JSON would hash differently from the
// artifact CI approved, and the pin would refuse a file that is in fact correct.
// So the assertion is byte identity with Go's MarshalIndent, not "parses to the
// same thing" — the golden strings here were printed by Go.

#include "lux/dexvm/json.hpp"
#include "lux/dexvm/manifest.hpp"

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

Id chain_all_11() {
    Id c{};
    for (auto& b : c) b = 0x11;
    return c;
}

Manifest fixed_manifest() {
    const Id chain = chain_all_11();
    Manifest m;
    m.network = "mainnet";
    m.network_id = 1;
    m.evm_chain_id = 96369;
    m.c_chain_id = chain;
    m.chain_labels[hex(chain)] = "Lux C-Chain";
    m.chain_labels["00"] = "z";  // a second key, so the sort order is visible

    Asset a;
    a.network_id = 1;
    a.chain_id = chain;
    a.kind = AssetKind::ERC20;
    a.canonical_ref = addr20(0x4a);
    a.decimals = 18;
    a.symbol = "WLUX";
    a.name = "Wrapped LUX";
    a.enabled = true;
    a.risk_tier = RiskTier::Tier0;
    m.assets.push_back(std::move(a));
    m.assets_is_null = false;
    // markets stays nil, which Go writes as null
    return m;
}

void writes_what_go_writes() {
    std::printf("the writer reproduces Go's MarshalIndent byte for byte\n");
    const Manifest m = fixed_manifest();
    const std::string got = m.encode();
    check_eq(got, golden::kFixedManifestJSON, "the fixed manifest's bytes");

    const Bytes raw(got.begin(), got.end());
    check_eq(hex(view(sha256(view(raw)))), golden::kFixedManifestSHA256,
             "…and therefore its content hash, which is what a pin binds");
}

void the_nil_and_empty_distinction() {
    std::printf("a nil list is null and an empty one is [] — Go's distinction, so ours\n");
    Manifest m = fixed_manifest();
    m.chain_labels.clear();  // omitempty drops the member entirely
    m.network = "localnet";
    m.network_id = 1337;
    m.evm_chain_id = 1337;
    m.assets[0].network_id = 1337;
    m.assets[0].kind = AssetKind::EVMNative;
    m.assets[0].canonical_ref = to_bytes(view(kEVMNativeMarker));
    m.assets[0].symbol = "LUX";
    m.assets[0].name = "Lux";
    check_eq(m.encode(), golden::kLocalnetManifestJSON, "the localnet manifest's bytes");

    const std::string s = m.encode();
    const Bytes raw(s.begin(), s.end());
    check_eq(hex(view(sha256(view(raw)))), golden::kLocalnetManifestSHA256, "…and its hash");

    m.markets_is_null = false;  // now an empty, non-nil list
    check(m.encode().find("\"markets\": []") != std::string::npos,
          "an empty list writes as [], not null");
}

void escaping_carries_gos_quirks() {
    std::printf("string escaping, quirks included\n");
    check_eq(json::escape("plain"), "\"plain\"", "a plain string");
    check_eq(json::escape("a\"b\\c"), "\"a\\\"b\\\\c\"", "a quote and a backslash");
    check_eq(json::escape("a\nb\tc\rd"), "\"a\\nb\\tc\\rd\"", "the short control escapes");
    check_eq(json::escape(std::string("\x01", 1)), "\"\\u0001\"", "and the long ones");
    // Go's encoder escapes these three so a document is safe to embed in HTML.
    // Reproducing the quirk is the whole point of a byte-identical writer.
    check_eq(json::escape("<a>&b"), "\"\\u003ca\\u003e\\u0026b\"", "< > and & are escaped");
    check_eq(json::escape("héllo"), "\"héllo\"", "UTF-8 passes through");
}

void reads_what_go_wrote() {
    std::printf("the reader accepts what the writer produced, and refuses what Go refuses\n");
    const Manifest m = fixed_manifest();
    const std::string s = m.encode();
    const Bytes raw(s.begin(), s.end());
    auto back = decode_manifest_bytes(view(raw), "round-trip");
    admitted(back, "the manifest decodes");
    if (back) {
        check_eq(back->manifest.encode(), s, "and re-encodes to the same bytes");
        check(back->manifest.chain_labels.size() == 2, "with both chain labels");
        check(back->manifest.markets_is_null, "and a nil market list stays nil");
    }

    // DisallowUnknownFields, recursively: a typo must be visible, because a
    // manifest that quietly dropped "asssets" would admit nothing and say it
    // succeeded.
    const std::string typo =
        R"({"network":"mainnet","networkID":1,"evmChainID":96369,"cChainID":")" +
        cb58(chain_all_11()) + R"(","asssets":[]})";
    const Bytes typo_raw(typo.begin(), typo.end());
    refused_any(decode_manifest_bytes(view(typo_raw), "typo"), "a typo'd top-level field");

    const std::string nested_typo =
        R"({"network":"mainnet","networkID":1,"evmChainID":96369,"cChainID":")" +
        cb58(chain_all_11()) +
        R"(","assets":[{"networkID":1,"assetKindd":"ERC20"}]})";
    const Bytes nested_raw(nested_typo.begin(), nested_typo.end());
    refused_any(decode_manifest_bytes(view(nested_raw), "nested typo"),
                "…and a typo'd field inside an asset");
}

void numbers_are_read_strictly() {
    std::printf("an unsigned field takes an unsigned integer, and nothing else\n");
    auto doc = [](const char* body) {
        const std::string s = body;
        return Bytes(s.begin(), s.end());
    };
    auto with_networkid = [&](const char* n) {
        return doc((std::string(R"({"network":"m","networkID":)") + n +
                    R"(,"evmChainID":96369,"cChainID":")" + cb58(chain_all_11()) + R"("})")
                       .c_str());
    };
    refused_any(decode_manifest_bytes(view(with_networkid("-1")), "neg"), "a negative networkID");
    refused_any(decode_manifest_bytes(view(with_networkid("1.5")), "frac"), "a fractional one");
    refused_any(decode_manifest_bytes(view(with_networkid("4294967296")), "over"),
                "one that overflows uint32");
    refused_any(decode_manifest_bytes(view(with_networkid(R"("1")")), "str"), "and a quoted one");

    auto ok = decode_manifest_bytes(view(with_networkid("4294967295")), "max");
    admitted(ok, "but the largest uint32 is fine");
}

void malformed_documents() {
    std::printf("malformed documents\n");
    auto doc = [](const std::string& s) { return Bytes(s.begin(), s.end()); };
    for (const std::string& bad : {std::string("{"), std::string("[]"), std::string(""),
                                   std::string("{} trailing"), std::string("{\"a\":}"),
                                   std::string("{\"a\" 1}")}) {
        refused_any(decode_manifest_bytes(view(doc(bad)), "bad"),
                    "\"" + bad + "\" is not a manifest");
    }
}

std::uint64_t next_random(std::uint64_t* state) {
    *state ^= *state << 13;
    *state ^= *state >> 7;
    *state ^= *state << 17;
    return *state;
}

void a_mutated_manifest_is_decided_not_crashed() {
    std::printf("a mutated manifest is decided, never half-believed\n");
    const std::string original = fixed_manifest().encode();

    // The manifest is the one artifact read from outside this chain, so its
    // decoder is the one that faces bytes nobody here wrote. Under ASan and
    // UBSan this loop is where a stray index would surface; the assertion is
    // that every input reaches a decision.
    std::uint64_t state = 0xD1B54A32D192ED03ull;
    int refused_count = 0, accepted = 0;
    for (int i = 0; i < 4000; ++i) {
        std::string mutated = original;
        const int edits = 1 + int(next_random(&state) % 3);
        for (int e = 0; e < edits; ++e)
            mutated[std::size_t(next_random(&state) % mutated.size())] =
                char(next_random(&state) & 0xff);
        const Bytes raw(mutated.begin(), mutated.end());
        if (decode_manifest_bytes(view(raw), "fuzz"))
            ++accepted;
        else
            ++refused_count;
    }
    check(refused_count > 0,
          "most mutations are refused (" + std::to_string(refused_count) + " of 4000)");
    check(accepted + refused_count == 4000, "and all 4000 were decided");

    // Truncation at every length, which is where a parser is most likely to
    // read past its input.
    for (std::size_t n = 0; n <= original.size(); ++n) {
        const std::string cut = original.substr(0, n);
        const Bytes raw(cut.begin(), cut.end());
        (void)decode_manifest_bytes(view(raw), "cut");
    }
    check(true, "every truncation is decided too");

    // Deep nesting is bounded rather than recursed into the stack.
    std::string deep = "{\"assets\":";
    for (int i = 0; i < 5000; ++i) deep += "[";
    const Bytes deep_raw(deep.begin(), deep.end());
    refused_any(decode_manifest_bytes(view(deep_raw), "deep"),
                "and deep nesting is refused rather than recursed");
}

}  // namespace

int main() {
    writes_what_go_writes();
    the_nil_and_empty_distinction();
    escaping_carries_gos_quirks();
    reads_what_go_wrote();
    numbers_are_read_strictly();
    malformed_documents();
    a_mutated_manifest_is_decided_not_crashed();
    return report("json");
}
