// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// corpus.hpp — what every C++ evaluator in the chain differential shares.
//
// An evaluator reads conformance/corpus/vectors.tsv, answers the vectors of ONE
// chain, and prints a result line per vector. None of that is chain knowledge:
// the tab format, the hex, the verdict words and the error-word table are the
// same in all of them, and they were copied into each one by hand until a
// classification the Go side had and the C++ side did not turned five true
// answers into five reported disagreements.
//
// So the table lives once. A chain-specific evaluator supplies only what is
// genuinely its own: how to parse its bytes, what to call the thing it parsed,
// and which of its refusals is which.
//
// It deliberately does NOT decide anything. `classify` maps words to a class
// and nothing here reads a chain; a header that knew a rule would be a fourth
// implementation, and the day it was wrong it would agree with itself.

#pragma once

#include <cctype>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#include <vector>

namespace lux::conformance {

inline constexpr const char* kNone = "-";

// The verdict vocabulary, shared with the Go and Rust evaluators. Every
// implementation maps its own error type into exactly one of these before
// printing, because "failed to fetch UTXO", `MissingUtxo` and `kUtxoNotFound`
// are three spellings of one answer.
inline constexpr const char* kOk = "OK";
inline constexpr const char* kMalformed = "MALFORMED";
inline constexpr const char* kSyntactic = "SYNTACTIC";
inline constexpr const char* kOverflow = "OVERFLOW";
inline constexpr const char* kLedger = "LEDGER";
inline constexpr const char* kAuth = "AUTH";
inline constexpr const char* kWarp = "WARP";
inline constexpr const char* kUnsupported = "UNSUPPORTED";
inline constexpr const char* kSkipped = "SKIPPED";  // never a pass
inline constexpr const char* kInternal = "INTERNAL";

// One row of the corpus: V <id> <chain> <op> <wire-hex>.
struct Vector {
    std::string id, chain, op, wire;
};

// One implementation's answer: R <id> <parse> <kind> <hash> <syntactic> <exec>
// <note>. The five fields before the note are COMPARED; the note is not, and
// carries this implementation's own words so a disagreement can be read
// without opening three debuggers.
struct Row {
    std::string id;
    std::string parse = kNone;
    std::string kind = kNone;
    std::string hash = kNone;
    std::string syntactic = kNone;
    std::string exec = kNone;
    std::string note;
};

inline void print(const Row& r) {
    std::string note = r.note;
    for (auto& c : note)
        if (c == '\t' || c == '\n') c = ' ';
    if (note.size() > 160) note.resize(160);
    if (note.empty()) note = kNone;
    std::printf("R\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.id.c_str(), r.parse.c_str(), r.kind.c_str(),
                r.hash.c_str(), r.syntactic.c_str(), r.exec.c_str(), note.c_str());
}

inline std::string hex(const std::uint8_t* b, std::size_t n) {
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(n * 2);
    for (std::size_t i = 0; i < n; ++i) {
        out.push_back(d[b[i] >> 4]);
        out.push_back(d[b[i] & 0xF]);
    }
    return out;
}

inline bool unhex(const std::string& s, std::vector<std::uint8_t>& out) {
    if (s == kNone) return true;
    if (s.size() % 2 != 0) return false;
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    out.reserve(s.size() / 2);
    for (std::size_t i = 0; i < s.size(); i += 2) {
        const int hi = nib(s[i]), lo = nib(s[i + 1]);
        if (hi < 0 || lo < 0) return false;
        out.push_back(static_cast<std::uint8_t>((hi << 4) | lo));
    }
    return true;
}

inline std::string lower(std::string s) {
    for (auto& c : s) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    return s;
}

// The word table, in the order the Go evaluator tests it — and the order is
// load-bearing. A refusal that names a kind the chain does not run usually
// lists the kinds it does, and those lists contain ledger words; testing the
// ledger group first would call every one of them a missing UTXO.
inline std::string classify(const std::string& raw) {
    const std::string s = lower(raw);
    auto has = [&](const char* w) { return s.find(w) != std::string::npos; };
    if (has("overflow") || has("underflow")) return kOverflow;
    if (has("wrong transaction type") || has("wrong tx type") || has("not permitted") ||
        has("not held") || has("unsupported") || has("forbidden") || has("unknown") ||
        has("parameter set"))
        return kUnsupported;
    if (has("credential") || has("signature") || has("unauthorized") || has("not authorised") ||
        has("not authorized") || has("proof verification failed") || has("does not match auth"))
        return kAuth;
    if (has("warp")) return kWarp;
    if (has("utxo") || has("funds") || has("insufficient") || has("burn") || has("consumed") ||
        has("produced") || has("flow") || has("fee") || has("not found") || has("doesn't exist") ||
        has("does not exist") || has("isn't a current") || has("not validator") || has("no such") ||
        has("could not load") || has("shared memory") || has("state root") ||
        has("precedes its parent") || has("skew allowance"))
        return kLedger;
    return kSyntactic;
}

// The identity row.
//
// Three chains hash something that never travels — the chain id, the network
// id — into every id they derive, so the two numbers are part of the corpus's
// contract and not of any vector. This prints the ones THIS evaluator was
// built with, never the ones the corpus asked about: a row that echoed the
// question would agree with an evaluator configured for another chain.
inline Row identity(const std::string& id, const std::uint8_t chain_byte, std::uint32_t network) {
    std::uint8_t chain[32];
    for (auto& b : chain) b = chain_byte;
    Row r;
    r.id = id;
    r.parse = "ok";
    r.kind = "ChainIdentity";
    r.hash = hex(chain, sizeof chain);
    r.syntactic = "network=" + std::to_string(network);
    r.exec = kOk;
    r.note = "the identity this evaluator derives ids under";
    return r;
}

// read splits the corpus into vectors, and returns false on a line that is not
// one. A reader that skipped what it did not understand would answer fewer
// vectors than it was given and call the silence agreement.
inline bool read(const char* path, const char* who, std::vector<Vector>& out) {
    std::ifstream in(path);
    if (!in) {
        std::fprintf(stderr, "%s: cannot read %s\n", who, path);
        return false;
    }
    std::string line;
    while (std::getline(in, line)) {
        if (line.empty() || line[0] == '#') continue;
        // Split on every tab, keeping a trailing empty column: a vector with no
        // bytes writes "-" rather than nothing, but a reader that dropped an
        // empty last field would silently shorten the row.
        std::vector<std::string> f;
        std::size_t start = 0;
        for (;;) {
            const std::size_t tab = line.find('\t', start);
            if (tab == std::string::npos) {
                f.push_back(line.substr(start));
                break;
            }
            f.push_back(line.substr(start, tab - start));
            start = tab + 1;
        }
        if (f.size() != 5 || f[0] != "V") {
            std::fprintf(stderr, "%s: not a vector line: %s\n", who, line.c_str());
            return false;
        }
        out.push_back(Vector{f[1], f[2], f[3], f[4]});
    }
    return true;
}

// The optional repeat count, from the `<vectors.tsv> [repeats]` every
// evaluator takes. Absent it is 0: answer the corpus once and say nothing
// about how long it took, which is what the differential asks for and what it
// has always got. False means the arguments were not that shape.
inline bool repeats(int argc, char** argv, const char* who, long& out) {
    out = 0;
    bool ok = argc == 2 || argc == 3;
    if (ok && argc == 3) {
        char* end = nullptr;
        out = std::strtol(argv[2], &end, 10);
        ok = end != argv[2] && *end == '\0' && out >= 1;
    }
    if (!ok) std::fprintf(stderr, "usage: %s <vectors.tsv> [repeats]\n", who);
    return ok;
}

// Answer one chain's vectors, `repeats` times over.
//
// The corpus is read before this is called and the answers are printed after
// the clock stops, so what the clock covers is parsing, verification and
// execution and nothing else. The answers are kept rather than dropped, so a
// round cannot be optimised away, and they are printed once however many
// rounds ran: the runner's input does not change because someone asked for a
// time. The timing line goes to stderr, where the runner does not read.
//
//	B <impl> <vectors> <repeats> <seconds>
template <class Answer>
inline void answer(const std::vector<Vector>& corpus, const char* chain, const char* name,
                   long repeats, Answer one) {
    // The other chains' vectors are theirs to answer, and selecting them here
    // rather than inside the loop keeps the time about this chain.
    std::vector<const Vector*> mine;
    for (const Vector& v : corpus)
        if (v.chain == chain) mine.push_back(&v);

    std::vector<Row> rows;
    rows.reserve(mine.size());
    const auto begun = std::chrono::steady_clock::now();
    for (long round = 0; round < (repeats > 0 ? repeats : 1); ++round) {
        rows.clear();
        for (const Vector* v : mine) rows.push_back(one(*v));
    }
    const double elapsed =
        std::chrono::duration<double>(std::chrono::steady_clock::now() - begun).count();

    for (const Row& r : rows) print(r);
    if (repeats > 0) {
        std::fprintf(stderr, "B\t%s\t%zu\t%ld\t%.6f\n", name, mine.size(), repeats, elapsed);
    }
}

}  // namespace lux::conformance
