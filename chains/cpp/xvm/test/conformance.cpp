// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ X-chain's answers to the shared corpus.
//
// One of three evaluators — Go, Rust, C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the three sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// Usage: xvm_conformance <vectors.tsv> [repeats]
//
// A repeat count asks for the same work to be done that many times and for the
// elapsed time of THAT WORK to be reported: the corpus is read before the clock
// starts and the verdicts are printed after it stops, so what the clock covers
// is parsing, verification and execution and nothing else. The verdicts are
// kept rather than dropped, so a round cannot be optimised away, and they are
// printed once however many rounds ran — the runner's input does not change
// because someone asked for a time. The timing line goes to stderr, where the
// runner does not read: B <impl> <vectors> <repeats> <seconds>
//
// THE TIME COVERS THE SAME WORK IN ALL THREE. eval_tx runs the parse, the
// syntactic pass, the chain's agreement and the execution, which is what the
// Go and Rust X-chains run, so a time measured here is comparable with theirs
// work for work.

#include "lux/xvm/block.hpp"
#include "lux/xvm/executor.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/txs.hpp"
#include "lux/xvm/vm.hpp"

#include <cctype>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#include <type_traits>
#include <vector>

using namespace lux::xvm;

namespace {

constexpr const char* kNone = "-";

// The verdict vocabulary, shared with the Go and Rust evaluators.
constexpr const char* kOk = "OK";
constexpr const char* kMalformed = "MALFORMED";
constexpr const char* kSyntactic = "SYNTACTIC";
constexpr const char* kOverflow = "OVERFLOW";
constexpr const char* kLedger = "LEDGER";
constexpr const char* kAuth = "AUTH";
constexpr const char* kWarp = "WARP";
constexpr const char* kUnsupported = "UNSUPPORTED";
constexpr const char* kSkipped = "SKIPPED";
constexpr const char* kInternal = "INTERNAL";

Id id_of(std::uint8_t b) {
    Id v{};
    v.fill(b);
    return v;
}

// The chain the corpus is built for. The X vectors are built fee-neutral —
// inputs equal outputs — so the differential measures the chain's rules and not
// three fee schedules. The Go and Rust evaluators are given the same.
executor::Backend backend() {
    executor::Backend b;
    b.config.tx_fee = 0;
    b.config.create_asset_tx_fee = 0;
    b.network_id = 1;
    b.chain_id = id_of(2);
    b.fee_asset_id = id_of(50);
    b.bootstrapped = true;
    b.fxs.push_back(executor::ParsedFx{id_of(1), std::make_shared<fx::Secp256k1Fx>()});
    b.fx_index.set(wire::TypeKind::Secp256k1, 0);
    return b;
}

std::string hex(const std::uint8_t* b, std::size_t n) {
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(n * 2);
    for (std::size_t i = 0; i < n; ++i) {
        out.push_back(d[b[i] >> 4]);
        out.push_back(d[b[i] & 0xF]);
    }
    return out;
}

bool unhex(const std::string& s, std::vector<std::uint8_t>& out) {
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

// One corpus line this evaluator answers, already split into fields. Splitting
// happens once, when the corpus is read; a repeated run re-evaluates the same
// vectors rather than re-reading the file.
struct Vector {
    std::string id, op, wire;
};

struct Row {
    std::string id, parse = kNone, kind = kNone, hash = kNone, syntactic = kNone, exec = kNone,
                note;
};

void print(const Row& r) {
    std::string note = r.note;
    for (auto& c : note)
        if (c == '\t' || c == '\n') c = ' ';
    if (note.empty()) note = kNone;
    std::printf("R\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.id.c_str(), r.parse.c_str(), r.kind.c_str(),
                r.hash.c_str(), r.syntactic.c_str(), r.exec.c_str(), note.c_str());
}

const char* kind_name(txs::XKind k) {
    switch (k) {
        case txs::XKind::Base: return "Base";
        case txs::XKind::CreateAsset: return "CreateAsset";
        case txs::XKind::Operation: return "Operation";
        case txs::XKind::Import: return "Import";
        case txs::XKind::Export: return "Export";
        case txs::XKind::Reserved: break;
    }
    return "unknown";
}

// The same word table the Go and Rust evaluators use, in the same order. The
// C++ X-chain raises its refusals as the Go sentinel's own words, so the table
// reads the reference's phrasing by construction.
std::string lower(std::string s) {
    for (auto& c : s) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    return s;
}

std::string classify(const std::string& raw) {
    const std::string s = lower(raw);
    auto has = [&](const char* w) { return s.find(w) != std::string::npos; };
    if (has("overflow") || has("underflow")) return kOverflow;
    if (has("wrong transaction type") || has("wrong tx type") || has("not permitted") ||
        has("not held") || has("unsupported"))
        return kUnsupported;
    if (has("credential") || has("signature") || has("unauthorized") || has("not authorised") ||
        has("not authorized"))
        return kAuth;
    if (has("warp")) return kWarp;
    if (has("utxo") || has("funds") || has("insufficient") || has("burn") || has("consumed") ||
        has("produced") || has("flow") || has("fee") || has("not found") || has("doesn't exist") ||
        has("does not exist") || has("isn't a current") || has("not validator") ||
        has("no such") || has("could not load") || has("shared memory"))
        return kLedger;
    return kSyntactic;
}

Row eval_tx(const std::string& id, const std::string& wire) {
    Row r;
    r.id = id;
    std::vector<std::uint8_t> bytes;
    if (!unhex(wire, bytes)) {
        r.parse = kInternal;
        r.note = "corpus wire is not hex";
        return r;
    }

    auto parsed = txs::parse(bytes);
    if (!parsed) {
        r.parse = kMalformed;
        r.syntactic = kMalformed;
        r.exec = kMalformed;
        r.note = parsed.error();
        return r;
    }
    const auto& tx = parsed.value();
    r.parse = "ok";
    r.kind = kind_name(tx->unsigned_tx->kind());
    r.hash = hex(tx->tx_id.data(), tx->tx_id.size());

    executor::Backend b = backend();
    executor::SyntacticVerifier v(b, *tx);
    auto syn = tx->unsigned_tx->visit(v);
    if (!syn) {
        r.syntactic = classify(syn.error());
        r.exec = r.syntactic;
        r.note = syn.error();
        return r;
    }
    r.syntactic = kOk;

    // And the two passes the chain runs after it: the chain's own agreement,
    // then the execution. Both read an EMPTY chain — the reference runs them
    // over a diff on a chain with nothing in it, so a vector that wanted a
    // UTXO is refused for the reason a chain would refuse it, and one that
    // wanted nothing goes through. A funded set would be a different question
    // than the one the corpus asks.
    //
    // A fresh state per vector, because the executor writes: two vectors
    // sharing one would be one vector judged against the other's leavings.
    state::State chain;
    executor::SemanticVerifier sem(b, chain, *tx);
    auto agreed = tx->unsigned_tx->visit(sem);
    if (!agreed) {
        r.exec = classify(agreed.error());
        r.note = agreed.error();
        return r;
    }
    executor::Executor exe(chain, *tx);
    auto ran = tx->unsigned_tx->visit(exe);
    if (!ran) {
        r.exec = classify(ran.error());
        r.note = ran.error();
        return r;
    }
    r.exec = kOk;
    r.note = "executed on the empty chain";
    return r;
}

Row eval_block(const std::string& id, const std::string& wire) {
    Row r;
    r.id = id;
    std::vector<std::uint8_t> bytes;
    if (!unhex(wire, bytes)) {
        r.parse = kInternal;
        r.note = "corpus wire is not hex";
        return r;
    }
    auto parsed = block::parse(bytes);
    if (!parsed) {
        r.parse = kMalformed;
        r.syntactic = kMalformed;
        r.exec = kMalformed;
        r.note = parsed.error();
        return r;
    }
    const auto& blk = parsed.value();
    r.parse = "ok";
    r.kind = "StandardBlock";
    r.hash = hex(blk->block_id.data(), blk->block_id.size());
    r.syntactic = kOk;
    r.exec = kLedger;
    r.note = "height=" + std::to_string(blk->height) + " parent=" +
             hex(blk->parent_id.data(), 4) + " txs=" + std::to_string(blk->transactions.size());
    return r;
}

// The block-decision seam.
//
// The answer comes from the compiler, not from this file's opinion. A chain
// that cannot reject a block cannot hand back what that block was carrying,
// and the two sides then disagree about what is still pending — which is not
// visible in any transaction's bytes, so no wire vector could catch it.
//
// THE QUESTION IS ABOUT THE SEAM, NOT ABOUT THIS PORT'S OWN CLASS. Consensus
// holds a lux::node::Block& and can call only what lux::node::Block declares;
// a reject that exists on VmBlock and nowhere in that interface is a method
// nothing will ever call. Asking `VmBlock` reported PRESENT while the seam had
// no reject at all — the differential agreeing with Go about a capability the
// C++ node does not have, which is the exact gap this vector was added to
// catch. So the probe is on the interface, and it goes green only when
// consensus can genuinely reach the port's reject: the seam declares it AND
// VmBlock is a lux::node::Block, so an override is the only way to satisfy
// both.
//
// Dependent on its parameter, so the compiler answers the question instead of
// refusing to ask it.
template <class B>
constexpr bool can_be_rejected = requires(B& b) { b.reject(); };

Row eval_seam(const std::string& id) {
    Row r;
    r.id = id;
    r.parse = "ok";
    r.kind = "Block.Reject";
    static_assert(std::is_base_of_v<lux::node::Block, VmBlock>,
                  "a reject reachable through the seam is only reachable if the port's block "
                  "IS the seam's block");
    if constexpr (can_be_rejected<lux::node::Block>) {
        r.exec = "PRESENT";
        r.note = "xvm VmBlock::reject, reached through lux::node::Block";
    } else {
        r.exec = "ABSENT";
        r.note = "the C++ block decision seam has verify and accept, and no reject: consensus "
                 "cannot tell a block it lost, so a rejected block's transactions are not "
                 "returned to the mempool";
    }
    return r;
}

Row evaluate(const Vector& v) {
    if (v.op == "tx") return eval_tx(v.id, v.wire);
    if (v.op == "block") return eval_block(v.id, v.wire);
    if (v.op == "seam") return eval_seam(v.id);
    Row r;
    r.id = v.id;
    r.parse = kInternal;
    r.note = "unknown op " + v.op;
    return r;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::fprintf(stderr, "usage: xvm_conformance <vectors.tsv> [repeats]\n");
        return 2;
    }
    // Absent, the repeat count is 0: evaluate the corpus once and say nothing
    // about how long it took, which is what the differential asks for and what
    // it has always got.
    long repeats = 0;
    if (argc == 3) {
        char* end = nullptr;
        repeats = std::strtol(argv[2], &end, 10);
        if (end == argv[2] || *end != '\0' || repeats < 1) {
            std::fprintf(stderr, "usage: xvm_conformance <vectors.tsv> [repeats]\n");
            return 2;
        }
    }
    std::ifstream in(argv[1]);
    if (!in) {
        std::fprintf(stderr, "xvm_conformance: cannot read %s\n", argv[1]);
        return 1;
    }

    // Reading the corpus, before the clock starts.
    std::vector<Vector> vectors;
    std::string line;
    while (std::getline(in, line)) {
        if (line.empty() || line[0] == '#') continue;
        // Split on every tab, keeping a trailing empty column.
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
            std::fprintf(stderr, "xvm_conformance: not a vector line: %s\n", line.c_str());
            return 1;
        }
        // This evaluator is the X-chain; the P-chain vectors are the P-chain
        // evaluator's to answer.
        if (f[2] != "X") continue;
        vectors.push_back(Vector{f[1], f[3], f[4]});
    }

    std::vector<Row> rows;
    rows.reserve(vectors.size());
    const auto begun = std::chrono::steady_clock::now();
    for (long round = 0; round < (repeats > 0 ? repeats : 1); ++round) {
        rows.clear();
        for (const Vector& v : vectors) rows.push_back(evaluate(v));
    }
    const double elapsed =
        std::chrono::duration<double>(std::chrono::steady_clock::now() - begun).count();

    for (const Row& r : rows) print(r);
    if (repeats > 0) {
        std::fprintf(stderr, "B\tcpp/xvm\t%zu\t%ld\t%.6f\n", vectors.size(), repeats, elapsed);
    }
    return 0;
}
