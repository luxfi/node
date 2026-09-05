// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ Q-chain's answers to the shared corpus.
//
// One of the evaluators — Go, Rust and C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// Every vector meets a chain stood up fresh over an in-memory store and seeded
// with its genesis, holding nothing above it. That is the arrangement the Go
// evaluator uses, and it is what makes the answers comparable: a refusal is
// then the rule that refused rather than a chain that happened to hold
// something this one did not.
//
// Usage: qvm_conformance <vectors.tsv>

#include "lux/conformance/corpus.hpp"

#include "lux/quantumvm/block.hpp"
#include "lux/quantumvm/config.hpp"
#include "lux/quantumvm/store.hpp"
#include "lux/quantumvm/vm.hpp"

#include <cstdio>
#include <string>
#include <type_traits>
#include <vector>

using namespace lux::quantumvm;
namespace conf = lux::conformance;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    v.fill(b);
    return v;
}

// The chain the Q vectors are built for.
//
// Q carries the pair ON the wire — a block names its chain and its network —
// but it also REFUSES a block whose pair is not the one this node serves, so
// the node's own identity is corpus contract just as Z's and F's are. It is
// asked back as Q_CHAIN_IDENTITY: an evaluator configured for another chain
// would answer "belongs to another chain" to every well-formed vector, and the
// rows would look like a chain that had lost its rules rather than one that had
// been pointed at the wrong network.
constexpr std::uint8_t kChainByte = 30;
constexpr std::uint32_t kNetworkID = 1;

// The chain's DEFAULT config, asked for by name rather than assembled here, so
// a change to the chain's defaults moves this evaluator with it. The Go
// evaluator asks its own reference the same way.
//
// It has to be asked for. A default-constructed Config is the ZERO value, and
// the zero value has quantum_stamp_enabled false — a Q-chain that checks no
// post-quantum signature at all. Config::validate normalises every other unset
// field to its default and leaves that one alone, so nothing downstream catches
// it. Built that way, this evaluator accepted four blocks Go refused: an
// expired stamp, a duplicate transaction and an unsupported ML-DSA parameter
// set all verified, because nothing was checking.
config::Config qconfig() { return config::default_config(); }

// A refusal Q reached without looking anything up.
//
// The Q-chain's Verify runs the chain binding and the block's own
// well-formedness before it goes looking for a parent, so which sentinel came
// back says which layer answered. The words are the port's own, and they are
// the Go chain's own, because the port raises Go's sentinels.
bool block_alone(const std::string& msg, std::uint64_t height) {
    auto has = [&](const char* w) { return msg.find(w) != std::string::npos; };
    if (has("belongs to another chain")) return true;
    if (has("carries no transactions")) return true;
    if (has("exceeds the wire bound")) return true;
    // One sentinel, two checks: height 0 is genesis and needs nothing but the
    // block, while a height that does not follow its parent needed the parent
    // to notice.
    if (has("invalid block height")) return height == 0;
    return false;
}

conf::Row eval_block(const std::string& id, const std::string& wire) {
    conf::Row r;
    r.id = id;
    std::vector<std::uint8_t> bytes;
    if (!conf::unhex(wire, bytes)) {
        r.parse = conf::kInternal;
        r.note = "corpus wire is not hex";
        return r;
    }

    // Fresh chain per vector, so no vector can be answered differently because
    // of one that ran before it.
    store::Memory db;
    QuantumVM vm(qconfig());
    Init init;
    init.db = &db;
    init.node_id = "conformance";
    init.chain_id = id_of(kChainByte);
    init.network_id = kNetworkID;
    if (auto st = vm.initialize(init); !st) {
        r.parse = conf::kInternal;
        r.note = "cannot stand up a Q-chain: " + st.error().message();
        return r;
    }

    auto parsed = vm.parse_block(bytes);
    if (!parsed) {
        r.parse = conf::kMalformed;
        r.syntactic = conf::kMalformed;
        r.exec = conf::kMalformed;
        r.note = parsed.error().message();
        return r;
    }
    const auto& blk = parsed.value();
    r.parse = "ok";
    r.kind = "QuantumBlock";
    const Id blk_id = blk->id();
    r.hash = conf::hex(blk_id.data(), blk_id.size());

    // One call, both layers, exactly as the Go evaluator does it: Verify runs
    // the chain binding and the block's own well-formedness before it looks for
    // a parent, so where the refusal came from is what says which layer
    // answered. Splitting it here would be this file deciding the layering
    // rather than reading it.
    auto st = blk->verify();
    if (st) {
        r.syntactic = conf::kOk;
        r.exec = conf::kOk;
        r.note = "verified against the seeded chain";
        return r;
    }
    const std::string msg = st.error().message();
    const std::string cls = conf::classify(msg);
    if (block_alone(msg, blk->height())) {
        r.syntactic = cls;
        r.exec = cls;
    } else {
        r.syntactic = conf::kOk;
        r.exec = cls;
    }
    r.note = msg;
    return r;
}

// The block-decision seam. The answer comes from the compiler, not from this
// file's opinion, and the question is about the SEAM rather than about this
// port's own class: consensus holds a lux::node::Block& and can call only what
// that interface declares, so a reject living anywhere else is a method nothing
// will ever call.
template <class B>
constexpr bool can_be_rejected = requires(B& b) { b.reject(); };

conf::Row eval_seam(const std::string& id) {
    conf::Row r;
    r.id = id;
    r.parse = "ok";
    r.kind = "Block.Reject";
    static_assert(std::is_base_of_v<lux::node::Block, VmBlock>,
                  "a reject reachable through the seam is only reachable if the port's block "
                  "IS the seam's block");
    if constexpr (can_be_rejected<lux::node::Block>) {
        r.exec = "PRESENT";
        r.note = "quantumvm VmBlock::reject, reached through lux::node::Block";
    } else {
        r.exec = "ABSENT";
        r.note = "the C++ block decision seam has verify and accept, and no reject";
    }
    return r;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: qvm_conformance <vectors.tsv>\n");
        return 2;
    }
    std::vector<conf::Vector> vectors;
    if (!conf::read(argv[1], "qvm_conformance", vectors)) return 1;

    for (const auto& v : vectors) {
        if (v.chain != "Q") continue;
        if (v.op == "block") {
            conf::print(eval_block(v.id, v.wire));
        } else if (v.op == "seam") {
            conf::print(eval_seam(v.id));
        } else if (v.op == "identity") {
            conf::print(conf::identity(v.id, kChainByte, kNetworkID));
        } else {
            std::fprintf(stderr, "qvm_conformance: unknown op %s on %s\n", v.op.c_str(),
                         v.id.c_str());
            return 1;
        }
    }
    return 0;
}
