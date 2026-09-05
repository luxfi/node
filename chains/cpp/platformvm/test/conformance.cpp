// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ P-chain's answers to the shared corpus.
//
// One of three evaluators — Go, Rust, C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the three sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// Usage: pvm_conformance <vectors.tsv> [repeats]
//
// A repeat count asks for the same work to be done that many times and for the
// elapsed time of THAT WORK to be reported: the corpus is read before the clock
// starts and the verdicts are printed after it stops, so what the clock covers
// is parsing, verification and execution and nothing else. The verdicts are
// kept rather than dropped, so a round cannot be optimised away, and they are
// printed once however many rounds ran — the runner's input does not change
// because someone asked for a time. The timing line goes to stderr, where the
// runner does not read: B <impl> <vectors> <repeats> <seconds>

#include "lux/platformvm/block.hpp"
#include "lux/platformvm/executor.hpp"
#include "lux/platformvm/state.hpp"
#include "lux/platformvm/txs.hpp"
#include "lux/platformvm/vm.hpp"

#include <chrono>
#include <cstdio>
#include <cctype>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <type_traits>
#include <vector>

using namespace lux::platformvm;
namespace ex = lux::platformvm::executor;

namespace {

constexpr const char* kNone = "-";

// The verdict vocabulary, shared with the Go and Rust evaluators. Three
// spellings of one answer would otherwise read as a disagreement that is not
// one.
constexpr const char* kOk = "OK";
constexpr const char* kMalformed = "MALFORMED";
constexpr const char* kSyntactic = "SYNTACTIC";
constexpr const char* kOverflow = "OVERFLOW";
constexpr const char* kLedger = "LEDGER";
constexpr const char* kAuth = "AUTH";
constexpr const char* kWarp = "WARP";
constexpr const char* kUnsupported = "UNSUPPORTED";
constexpr const char* kInternal = "INTERNAL";

// The chain the corpus is built for: network 1, chain id all-3s, fee and stake
// asset all-9s. The Go and Rust evaluators are given the same three numbers.
Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v.b[i] = b;
    return v;
}

Runtime runtime() { return Runtime{1, id_of(3), id_of(9)}; }

ex::StakingPolicy policy() {
    ex::StakingPolicy p;
    p.min_validator_stake = 1;
    p.max_validator_stake = std::uint64_t(1) << 60;
    p.min_delegator_stake = 1;
    p.min_stake_duration = 24 * 60 * 60;
    p.max_stake_duration = 365 * 24 * 60 * 60;
    p.min_delegation_fee = 20'000;
    p.uptime_requirement = 800'000;
    return p;
}

reward::Config reward_config() {
    reward::Config c;
    c.max_consumption_rate = 120'000;
    c.min_consumption_rate = 100'000;
    c.minting_period = 365 * reward::kDay;
    c.supply_cap = 720'000'000'000'000;
    return c;
}

const ex::FlatFee& fees() {
    static ex::FlatFee f(0);
    return f;
}

ex::Backend backend() {
    ex::Backend b;
    b.runtime = runtime();
    b.policy = policy();
    b.reward_config = reward_config();
    b.fees = &fees();
    b.bootstrapped = true;
    b.now = 1000;
    return b;
}

// A chain that holds nothing, at the corpus's genesis time. Every vector meets
// the same empty ledger — the one starting state all three implementations can
// stand up without three state builders to compare.
state::MemState empty_chain() {
    state::MemState s;
    s.set_timestamp(1000);
    return s;
}

bool unhex(const std::string& s, std::vector<std::uint8_t>& out) {
    if (s == kNone) return true;
    if (s.size() % 2 != 0) return false;
    out.reserve(s.size() / 2);
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    for (std::size_t i = 0; i < s.size(); i += 2) {
        int hi = nib(s[i]), lo = nib(s[i + 1]);
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

const char* kind_name(txs::Kind k) {
    switch (k) {
        case txs::Kind::RewardValidator: return "RewardValidator";
        case txs::Kind::Base: return "Base";
        case txs::Kind::Import: return "Import";
        case txs::Kind::Export: return "Export";
        case txs::Kind::CreateNetwork: return "CreateNetwork";
        case txs::Kind::CreateChain: return "CreateChain";
        case txs::Kind::TransferChainOwnership: return "TransferChainOwnership";
        case txs::Kind::RemoveChainValidator: return "RemoveChainValidator";
        case txs::Kind::TransformChain: return "TransformChain";
        case txs::Kind::AddValidator: return "AddValidator";
        case txs::Kind::AddChainValidator: return "AddChainValidator";
        case txs::Kind::AddDelegator: return "AddDelegator";
        case txs::Kind::AddPermissionlessValidator: return "AddPermissionlessValidator";
        case txs::Kind::AddPermissionlessDelegator: return "AddPermissionlessDelegator";
        case txs::Kind::RegisterL1Validator: return "RegisterL1Validator";
        case txs::Kind::SetL1ValidatorWeight: return "SetL1ValidatorWeight";
        case txs::Kind::IncreaseL1ValidatorBalance: return "IncreaseL1ValidatorBalance";
        case txs::Kind::DisableL1Validator: return "DisableL1Validator";
        case txs::Kind::ConvertNetwork: return "ConvertNetwork";
    }
    return "unknown";
}

const char* block_kind_name(block::Kind k) {
    switch (k) {
        case block::Kind::Abort: return "AbortBlock";
        case block::Kind::Commit: return "CommitBlock";
        case block::Kind::Proposal: return "ProposalBlock";
        case block::Kind::Standard: return "StandardBlock";
    }
    return "unknown";
}

// The C++ chain names every refusal as a value, so its class comes from the
// enumerator rather than from words. That is the same mapping the other two
// evaluators make, made more directly.
const char* classify_family(Err e) {
    switch (e) {
        case Err::None: return kOk;

        // The bytes are not a transaction.
        case Err::InvalidMagic:
        case Err::InvalidVersion:
        case Err::BufferTooSmall:
        case Err::UnknownTxKind:
        case Err::CredSigsOutOfRange:
        case Err::UnsupportedAuth:
        case Err::UnsupportedOwner:
        case Err::UnsupportedSigner:
        case Err::UnsupportedFxOutput:
        case Err::UnsupportedFxInput:
        case Err::UnsupportedCred: return kMalformed;

        case Err::Overflow:
        case Err::Underflow: return kOverflow;

        // A refusal that never looked at the chain: this implementation does
        // not run this kind of transaction at all.
        case Err::WrongTxType:
        case Err::AddValidatorTxNotPermitted:
        case Err::AddDelegatorTxNotPermitted:
        case Err::ProposedAddStakerTxNotPermitted:
        case Err::UnsupportedTx: return kUnsupported;

        default: break;
    }
    // Everything else is decided by name below; the switch above holds only the
    // families whose members are all one class.
    return nullptr;
}

}  // namespace

// classify_rest is generated from the error's own printed name, the same word
// table the Go and Rust evaluators use, in the same order. The C++ chain prints
// a sentinel's Go name, so the words match the reference's words by
// construction.
static std::string lower(std::string s) {
    for (auto& c : s) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    return s;
}

// The C++ chain names an error `ChainNotFound`; the other two say "chain not
// found". Splitting the name into words is what lets one table read all three.
static std::string spaced(const std::string& name) {
    std::string out;
    for (std::size_t i = 0; i < name.size(); ++i) {
        const bool boundary = i > 0 && std::isupper(static_cast<unsigned char>(name[i])) &&
                              !std::isupper(static_cast<unsigned char>(name[i - 1]));
        if (boundary) out.push_back(' ');
        out.push_back(name[i]);
    }
    return out;
}

static std::string classify_words(const std::string& raw) {
    const std::string s = lower(spaced(raw));
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

static std::string classify_error(const Error& e) {
    if (const char* fixed = classify_family(e.code)) return fixed;
    return classify_words(std::string(err_name(e.code)) + " " + e.detail);
}

static Row eval_tx(const std::string& id, const std::string& wire) {
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
        r.note = parsed.error().message();
        return r;
    }
    const txs::Tx& tx = parsed.value();
    r.parse = "ok";
    r.kind = kind_name(tx.unsigned_tx->kind());
    Id txid = tx.id();
    r.hash = txid.hex();

    Status syn = tx.syntactic_verify(runtime());
    if (!syn) {
        r.syntactic = classify_error(syn.error());
        r.exec = r.syntactic;
        r.note = syn.error().message();
        return r;
    }
    r.syntactic = kOk;

    state::MemState chain = empty_chain();
    state::Diff layer(&chain);
    ex::Backend b = backend();
    auto effects = ex::standard_tx(b, tx, layer);
    if (effects) {
        r.exec = kOk;
        r.note = "executed on the empty chain";
    } else {
        r.exec = classify_error(effects.error());
        r.note = effects.error().message();
    }
    return r;
}

static Row eval_block(const std::string& id, const std::string& wire) {
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
        r.note = parsed.error().message();
        return r;
    }
    const auto& blk = parsed.value();
    r.parse = "ok";
    r.kind = block_kind_name(blk->kind());
    Id bid = blk->id();
    r.hash = bid.hex();
    r.syntactic = kOk;
    r.exec = kLedger;
    Id parent = blk->parent();
    r.note = "height=" + std::to_string(blk->height()) + " parent=" + parent.hex().substr(0, 8);
    return r;
}

// The block-decision seam.
//
// The answer comes from the compiler, not from this file's opinion: the
// requires-expression below asks whether the block manager has a `reject` that
// can be called, and the two branches are the two honest answers. A chain that
// cannot reject a block cannot hand back what that block was carrying, and the
// two sides then disagree about what is still pending — which is not visible in
// any transaction's bytes, so no wire vector could catch it.
//
// THE QUESTION IS ABOUT THE SEAM, NOT ABOUT THIS PORT'S OWN CLASS. Consensus
// holds a lux::node::Block& and can call only what lux::node::Block declares; a
// reject that exists on VmBlock and nowhere in that interface is a method
// nothing will ever call. Asked of VmBlock the probe reported PRESENT while the
// seam had no reject at all — the differential agreeing with Go about a
// capability the C++ node does not have, which is the exact gap this vector was
// added to catch. So the probe is on the interface, and it goes green only when
// consensus can genuinely reach the port's reject: the seam declares it AND
// VmBlock is a lux::node::Block, so an override is the only way to satisfy both.
//
// Dependent on its parameter, so the compiler answers the question instead of
// refusing to ask it.
template <class B>
constexpr bool can_be_rejected = requires(B& b) { b.reject(); };

static Row eval_seam(const std::string& id) {
    Row r;
    r.id = id;
    r.parse = "ok";
    r.kind = "Block.Reject";
    static_assert(std::is_base_of_v<lux::node::Block, vm::VmBlock>,
                  "a reject reachable through the seam is only reachable if the port's block "
                  "IS the seam's block");
    if constexpr (can_be_rejected<lux::node::Block>) {
        r.exec = "PRESENT";
        r.note = "platformvm VmBlock::reject, reached through lux::node::Block";
    } else {
        r.exec = "ABSENT";
        r.note = "the C++ block decision seam has verify and accept, and no reject: consensus "
                 "cannot tell a block it lost, so a rejected block's decision transactions are "
                 "not reissued";
    }
    return r;
}

static Row evaluate(const Vector& v) {
    if (v.op == "tx") return eval_tx(v.id, v.wire);
    if (v.op == "block") return eval_block(v.id, v.wire);
    if (v.op == "seam") return eval_seam(v.id);
    Row r;
    r.id = v.id;
    r.parse = kInternal;
    r.note = "unknown op " + v.op;
    return r;
}

int main(int argc, char** argv) {
    if (argc < 2 || argc > 3) {
        std::fprintf(stderr, "usage: pvm_conformance <vectors.tsv> [repeats]\n");
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
            std::fprintf(stderr, "usage: pvm_conformance <vectors.tsv> [repeats]\n");
            return 2;
        }
    }
    std::ifstream in(argv[1]);
    if (!in) {
        std::fprintf(stderr, "pvm_conformance: cannot read %s\n", argv[1]);
        return 1;
    }

    // Reading the corpus, before the clock starts.
    std::vector<Vector> vectors;
    std::string line;
    while (std::getline(in, line)) {
        if (line.empty() || line[0] == '#') continue;
        // Split on every tab, keeping a trailing empty column. std::getline
        // would drop one, and a vector whose wire is empty has exactly that
        // shape.
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
            std::fprintf(stderr, "pvm_conformance: not a vector line: %s\n", line.c_str());
            return 1;
        }
        // This evaluator is the P-chain. The X-chain vectors belong to the
        // X-chain evaluator; printing nothing for them is what lets the runner
        // see that this implementation did not answer, rather than inventing an
        // answer it has no standing to give.
        if (f[2] != "P") continue;
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
        std::fprintf(stderr, "B\tcpp/platformvm\t%zu\t%ld\t%.6f\n", vectors.size(), repeats,
                     elapsed);
    }
    return 0;
}
