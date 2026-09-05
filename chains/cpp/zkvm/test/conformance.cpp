// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ Z-chain's answers to the shared corpus.
//
// One of the evaluators — Go here, and C++ in this file — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// Every vector meets a chain stood up fresh over an in-memory store, holding no
// spent note, no output and no accepted block, on the Z-chain's DEFAULT profile
// — which is the strict-PQ one. That is the arrangement the Go evaluator uses,
// and it is what makes the profile gate comparable: a chain that verified a
// classical proof here would be one a CRQC could mint shielded value on, and
// the row would say so.
//
// Usage: zkvm_conformance <vectors.tsv>

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/vm.hpp"

#include <cctype>
#include <cstdio>
#include <fstream>
#include <memory>
#include <string>
#include <type_traits>
#include <vector>

using namespace lux::zkvm;

namespace {

constexpr const char* kNone = "-";

// The verdict vocabulary, shared with the Go evaluator.
constexpr const char* kOk = "OK";
constexpr const char* kMalformed = "MALFORMED";
constexpr const char* kSyntactic = "SYNTACTIC";
constexpr const char* kOverflow = "OVERFLOW";
constexpr const char* kLedger = "LEDGER";
constexpr const char* kAuth = "AUTH";
constexpr const char* kWarp = "WARP";
constexpr const char* kUnsupported = "UNSUPPORTED";
constexpr const char* kInternal = "INTERNAL";

Id id_of(std::uint8_t b) {
    Id v{};
    v.fill(b);
    return v;
}

// The chain the Z vectors are built for. A block id opens with
// sha256(ChainID ‖ NetworkID), which is not on the wire, so these two numbers
// are part of the corpus's contract: the Go evaluator is given the same.
//
// The proof profile is the CONSTRUCTED DEFAULT — strict-PQ, no verifying keys —
// stated by leaving VmConfig::z alone rather than by setting it here, so a
// change to the chain's default profile moves this evaluator with it.
VmConfig zconfig() {
    VmConfig c;
    c.chain_id = id_of(4);
    c.network_id = 1;
    c.alias = "Z";
    return c;
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

struct Row {
    std::string id, parse = kNone, kind = kNone, hash = kNone, syntactic = kNone, exec = kNone,
                note;
};

void print(const Row& r) {
    std::string note = r.note;
    for (auto& c : note)
        if (c == '\t' || c == '\n') c = ' ';
    if (note.size() > 160) note.resize(160);
    if (note.empty()) note = kNone;
    std::printf("R\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.id.c_str(), r.parse.c_str(), r.kind.c_str(),
                r.hash.c_str(), r.syntactic.c_str(), r.exec.c_str(), note.c_str());
}

const char* tx_kind_name(TxType t) {
    switch (t) {
        case TxType::Transfer: return "Transfer";
        case TxType::Mint: return "Mint";
        case TxType::Burn: return "Burn";
        case TxType::Shield: return "Shield";
        case TxType::Unshield: return "Unshield";
    }
    return "unknown";
}

// A block of one kind repeated is named once with its count, so a
// hundred-transaction vector does not print a hundred names; a mixed block
// spells every one out, because which types a block mixes is the thing the
// field is there to compare.
std::string kind_name(const std::vector<Transaction>& txs) {
    if (txs.empty()) return "Empty";
    const std::string first = tx_kind_name(txs[0].type);
    bool uniform = true;
    std::string joined = first;
    for (std::size_t i = 1; i < txs.size(); ++i) {
        const std::string n = tx_kind_name(txs[i].type);
        if (n != first) uniform = false;
        joined += "+";
        joined += n;
    }
    if (uniform) {
        if (txs.size() == 1) return first;
        return first + "x" + std::to_string(txs.size());
    }
    return joined;
}

// The same word table the Go evaluator uses, in the same order. The C++
// Z-chain raises its refusals as the Go sentinel's own words, so the table
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
        has("not held") || has("unsupported") || has("forbidden"))
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

Row eval_block(const std::string& id, const std::string& wire) {
    Row r;
    r.id = id;
    std::vector<std::uint8_t> bytes;
    if (!unhex(wire, bytes)) {
        r.parse = kInternal;
        r.note = "corpus wire is not hex";
        return r;
    }

    // Fresh chain per vector: each block is judged on its own against a chain
    // that has accepted nothing, so no vector can be answered differently
    // because of one that ran before it.
    store::Memory base;
    Vm vm(zconfig(), base);
    // The clock is the WALL clock here, deliberately, because the Go evaluator
    // reads its own. Stating a time would make this chain hold a block to a
    // skew bound the reference does not, and a vector between the two bounds
    // would then disagree over which clock was read rather than over any rule.
    // Every vector's timestamp is either far in the past or past 2100, so both
    // read the same verdict off whatever clock they have.
    if (auto init = vm.initialize(Genesis{}); !init) {
        r.parse = kInternal;
        r.note = "cannot stand up a Z-chain: " + init.error();
        return r;
    }

    auto parsed = vm.parse_block(bytes);
    if (!parsed) {
        r.parse = kMalformed;
        r.syntactic = kMalformed;
        r.exec = kMalformed;
        r.note = parsed.error();
        return r;
    }
    const auto& blk = parsed.value();
    r.parse = "ok";
    r.kind = kind_name(blk->txs);
    const Id blk_id = blk->id();
    r.hash = hex(blk_id.data(), blk_id.size());

    // The shape check, per transaction, from the port's own method. It is the
    // Z-chain's analogue of a syntactic pass: everything about a transaction
    // that can be decided without asking the chain anything.
    for (const auto& tx : blk->txs) {
        if (auto v = tx.validate_basic(); !v) {
            r.syntactic = classify(v.error());
            r.exec = r.syntactic;
            r.note = v.error();
            return r;
        }
    }
    r.syntactic = kOk;

    // check is this node's whole verdict on the block: the transaction cap, the
    // clock, the block-level spent set, the one admission predicate over every
    // transaction — which is where the strict-PQ profile gate fires — and the
    // state root.
    if (auto v = blk->check(); !v) {
        r.exec = classify(v.error());
        r.note = v.error();
        return r;
    }
    r.exec = kOk;
    r.note = "height=" + std::to_string(blk->block_height) +
             " txs=" + std::to_string(blk->txs.size());
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
// a reject that exists on zkvm::Block and nowhere in that interface is a method
// nothing will ever call. So the probe is on the interface, and it goes green
// only when consensus can genuinely reach the port's reject: the seam declares
// it AND zkvm::Block is a lux::node::Block, so an override is the only way to
// satisfy both.
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
    static_assert(std::is_base_of_v<lux::node::Block, Block>,
                  "a reject reachable through the seam is only reachable if the port's block "
                  "IS the seam's block");
    if constexpr (can_be_rejected<lux::node::Block>) {
        r.exec = "PRESENT";
        r.note = "zkvm Block::reject, reached through lux::node::Block";
    } else {
        r.exec = "ABSENT";
        r.note = "the C++ block decision seam has verify and accept, and no reject: consensus "
                 "cannot tell a block it lost, so a rejected block's transactions are not "
                 "returned to the mempool";
    }
    return r;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: zkvm_conformance <vectors.tsv>\n");
        return 2;
    }
    std::ifstream in(argv[1]);
    if (!in) {
        std::fprintf(stderr, "zkvm_conformance: cannot read %s\n", argv[1]);
        return 1;
    }

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
            std::fprintf(stderr, "zkvm_conformance: not a vector line: %s\n", line.c_str());
            return 1;
        }
        // This evaluator is the Z-chain; the other chains' vectors are theirs
        // to answer.
        if (f[2] != "Z") continue;

        if (f[3] == "block") {
            print(eval_block(f[1], f[4]));
        } else if (f[3] == "seam") {
            print(eval_seam(f[1]));
        } else {
            std::fprintf(stderr, "zkvm_conformance: unknown op %s on %s\n", f[3].c_str(),
                         f[1].c_str());
            return 1;
        }
    }
    return 0;
}
