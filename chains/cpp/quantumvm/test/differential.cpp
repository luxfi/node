// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// differential.cpp — this chain's answer to the shared conformance corpus.
//
// The harness (conformance/harness_runner.py) hands every runtime the SAME
// bytes and compares what each one says about them, so a disagreement names the
// pair rather than being discovered in production. This binary prints one
// `RESULT id=… status=… detail=…` line per Q-chain vector, which is the whole
// protocol between it and the runner.
//
// It is a TEST as well as an evaluator: the corpus carries Go's expected id for
// each vector, so an id this implementation derives differently is a failure
// here, not a line for a human to notice. An unreadable corpus is a failure too
// — an evaluator that prints nothing would look like agreement.

#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/wire.hpp"

#include "corpus.hpp"

#include <cstdio>
#include <cstdlib>
#include <string>

using namespace lux::quantumvm;

namespace {

struct Outcome {
    std::string status;  // ACCEPTED | REJECTED
    std::string detail;
    bool matches_go = false;
};

// A transaction vector: the bytes are a signature PREIMAGE, and what is asserted
// is that they decode, re-encode identically, and hash to the id Go published.
Outcome evaluate_tx(ByteView wire, const std::string& want_id) {
    Outcome o;
    auto tx = wire::parse_tx_body(wire);
    if (!tx) {
        o.status = "REJECTED";
        o.detail = "type=QuantumBaseTx valid=false reason=" + tx.error().message();
        return o;
    }

    const ByteView again = (*tx)->bytes();
    if (again.size() != wire.size() ||
        !std::equal(again.begin(), again.end(), wire.begin())) {
        o.status = "REJECTED";
        o.detail = "type=QuantumBaseTx valid=false reason=non-canonical re-encoding";
        return o;
    }

    const std::string got = text((*tx)->id());
    o.status = "ACCEPTED";
    o.detail = "type=QuantumBaseTx valid=true";
    o.matches_go = want_id.empty() || got == want_id;
    if (!o.matches_go) o.detail += " id=" + got + " wantID=" + want_id;
    return o;
}

// A block vector: the bytes must be the canonical encoding of what comes out of
// them, and the id is sha256 of exactly those bytes.
Outcome evaluate_block(ByteView wire, const std::string& want_id) {
    Outcome o;
    auto blk = wire::parse_block_bytes(wire);
    if (!blk) {
        o.status = "REJECTED";
        o.detail = "type=QuantumBlock valid=false reason=" + blk.error().message();
        return o;
    }

    const std::string got = text(of(wire));
    o.status = "ACCEPTED";
    o.detail = "type=QuantumBlock valid=true txs=" + std::to_string(blk->transactions.size());
    o.matches_go = want_id.empty() || got == want_id;
    if (!o.matches_go) o.detail += " id=" + got + " wantID=" + want_id;
    return o;
}

}  // namespace

int main(int argc, char** argv) {
    std::string path = argc > 1 ? argv[1] : qvmtest::corpus::find_corpus(nullptr);
    if (path.empty()) {
        std::fprintf(stderr,
                     "qvm differential: the corpus was not found. Set NODE2_CORPUS_PATH or pass "
                     "conformance/corpus/chain_differential.json as the first argument.\n");
        return 2;
    }

    qvmtest::corpus::Value root;
    if (!qvmtest::corpus::load(path, root)) {
        std::fprintf(stderr, "qvm differential: %s did not parse as the corpus\n", path.c_str());
        return 2;
    }

    const qvmtest::corpus::Value* vectors = root.at("vectors");
    if (vectors == nullptr || vectors->kind != qvmtest::corpus::Value::Kind::Array) {
        std::fprintf(stderr, "qvm differential: %s carries no vectors\n", path.c_str());
        return 2;
    }

    int evaluated = 0, disagreed = 0;
    for (const auto& v : *vectors->array) {
        if (v.str("chain") != "Q") continue;  // this evaluator speaks for the Q-chain

        const std::string id = v.str("id");
        const std::string category = v.str("category");
        const auto wire = qvmtest::corpus::unhex(v.str("wire_hex"));

        std::string want_status = "ACCEPTED", want_tx_id, want_block_id;
        if (const auto* exp = v.at("go_expectation")) {
            want_status = exp->str("status");
            want_tx_id = exp->str("tx_id");
            want_block_id = exp->str("block_id");
            if (want_status.empty()) want_status = exp->flag("valid") ? "ACCEPTED" : "REJECTED";
        }

        Outcome o;
        if (category == "block_wire" || category == "block_rejection")
            o = evaluate_block(ByteView(wire.data(), wire.size()), want_block_id);
        else
            o = evaluate_tx(ByteView(wire.data(), wire.size()), want_tx_id);

        std::printf("RESULT id=%s status=%s detail=%s\n", id.c_str(), o.status.c_str(),
                    o.detail.c_str());
        ++evaluated;

        if (o.status != want_status) {
            std::fprintf(stderr, "%s: this runtime says %s, Go says %s (%s)\n", id.c_str(),
                         o.status.c_str(), want_status.c_str(), o.detail.c_str());
            ++disagreed;
        } else if (!o.matches_go) {
            std::fprintf(stderr, "%s: agreed on the verdict and not on the id — %s\n", id.c_str(),
                         o.detail.c_str());
            ++disagreed;
        }
    }

    if (evaluated == 0) {
        std::fprintf(stderr, "qvm differential: the corpus holds no Q-chain vector; an evaluator "
                             "that answers nothing is not agreement\n");
        return 2;
    }
    std::fprintf(stderr, "qvm differential: %d Q vectors evaluated, %d disagreements\n", evaluated,
                 disagreed);
    return disagreed == 0 ? 0 : 1;
}
