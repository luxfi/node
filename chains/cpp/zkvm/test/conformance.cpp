// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ Z-chain's answers to the shared corpus.
//
// One of the evaluators — Go, Rust and C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// Every vector meets a chain stood up fresh over an in-memory store, seeded
// with the corpus's genesis and holding no spent note, no output and no
// accepted block beyond it, on the Z-chain's DEFAULT profile — which is the
// strict-PQ one. That is the arrangement the Go evaluator uses, and it is what
// makes the profile gate comparable: a chain that verified a classical proof
// here would be one a CRQC could mint shielded value on, and the row would
// say so.
//
// Usage: zkvm_conformance <vectors.tsv>

#include "lux/conformance/corpus.hpp"

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/vm.hpp"

#include <cstdio>
#include <string>
#include <type_traits>
#include <vector>

using namespace lux::zkvm;
namespace conf = lux::conformance;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    v.fill(b);
    return v;
}

// The chain the Z vectors are built for, and the genesis it is born with.
//
// A Z block id opens with sha256(ChainID ‖ NetworkID), and NEITHER number is on
// the wire, so both are corpus contract rather than anything a vector carries.
// The corpus states them in conformance/gen/identity.go and asks for them back
// in Z_CHAIN_IDENTITY, because an evaluator that picks its own derives a
// different id for every well-formed vector on the chain — which is what a hash
// fork looks like, and which is exactly what these two numbers being 4 here and
// 40 there produced on the first run: twenty-five id disagreements, none of
// them a chain's fault.
//
// The genesis timestamp is contract for the same reason. The genesis block's id
// is hashed from it, and every vector names that block as its parent, so a
// chain born at a different second holds none of them.
constexpr std::uint8_t kChainByte = 40;
constexpr std::uint32_t kNetworkID = 1;
constexpr std::int64_t kGenesisTime = 1000;

// The proof profile is the CONSTRUCTED DEFAULT — strict-PQ, no verifying keys —
// stated by leaving VmConfig::z alone rather than by setting it here, so a
// change to the chain's default profile moves this evaluator with it.
VmConfig zconfig() {
    VmConfig c;
    c.chain_id = id_of(kChainByte);
    c.network_id = kNetworkID;
    c.alias = "Z";
    return c;
}

Genesis zgenesis() {
    Genesis g;
    g.timestamp = kGenesisTime;
    return g;
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

// What to call the thing that came off the wire.
//
// The Z-chain has one block type, so the name that carries information is what
// the block CONTAINS: a block of one kind repeated is named once with its
// count, and a mixed block spells every one out, because which types a block
// mixes is the thing the field is there to compare.
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

conf::Row eval_block(const std::string& id, const std::string& wire) {
    conf::Row r;
    r.id = id;
    std::vector<std::uint8_t> bytes;
    if (!conf::unhex(wire, bytes)) {
        r.parse = conf::kInternal;
        r.note = "corpus wire is not hex";
        return r;
    }

    // Fresh chain per vector: each block is judged on its own against a chain
    // that has accepted nothing past genesis, so no vector can be answered
    // differently because of one that ran before it.
    store::Memory base;
    Vm vm(zconfig(), base);
    // The clock is the WALL clock here, deliberately, because the Go evaluator
    // reads its own. Stating a time would make this chain hold a block to a
    // skew bound the reference does not, and a vector between the two bounds
    // would then disagree over which clock was read rather than over any rule.
    // Every vector's timestamp is either far in the past or past 2100, so both
    // read the same verdict off whatever clock they have.
    if (auto init = vm.initialize(zgenesis()); !init) {
        r.parse = conf::kInternal;
        r.note = "cannot stand up a Z-chain: " + init.error();
        return r;
    }

    auto parsed = vm.parse_block(bytes);
    if (!parsed) {
        r.parse = conf::kMalformed;
        r.syntactic = conf::kMalformed;
        r.exec = conf::kMalformed;
        r.note = parsed.error();
        return r;
    }
    const auto& blk = parsed.value();
    r.parse = "ok";
    r.kind = kind_name(blk->txs);
    const Id blk_id = blk->id();
    r.hash = conf::hex(blk_id.data(), blk_id.size());

    // The syntactic pass is the chain's own — check's first half, everything it
    // settles from the block in hand before it reads a key. Asking the chain
    // where its boundary falls is the point: an evaluator that decided that for
    // itself would be a second opinion about the port under test, and this row
    // disagreed with Go for four vectors because both evaluators used to hold
    // one. Go names the rules that land here by their sentinels; this names them
    // by calling the method they live in.
    if (auto v = blk->syntactic_verify(); !v) {
        r.syntactic = conf::classify(v.error());
        r.exec = r.syntactic;
        r.note = v.error();
        return r;
    }
    r.syntactic = conf::kOk;

    // check is this node's whole verdict: the half above, then the spent set and
    // the proofs — which is where the strict-PQ profile gate fires — then the
    // parent, the tip it must sit on, and the state root.
    if (auto v = blk->check(); !v) {
        r.exec = conf::classify(v.error());
        r.note = v.error();
        return r;
    }
    r.exec = conf::kOk;
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

conf::Row eval_seam(const std::string& id) {
    conf::Row r;
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
    std::vector<conf::Vector> vectors;
    if (!conf::read(argv[1], "zkvm_conformance", vectors)) return 1;

    for (const auto& v : vectors) {
        // This evaluator is the Z-chain; the other chains' vectors are theirs
        // to answer.
        if (v.chain != "Z") continue;

        if (v.op == "block") {
            conf::print(eval_block(v.id, v.wire));
        } else if (v.op == "seam") {
            conf::print(eval_seam(v.id));
        } else if (v.op == "identity") {
            conf::print(conf::identity(v.id, kChainByte, kNetworkID));
        } else {
            std::fprintf(stderr, "zkvm_conformance: unknown op %s on %s\n", v.op.c_str(),
                         v.id.c_str());
            return 1;
        }
    }
    return 0;
}
