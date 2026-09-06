// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ D-chain's answers to the shared corpus.
//
// One of the evaluators — Go, Rust and C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// D IS NOT A BLOCK CHAIN IN THIS CORPUS. Go's dexvm is a REGISTRY, and what it
// decides is identity and admission — what an asset IS, what a market IS, which
// kinds may be registered, and whether native value may activate at all. Those
// are consensus decisions with no wire of their own: two implementations
// deriving different bytes for one asset have forked the value plane without
// ever disagreeing about a transaction.
//
// So a D vector's wire column carries the ARGUMENTS, pipe-separated ASCII, and
// the op says which function is being asked:
//
//   assetid   networkID | sourceChainHex | kindToken | refHex
//   marketid  networkID | baseHex | quoteHex | venueHex
//   kind      token
//   mode      token
//   class     networkID
//   guard     valueEnabled | modeToken | capsOn | realAssetsOnly | haltReady
//
// Usage: dexvm_conformance <vectors.tsv>

#include "lux/conformance/corpus.hpp"

#include "lux/dexvm/asset.hpp"
#include "lux/dexvm/consensus_mode.hpp"
#include "lux/dexvm/gate.hpp"

#include <cstdio>
#include <string>
#include <vector>

using namespace lux::dexvm;
namespace conf = lux::conformance;

namespace {

// The class of a refusal, decided on its SENTINEL rather than on the sentence
// it was wrapped in.
//
// Go's classify unwraps to the deepest error before it reads any words, and
// prints the wrapper in the note beside it. The C++ Error carries the same
// identity as a code whose text_of() IS the Go sentinel, so reading that is
// the same question. Reading the wrapped text instead is a different one, and
// it answers differently: "canonical reference does not match asset kind: UTXO
// assetID must be 32 bytes" is a syntactic refusal whose DETAIL contains the
// word utxo, and the ledger arm claims it.
// Err::Other is this port's "no sentinel" — the refusal was raised with words
// and no identity, which is Go's fmt.Errorf without a %w. root() returns such
// an error unchanged, so its own text IS what gets classified, on both sides.
std::string classify_of(const Error& e) {
    if (e.code == Err::Other) return conf::classify(e.text);
    return conf::classify(std::string(text_of(e.code)));
}

std::vector<std::string> split(const std::string& s, char sep) {
    std::vector<std::string> out;
    std::size_t start = 0;
    for (;;) {
        const std::size_t at = s.find(sep, start);
        if (at == std::string::npos) {
            out.push_back(s.substr(start));
            return out;
        }
        out.push_back(s.substr(start, at - start));
        start = at + 1;
    }
}

bool parse_u32(const std::string& s, std::uint32_t& out) {
    if (s.empty()) return false;
    std::uint64_t v = 0;
    for (const char c : s) {
        if (c < '0' || c > '9') return false;
        v = v * 10 + static_cast<std::uint64_t>(c - '0');
        if (v > 0xFFFFFFFFull) return false;
    }
    out = static_cast<std::uint32_t>(v);
    return true;
}

bool parse_bool(const std::string& s, bool& out) {
    if (s == "true") {
        out = true;
        return true;
    }
    if (s == "false") {
        out = false;
        return true;
    }
    return false;
}

bool parse_id(const std::string& s, Id& out) {
    std::vector<std::uint8_t> b;
    if (!conf::unhex(s, b) || b.size() != out.size()) return false;
    std::copy(b.begin(), b.end(), out.begin());
    return true;
}

// A vector whose ARGUMENTS do not read. That is this evaluator's failure, not
// the chain's, and it is reported as such rather than as a refusal the chain
// never made.
conf::Row bad_input(conf::Row r, const std::string& why) {
    r.parse = conf::kInternal;
    r.syntactic = conf::kInternal;
    r.exec = conf::kInternal;
    r.note = why;
    return r;
}

conf::Row eval_assetid(conf::Row r, const std::vector<std::string>& a) {
    r.kind = "AssetID";
    if (a.size() != 4) return bad_input(r, "assetid wants 4 arguments");
    std::uint32_t network = 0;
    Id chain{};
    std::vector<std::uint8_t> ref;
    if (!parse_u32(a[0], network)) return bad_input(r, "network id: " + a[0]);
    if (!parse_id(a[1], chain)) return bad_input(r, "source chain: " + a[1]);
    if (!conf::unhex(a[3], ref)) return bad_input(r, "reference: " + a[3]);

    // The kind token goes through the registry's own parser, so a token it
    // refuses is refused at the layer a manifest would meet it.
    auto kind = parse_kind(a[2]);
    if (!kind) {
        r.syntactic = classify_of(kind.error());
        r.exec = r.syntactic;
        r.note = kind.error().text;
        return r;
    }
    r.syntactic = conf::kOk;

    auto asset = derive_asset_id(network, chain, kind.value(), ref);
    if (!asset) {
        r.exec = classify_of(asset.error());
        r.note = asset.error().text;
        return r;
    }
    const Id& v = asset.value();
    r.hash = conf::hex(v.data(), v.size());
    r.exec = conf::kOk;
    r.note = "derived";
    return r;
}

conf::Row eval_marketid(conf::Row r, const std::vector<std::string>& a) {
    r.kind = "MarketID";
    if (a.size() != 4) return bad_input(r, "marketid wants 4 arguments");
    std::uint32_t network = 0;
    Id base{}, quote{};
    std::vector<std::uint8_t> venue;
    if (!parse_u32(a[0], network)) return bad_input(r, "network id: " + a[0]);
    if (!parse_id(a[1], base)) return bad_input(r, "base asset: " + a[1]);
    if (!parse_id(a[2], quote)) return bad_input(r, "quote asset: " + a[2]);
    if (!conf::unhex(a[3], venue)) return bad_input(r, "venue: " + a[3]);

    r.syntactic = conf::kOk;
    const Id m = market_id(network, base, quote, venue);
    r.hash = conf::hex(m.data(), m.size());
    r.exec = conf::kOk;
    r.note = "derived";
    return r;
}

conf::Row eval_kind(conf::Row r, const std::vector<std::string>& a) {
    r.kind = "AssetKind";
    if (a.size() != 1) return bad_input(r, "kind wants 1 argument");
    auto k = parse_kind(a[0]);
    if (!k) {
        r.syntactic = classify_of(k.error());
        r.exec = r.syntactic;
        r.note = k.error().text;
        return r;
    }
    r.syntactic = conf::kOk;
    r.exec = conf::kOk;
    r.note = std::string(to_string(k.value()));
    return r;
}

conf::Row eval_mode(conf::Row r, const std::vector<std::string>& a) {
    r.kind = "ConsensusMode";
    if (a.size() != 1) return bad_input(r, "mode wants 1 argument");
    auto m = parse_consensus_mode(a[0]);
    if (!m) {
        r.syntactic = classify_of(m.error());
        r.exec = r.syntactic;
        r.note = m.error().text;
        return r;
    }
    r.syntactic = conf::kOk;
    r.exec = conf::kOk;
    r.note = std::string(to_string(m.value()));
    return r;
}

conf::Row eval_class(conf::Row r, const std::vector<std::string>& a) {
    r.kind = "NetworkClass";
    if (a.size() != 1) return bad_input(r, "class wants 1 argument");
    std::uint32_t network = 0;
    if (!parse_u32(a[0], network)) return bad_input(r, "network id: " + a[0]);
    r.syntactic = conf::kOk;
    r.exec = conf::kOk;
    r.note = std::string(to_string(network_class_for(network)));
    return r;
}

conf::Row eval_guard(conf::Row r, const std::vector<std::string>& a) {
    r.kind = "ValueActivation";
    if (a.size() != 5) return bad_input(r, "guard wants 5 arguments");
    bool enabled = false, caps = false, real = false, halt = false;
    if (!parse_bool(a[0], enabled)) return bad_input(r, "value enabled: " + a[0]);
    if (!parse_bool(a[2], caps)) return bad_input(r, "caps: " + a[2]);
    if (!parse_bool(a[3], real)) return bad_input(r, "real assets: " + a[3]);
    if (!parse_bool(a[4], halt)) return bad_input(r, "halt ready: " + a[4]);

    // An unparseable mode token is UNSET, because that is what a caller who
    // declared nothing legible has declared — and UNSET is refused. Reading it
    // as an error here would report the parse rather than the guard.
    ConsensusMode mode = ConsensusMode::Unset;
    if (auto m = parse_consensus_mode(a[1])) mode = m.value();
    r.syntactic = conf::kOk;

    auto st = guard_value_activation(enabled, mode, LaunchAssertions{caps, real, halt});
    if (!st) {
        r.exec = classify_of(st.error());
        r.note = st.error().text;
        return r;
    }
    r.exec = conf::kOk;
    const std::string mode_name(to_string(st.value().mode));
    r.note = st.value().status.empty() ? mode_name + " (no disclaimer)"
                                       : mode_name + " " + st.value().status;
    return r;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: dexvm_conformance <vectors.tsv>\n");
        return 2;
    }
    std::vector<conf::Vector> vectors;
    if (!conf::read(argv[1], "dexvm_conformance", vectors)) return 1;

    for (const auto& v : vectors) {
        if (v.chain != "D") continue;
        conf::Row r;
        r.id = v.id;
        r.parse = "ok";
        // The wire column is hex, always — a D vector's arguments are ASCII,
        // but they travel the same column a block's bytes do, so they are
        // hex-encoded like everything else. Splitting the column itself finds
        // one field whose text is "4552433230".
        std::vector<std::uint8_t> raw;
        if (!conf::unhex(v.wire, raw)) {
            conf::print(bad_input(r, "corpus wire is not hex"));
            continue;
        }
        const std::vector<std::string> args =
            split(std::string(raw.begin(), raw.end()), '|');

        if (v.op == "assetid") {
            conf::print(eval_assetid(r, args));
        } else if (v.op == "marketid") {
            conf::print(eval_marketid(r, args));
        } else if (v.op == "kind") {
            conf::print(eval_kind(r, args));
        } else if (v.op == "mode") {
            conf::print(eval_mode(r, args));
        } else if (v.op == "class") {
            conf::print(eval_class(r, args));
        } else if (v.op == "guard") {
            conf::print(eval_guard(r, args));
        } else {
            std::fprintf(stderr, "dexvm_conformance: unknown op %s on %s\n", v.op.c_str(),
                         v.id.c_str());
            return 1;
        }
    }
    return 0;
}
