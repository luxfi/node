// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ F-chain's answers to the shared corpus.
//
// One of the evaluators — Go, Rust and C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// THE CHAIN IS PART OF THE QUESTION. An F transaction is judged against a
// funded payer, a committee, a threshold and a network key, and none of those
// is in its bytes — they come from genesis. So the corpus carries the genesis
// as a vector of its own, and this evaluator stands its chain up on THOSE
// bytes rather than on a reconstruction of them. A reconstruction would be a
// second opinion about the configuration, and every transaction would then be
// refused for reasons that had nothing to do with its bytes.
//
// Usage: fhevm_conformance <vectors.tsv>

#include "lux/conformance/corpus.hpp"

#include "lux/fhevm/store.hpp"
#include "lux/fhevm/transaction.hpp"
#include "lux/fhevm/vm.hpp"

#include <cstdio>
#include <memory>
#include <string>
#include <vector>

using namespace lux::fhevm;
namespace conf = lux::conformance;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    v.fill(b);
    return v;
}

// The chain the F vectors are built for.
//
// An F transaction's signing preimage is bound to the chain id, and the chain
// id is NOT on the wire — each side supplies its own — so a transaction signed
// for one F-chain cannot authenticate on another. That makes the number corpus
// contract rather than anything a vector carries, and F_CHAIN_IDENTITY asks for
// it back: an evaluator configured for another chain would answer "invalid
// payer signature" to every signed vector, which reads like a chain that had
// lost its verifier rather than one pointed at the wrong network.
constexpr std::uint8_t kChainByte = 50;
constexpr std::uint32_t kNetworkID = 1;

VM::Config fconfig() {
    VM::Config c;
    c.network_id = kNetworkID;
    c.chain_id = id_of(kChainByte);
    c.alias = "F";
    return c;
}

// The class of a refusal, decided on its SENTINEL rather than on the sentence
// it was wrapped in — which is what Go does when it unwraps to the root before
// reading any words. Every F refusal carries a code, and name(code) IS the Go
// sentinel's text, so the two sides classify the same string. The detail rides
// in the note beside it, uncompared.
std::string classify_of(const Error& e) { return conf::classify(std::string(name(e.code))); }

const char* kind_name(std::uint8_t t) {
    switch (t) {
        case kTxRegisterCiphertext: return "RegisterCiphertext";
        case kTxGrantPermit: return "GrantPermit";
        case kTxRevokePermit: return "RevokePermit";
        case kTxRequestDecrypt: return "RequestDecrypt";
        case kTxFulfillDecrypt: return "FulfillDecrypt";
        case kTxAdvanceEpoch: return "AdvanceEpoch";
        default: return "unknown";
    }
}

// The chain every F vector meets: stood up fresh, on the corpus's own genesis.
struct Chain {
    Memory store;
    VM vm{&store, fconfig()};
    std::string error;

    explicit Chain(const std::string& genesis) {
        if (auto init = vm.initialize(genesis); !init) error = init.error().message();
    }
};

conf::Row eval_genesis(const std::string& id, const std::string& genesis) {
    conf::Row r;
    r.id = id;
    r.parse = "ok";
    r.kind = "Genesis";
    Chain c(genesis);
    if (!c.error.empty()) {
        r.syntactic = conf::kInternal;
        r.exec = conf::kInternal;
        r.note = "cannot stand up an F-chain: " + c.error;
        return r;
    }
    const lux::node::Id last = c.vm.last_accepted();
    r.hash = conf::hex(last.data(), last.size());
    r.syntactic = conf::kOk;
    r.exec = conf::kOk;
    r.note = "the chain the F vectors are judged on";
    return r;
}

conf::Row eval_tx(const std::string& id, const std::string& wire, const std::string& genesis) {
    conf::Row r;
    r.id = id;
    std::vector<std::uint8_t> bytes;
    if (!conf::unhex(wire, bytes)) {
        r.parse = conf::kInternal;
        r.note = "corpus wire is not hex";
        return r;
    }

    auto parsed = parse_transaction(bytes);
    if (!parsed) {
        r.parse = conf::kMalformed;
        r.syntactic = conf::kMalformed;
        r.exec = conf::kMalformed;
        r.note = parsed.error().message();
        return r;
    }
    const Transaction& tx = parsed.value();
    r.parse = "ok";
    r.kind = kind_name(tx.type);
    const Id tx_id = tx.id();
    r.hash = conf::hex(tx_id.data(), tx_id.size());

    // What the transaction says about itself, decided without the chain.
    if (auto syn = tx.syntactic_verify(); !syn) {
        const std::string cls = classify_of(syn.error());
        r.syntactic = cls;
        r.exec = cls;
        r.note = syn.error().message();
        return r;
    }
    r.syntactic = conf::kOk;

    // And what needed the chain: the payer's balance and nonce, the signature
    // over the preimage this chain id binds, and whatever the operation names.
    // Fresh per vector, so no vector can be answered differently because of one
    // that ran before it.
    Chain c(genesis);
    if (!c.error.empty()) {
        r.exec = conf::kInternal;
        r.note = "cannot stand up an F-chain: " + c.error;
        return r;
    }
    if (auto sub = c.vm.submit_tx(tx); !sub) {
        r.exec = classify_of(sub.error());
        r.note = sub.error().message();
        return r;
    }
    r.exec = conf::kOk;
    r.note = "admitted by the funded chain";
    return r;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: fhevm_conformance <vectors.tsv>\n");
        return 2;
    }
    std::vector<conf::Vector> vectors;
    if (!conf::read(argv[1], "fhevm_conformance", vectors)) return 1;

    // The genesis first, because every transaction is judged on the chain it
    // makes. Its absence is a failure rather than a chain quietly stood up on
    // something else.
    std::string genesis;
    int genesis_count = 0;
    for (const auto& v : vectors) {
        if (v.chain == "F" && v.op == "genesis") {
            ++genesis_count;
            std::vector<std::uint8_t> b;
            if (!conf::unhex(v.wire, b)) {
                std::fprintf(stderr, "fhevm_conformance: the genesis vector is not hex\n");
                return 1;
            }
            genesis.assign(b.begin(), b.end());
        }
    }
    // Exactly one, or none of the answers below mean anything: a second
    // genesis is a second answer to the question of which chain these
    // transactions are being judged on, and last-one-wins would pick it
    // silently.
    if (genesis_count > 1) {
        std::fprintf(stderr, "fhevm_conformance: the corpus names %d F genesis vectors\n",
                     genesis_count);
        return 1;
    }
    bool has_f = false;
    for (const auto& v : vectors) has_f = has_f || v.chain == "F";
    if (has_f && genesis.empty()) {
        std::fprintf(stderr, "fhevm_conformance: the corpus has F vectors and no F genesis\n");
        return 1;
    }

    for (const auto& v : vectors) {
        if (v.chain != "F") continue;
        if (v.op == "genesis") {
            conf::print(eval_genesis(v.id, genesis));
        } else if (v.op == "tx") {
            conf::print(eval_tx(v.id, v.wire, genesis));
        } else if (v.op == "identity") {
            conf::print(conf::identity(v.id, kChainByte, kNetworkID));
        } else {
            std::fprintf(stderr, "fhevm_conformance: unknown op %s on %s\n", v.op.c_str(),
                         v.id.c_str());
            return 1;
        }
    }
    return 0;
}
