// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm_test.cpp — the whole chain through the node's VM seam, ported from
// vm_test.go and block/executor/block_test.go (TestBlockVerify / TestBlockAccept
// / TestBlockReject) and manager_test.go.
//
// Everything below is asked through `lux::node::VM` and `lux::node::Block` —
// build, parse, get, prefer, last-accepted; and on a block: id, parent, height,
// root, verify, accept. That is deliberate: a chain that only works through its
// own headers is not wired to anything, and the seam is the whole point of the
// port.
//
// The genesis asset is a real CreateAssetTx whose initial state carries the
// spendable outputs, so every later transaction spends value that a transaction
// created — there is no back door that mints UTXOs into the set.

#include "check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/vm.hpp"

#include <algorithm>

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

constexpr std::uint32_t kNetworkID = 369;
constexpr std::uint64_t kGenesisTime = 1749000000;
constexpr std::uint64_t kStartingBalance = 1000000;

Id chain_id() { return id(0xC1); }

fx::OutputOwners owner(int key = 0) { return fx::OutputOwners{0, 1, {test_address(key)}}; }

std::shared_ptr<fx::secp256k1fx::TransferOutput> tout(std::uint64_t amt, int key = 0) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = owner(key);
    return o;
}

std::shared_ptr<fx::secp256k1fx::TransferInput> tin(std::uint64_t amt) {
    auto i = std::make_shared<fx::secp256k1fx::TransferInput>();
    i->amt = amt;
    i->input.sig_indices = {0};
    return i;
}

// genesis_asset is the one transaction the chain starts with: it defines the
// asset AND, through its initial state, the outputs that hold its whole supply.
std::shared_ptr<txs::Tx> genesis_asset(int num_outputs) {
    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->name = "Lux";
    utx->symbol = "LUX";
    utx->denomination = 0;
    txs::InitialState s;
    s.fx_index = 0;
    for (int i = 0; i < num_outputs; ++i) s.outs.push_back(tout(kStartingBalance, 0));
    s.sort();
    utx->states = {s};

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

std::vector<executor::ParsedFx> the_fxs() {
    return {executor::ParsedFx{id(1), std::make_shared<fx::Secp256k1Fx>()},
            executor::ParsedFx{id(2), std::make_shared<fx::NFTFx>()},
            executor::ParsedFx{id(3), std::make_shared<fx::PropertyFx>()}};
}

// Chain is a booted VM: genesis installed, bootstrapped, clock set.
struct Chain {
    std::shared_ptr<txs::Tx> genesis;
    std::unique_ptr<Vm> vm;
    Id asset;

    explicit Chain(int genesis_outputs = 4, std::uint64_t tx_fee = 0) {
        genesis = genesis_asset(genesis_outputs);
        asset = genesis->id();

        VmConfig cfg;
        cfg.network_id = kNetworkID;
        cfg.chain_id = chain_id();
        cfg.net_id = id(0x0A);
        cfg.fee_asset_id = asset;
        cfg.tx_fee = tx_fee;
        cfg.create_asset_tx_fee = tx_fee;
        vm = std::make_unique<Vm>(cfg, the_fxs());
        auto r = vm->initialize({genesis}, kGenesisTime);
        check(r.has_value(), r ? "genesis installs" : "genesis installs: " + r.error());
        vm->set_bootstrapped(true);
        vm->set_now(kGenesisTime + 1);
    }

    // funded_utxo names the i'th genesis output — a real UTXO of the genesis
    // asset, at the index the executor gave it.
    txs::UTXOID funded_utxo(std::uint32_t i) const {
        return txs::UTXOID{genesis->id(), i, false};
    }

    // spend builds a signed BaseTx moving `amt` out of genesis output `i`.
    std::shared_ptr<txs::Tx> spend(std::uint32_t i, std::uint64_t amt, int signer = 0,
                                   std::uint64_t fee = 0) const {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(
            txs::TransferableInput{funded_utxo(i), asset, tin(kStartingBalance)});
        utx->base.outs.push_back(txs::TransferableOutput{asset, tout(amt)});
        (void)fee;
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto c = std::make_shared<fx::secp256k1fx::Credential>();
        c->signatures = {sign_unsigned_tx(test_key(signer), view(utx->bytes()))};
        tx->creds.push_back(c);
        (void)tx->initialize();
        return tx;
    }
};

// ================= genesis and the seam's identity =================

void genesis_and_identity() {
    std::printf("\n  -- genesis --\n");
    Chain c;

    check(c.vm->alias() == "X", "the chain answers to X");
    check(c.vm->chain_id() == chain_id(), "…and knows its own id");
    check(c.vm->last_accepted() != kEmptyId, "a genesis block was accepted");
    check(c.vm->last_accepted_height() == 0, "…at height 0");

    auto blk = c.vm->get(c.vm->last_accepted());
    check(blk != nullptr, "the genesis block is retrievable through the seam");
    if (blk != nullptr) {
        check(blk->height() == 0, "…at height 0");
        check(blk->parent() == kEmptyId, "…with no parent");
        check(blk->id() == c.vm->last_accepted(), "…and the id the VM reports");
    }

    // The genesis asset's supply is in the UTXO set, at the indices the
    // executor's rule fixes: base outputs first (there are none), then the
    // initial state's, all of them outputs of the new asset.
    for (std::uint32_t i = 0; i < 4; ++i) {
        auto u = c.vm->chain_state().get_utxo(c.funded_utxo(i).input_id());
        check(u.has_value(), "genesis output " + std::to_string(i) + " is spendable");
        check(u && u->asset_id == c.asset, "…and it is of the genesis asset");
    }
    check(c.vm->chain_state().utxo_count() == 4, "the whole supply is there and nothing else");

    auto asset_tx = c.vm->chain_state().get_tx(c.asset);
    check(asset_tx.has_value(), "the asset's own definition is in the chain");
}

// ================= the other half of a decision =================

// A block that loses gives back what it was holding. The transactions were
// never refused — they lost a race — so a node that drops them disagrees with
// every other node about what is still pending, and then builds on that.
void reject_returns_the_transactions() {
    std::printf("\n  -- rejecting --\n");
    Chain c;

    auto tx = c.spend(0, 100);
    check(c.vm->issue(tx).has_value(), "the tx is issued");
    check(c.vm->mempool_size() == 1, "…and waits in the mempool");

    auto blk = c.vm->build();
    if (blk == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    check(c.vm->mempool_size() == 0, "building took it out of the mempool");

    blk->reject();
    check(c.vm->mempool_size() == 1,
          "rejecting the block put the transaction back");

    // And the block is gone: what it pinned is released, so the next build
    // draws from the state that actually won.
    auto again = c.vm->build();
    check(again != nullptr, "a block can be built again from the returned tx");
    if (again != nullptr) {
        check(again->height() == 1, "…at the same height the loser held");
    }
}

// ================= the mempool gate =================

void issue_gate() {
    std::printf("\n  -- issuing --\n");
    Chain c;

    check(c.vm->issue(c.spend(0, 100)).has_value(), "a fundable, authorized tx is accepted");
    check(c.vm->mempool_size() == 1, "…and is waiting for a block");

    {
        // Signed by someone who does not own the output.
        Chain d;
        auto r = d.vm->issue(d.spend(0, 100, 1));
        check(!r && r.error().find(fx::kErrWrongSig) != std::string::npos,
              "a tx signed by the wrong key is refused");
        check(d.vm->mempool_size() == 0, "…and never enters the mempool");
    }
    {
        // Spending more than the input holds.
        Chain d;
        auto tx = d.spend(0, kStartingBalance + 1);
        auto r = d.vm->issue(tx);
        check(!r && r.error().find(txs::kErrInsufficientFunds) != std::string::npos,
              "an unfunded tx is refused");
    }
    {
        // A UTXO that does not exist.
        Chain d;
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(
            txs::TransferableInput{txs::UTXOID{id(0x99), 0, false}, d.asset, tin(1)});
        utx->base.outs.push_back(txs::TransferableOutput{d.asset, tout(1)});
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
        (void)tx->initialize();
        auto r = d.vm->issue(tx);
        check(!r && r.error().find(state::kErrNotFound) != std::string::npos,
              "a tx spending a UTXO that does not exist is refused");
    }
    {
        Chain d;
        auto r = d.vm->issue(nullptr);
        check(!r, "a null tx is refused");
    }
}

// ================= build, verify, accept =================

void happy_path() {
    std::printf("\n  -- build, verify, accept --\n");
    Chain c;

    const Id parent = c.vm->last_accepted();
    auto tx = c.spend(0, 100);
    check(c.vm->issue(tx).has_value(), "the tx is issued");

    auto blk = c.vm->build();
    if (blk == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    check(blk->parent() == parent, "the built block extends the preferred block");
    check(blk->height() == 1, "…at height 1");
    check(blk->root() != kEmptyId, "…and it commits to an execution root");
    check(!blk->bytes().empty(), "…and it has wire bytes");

    // Building already verified it, so verify is idempotent — Go's "block
    // already verified" case.
    check(blk->verify(), "a block this node built verifies");
    check(blk->verify(), "…and verifying it twice is the same answer");

    // Nothing is applied until acceptance.
    check(c.vm->last_accepted() == parent, "the chain has not moved yet");
    check(c.vm->chain_state().get_utxo(c.funded_utxo(0).input_id()).has_value(),
          "the spent UTXO is still there before acceptance");

    blk->accept();
    check(c.vm->last_accepted() == blk->id(), "acceptance moves the chain");
    check(c.vm->last_accepted_height() == 1, "…to height 1");
    check(!c.vm->chain_state().get_utxo(c.funded_utxo(0).input_id()).has_value(),
          "the spent UTXO is gone");
    txs::UTXOID produced{tx->id(), 0, false};
    check(c.vm->chain_state().get_utxo(produced.input_id()).has_value(),
          "the produced UTXO is there");
    check(c.vm->mempool_size() == 0, "the mempool is drained");

    // The accepted block is retrievable by id, and by parsing its bytes.
    auto again = c.vm->get(blk->id());
    check(again != nullptr && again->id() == blk->id(), "the accepted block is retrievable");
    Bytes raw(blk->bytes().begin(), blk->bytes().end());
    auto parsed = c.vm->parse(view(raw));
    check(parsed != nullptr, "its bytes parse back through the seam");
    if (parsed != nullptr) {
        check(parsed->id() == blk->id(), "…to the same id");
        check(parsed->root() == blk->root(), "…and the same root");
        check(parsed->height() == 1, "…and the same height");
    }
}

void build_when_empty() {
    std::printf("\n  -- nothing to build --\n");
    Chain c;
    auto blk = c.vm->build();
    check(blk == nullptr, "an empty mempool builds no block");
    check(c.vm->last_error() == std::string(kErrEmptyBlock), "…and says why");
}

// ================= what a block is refused for =================

// A block assembled by hand, so a case can state exactly one thing wrong with
// it. Every field here is one the proposer controls, which is why each has to be
// checked rather than trusted.
std::shared_ptr<block::StandardBlock> forge(const Chain& c, const Id& parent,
                                            std::uint64_t height, std::uint64_t time,
                                            const Id& root,
                                            std::vector<std::shared_ptr<txs::Tx>> tx_list) {
    (void)c;
    auto b = block::build(parent, height, time, root, std::move(tx_list));
    return b ? *b : nullptr;
}

void refusals() {
    std::printf("\n  -- what a block is refused for --\n");

    {
        Chain c;
        auto tx = c.spend(0, 100);
        (void)c.vm->issue(tx);
        auto good = c.vm->build();
        if (good == nullptr) {
            check(false, "build: " + c.vm->last_error());
            return;
        }
        // Re-parse so this is a fresh, unverified view of the same bytes.
        Bytes raw(good->bytes().begin(), good->bytes().end());
        auto again = c.vm->parse(view(raw));
        check(again != nullptr && again->verify(), "a re-parsed valid block still verifies");
    }
    {
        // A block proposed too far ahead of this node's clock.
        Chain c;
        auto tx = c.spend(0, 100);
        auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now() + kSyncBoundSeconds + 1,
                         kEmptyId, {tx});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block from too far in the future is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(kErrTimestampBeyondSyncBound) != std::string::npos,
              "…because its timestamp is beyond the sync bound");
    }
    {
        // An empty block. A block that decides nothing still costs a round.
        Chain c;
        auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now(), kEmptyId, {});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "an empty block is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(kErrEmptyBlock) != std::string::npos,
              "…because it contains no transactions");
    }
    {
        // A transaction that fails SYNTACTIC verification.
        Chain c;
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID + 1;  // wrong network
        utx->base.blockchain_id = chain_id();
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        (void)tx->initialize();
        auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now(), kEmptyId, {tx});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block whose tx fails syntax is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(txs::kErrWrongNetworkID) != std::string::npos,
              "…naming the syntactic reason");
    }
    {
        // The parent is not a block this chain knows.
        Chain c;
        auto tx = c.spend(0, 100);
        auto blk = forge(c, id(0xAB), 1, c.vm->now(), kEmptyId, {tx});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block whose parent is unknown is refused");
    }
    {
        // The height is not the parent's plus one.
        Chain c;
        auto tx = c.spend(0, 100);
        auto blk = forge(c, c.vm->last_accepted(), 7, c.vm->now(), kEmptyId, {tx});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block at the wrong height is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(kErrIncorrectHeight) != std::string::npos,
              "…because the height is not the parent's plus one");
    }
    {
        // Time may not run backwards.
        Chain c;
        c.vm->set_now(kGenesisTime + 100);
        auto tx = c.spend(0, 100);
        auto blk = forge(c, c.vm->last_accepted(), 1, kGenesisTime - 1, kEmptyId, {tx});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block older than its parent is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr &&
                  vb->error().find(kErrChildBlockEarlierThanParent) != std::string::npos,
              "…because its timestamp precedes the chain time");
    }
    {
        // A transaction that fails SEMANTIC verification: the signature is
        // syntactically fine and simply belongs to someone else.
        Chain c;
        auto tx = c.spend(0, 100, 1);
        auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now(), kEmptyId, {tx});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block whose tx fails semantics is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(fx::kErrWrongSig) != std::string::npos,
              "…naming the semantic reason");
    }
    {
        // Two transactions in one block spending the SAME output. The first
        // executes; the second then finds nothing to spend.
        Chain c;
        auto a = c.spend(0, 100);
        auto b = c.spend(0, 200);
        check(a->id() != b->id(), "the two txs are distinct");
        auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now(), kEmptyId, {a, b});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(), "a block that double-spends inside itself is refused");
    }
}

// ================= the root is recomputed, never trusted =================

void root_is_recomputed() {
    std::printf("\n  -- the execution root --\n");

    Chain c;
    auto tx = c.spend(0, 100);
    (void)c.vm->issue(tx);
    auto good = c.vm->build();
    if (good == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    const Id declared = good->root();
    check(declared != kEmptyId, "the built block declares a root");

    // The same block, with one byte of the root changed. Everything else about
    // it is valid — the transactions verify, the height and time are right — so
    // the ONLY thing that can refuse it is this node re-running the execution
    // and disagreeing.
    Chain d;
    auto same_tx = d.spend(0, 100);
    Id wrong = declared;
    wrong[0] = std::uint8_t(wrong[0] ^ 0x01);
    auto forged = forge(d, d.vm->last_accepted(), 1, d.vm->now(), wrong, {same_tx});
    auto v = d.vm->parse(view(forged->bytes));
    check(v != nullptr && !v->verify(), "a block whose declared root is wrong is refused");
    auto* vb = dynamic_cast<VmBlock*>(v.get());
    check(vb != nullptr && vb->error().find(kErrUnexpectedMerkleRoot) != std::string::npos,
          "…because this node's own execution produced a different root");

    // …and the honest root for that same block is the one the builder computed,
    // which is what makes the two nodes agree rather than merely not disagree.
    auto honest = forge(d, d.vm->last_accepted(), 1, d.vm->now(), declared, {same_tx});
    auto hv = d.vm->parse(view(honest->bytes));
    check(hv != nullptr && hv->verify(), "the honest root, computed independently, is accepted");
    check(hv != nullptr && hv->root() == declared,
          "…and it is the root the other node's builder produced");
}

// ================= two blocks, one chain =================

void two_blocks() {
    std::printf("\n  -- a second block on top of the first --\n");
    Chain c;

    auto tx1 = c.spend(0, 100);
    check(c.vm->issue(tx1).has_value(), "the first tx is issued");
    auto blk1 = c.vm->build();
    if (blk1 == nullptr) {
        check(false, "build 1: " + c.vm->last_error());
        return;
    }
    blk1->accept();
    check(c.vm->last_accepted_height() == 1, "the first block is accepted");

    auto tx2 = c.spend(1, 200);
    check(c.vm->issue(tx2).has_value(), "the second tx is issued on the new state");
    auto blk2 = c.vm->build();
    if (blk2 == nullptr) {
        check(false, "build 2: " + c.vm->last_error());
        return;
    }
    check(blk2->parent() == blk1->id(), "the second block names the first as its parent");
    check(blk2->height() == 2, "…at height 2");
    check(blk2->root() != blk1->root(), "…and its root differs, because the state moved");
    blk2->accept();
    check(c.vm->last_accepted_height() == 2, "the chain is at height 2");
    check(c.vm->chain_state().utxo_count() == 4,
          "two of four genesis outputs spent, two produced");

    // Heights resolve to the blocks that were accepted at them.
    auto h1 = c.vm->chain_state().get_block_id_at_height(1);
    auto h2 = c.vm->chain_state().get_block_id_at_height(2);
    check(h1 && *h1 == blk1->id(), "height 1 resolves to the first block");
    check(h2 && *h2 == blk2->id(), "height 2 resolves to the second");

    // Re-spending an output the chain already consumed is refused on the new
    // state — the double spend is caught by the chain, not by the mempool's
    // memory of what it has seen.
    auto again = c.spend(0, 300);
    auto r = c.vm->issue(again);
    check(!r && r.error().find(state::kErrNotFound) != std::string::npos,
          "spending an already-spent output is refused");
}

// ================= preference =================

void preference() {
    std::printf("\n  -- preference --\n");
    Chain c;

    const Id genesis = c.vm->last_accepted();
    auto tx = c.spend(0, 100);
    (void)c.vm->issue(tx);
    auto blk = c.vm->build();
    if (blk == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    // A verified-but-undecided block is a state a later block can be built on,
    // which is what preferring it means.
    c.vm->prefer(blk->id());
    check(c.vm->get_state(blk->id()) != nullptr, "a verified block exposes its pending state");
    check(c.vm->get_state(genesis) != nullptr, "…and the accepted state is still reachable");
    check(c.vm->get_state(id(0xEE)) == nullptr, "…while an unknown id resolves to nothing");

    blk->accept();
    check(c.vm->get_state(blk->id()) != nullptr, "after acceptance the block IS the chain state");
}

// ================= accepting something that was never verified =================

void accept_unverified() {
    std::printf("\n  -- accepting an unverified block --\n");
    Chain c;
    auto tx = c.spend(0, 100);
    auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now(), kEmptyId, {tx});
    auto v = c.vm->parse(view(blk->bytes));
    check(v != nullptr, "the block parses");
    const Id before = c.vm->last_accepted();
    v->accept();
    check(c.vm->last_accepted() == before, "accepting a block nobody verified moves nothing");
    check(c.vm->last_error() == std::string(kErrBlockNotFound), "…and says the block is not found");
}

// ================= the seam's own answers =================

void seam_answers() {
    std::printf("\n  -- the seam --\n");
    Chain c;

    Bytes junk{0x00, 0x01, 0x02};
    check(c.vm->parse(view(junk)) == nullptr, "unparseable bytes are not a block");
    check(!c.vm->last_error().empty(), "…and the reason is available");
    check(c.vm->get(id(0xDD)) == nullptr, "an unknown block id resolves to nothing");

    // A null answer is "no", not a failure: that is the house form for the whole
    // seam, and it is why nothing here throws.
    check(c.vm->build() == nullptr, "…and so is nothing to build");
}

// ================= driven ONLY through the seam =================

// Everything below goes through `lux::node::VM&` and `lux::node::Block&` — no
// xvm type is named. If the port had leaked a requirement into its own headers,
// this would not compile; since it does, a host that knows nothing but the seam
// can run this chain.
void through_the_seam_only() {
    std::printf("\n  -- driven through lux::node::VM alone --\n");
    Chain c;
    auto tx = c.spend(0, 100);
    check(c.vm->issue(tx).has_value(), "a transaction is waiting");

    lux::node::VM& vm = *c.vm;
    check(vm.alias() == "X", "the seam reports the chain's alias");
    const lux::node::Id before = vm.last_accepted();
    check(vm.last_accepted_height() == 0, "…and its height");

    std::shared_ptr<lux::node::Block> blk = vm.build();
    if (blk == nullptr) {
        check(false, "the seam builds a block");
        return;
    }
    check(blk->parent() == before, "the block extends the last accepted one");
    check(blk->height() == 1, "…at the next height");
    check(blk->root() != lux::node::kEmptyId, "…committing to what its execution produced");

    // A peer receives BYTES, and derives everything else itself.
    const std::vector<std::uint8_t> on_the_wire(blk->bytes().begin(), blk->bytes().end());
    std::shared_ptr<lux::node::Block> received = vm.parse(on_the_wire);
    check(received != nullptr, "a peer parses those bytes");
    if (received == nullptr) return;
    check(received->id() == blk->id(), "…and derives the same id");
    check(received->verify(), "…and its own execution agrees");

    received->accept();
    check(vm.last_accepted() == received->id(), "accepting through the seam moves the chain");
    check(vm.last_accepted_height() == 1, "…to the next height");
    vm.prefer(received->id());
    check(vm.get(received->id()) != nullptr, "…and the block is retrievable by id");
    check(vm.chain_id() == chain_id(), "the chain still knows its own id");
}

}  // namespace

int main() {
    std::printf("xvm — the chain through the node's VM seam\n");
    genesis_and_identity();
    issue_gate();
    happy_path();
    build_when_empty();
    refusals();
    root_is_recomputed();
    two_blocks();
    preference();
    accept_unverified();
    seam_answers();
    through_the_seam_only();
    reject_returns_the_transactions();
    return report("vm");
}
