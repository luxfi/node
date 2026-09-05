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

#include "lux/core/check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/vm.hpp"

#include <algorithm>
#include <ctime>
#include <string>
#include <type_traits>
#include <unistd.h>

using namespace lux::xvm;
using namespace lux::core::test;
using namespace lux::xvm::test;

namespace {

// Chain is a booted VM: genesis installed, bootstrapped, clock set.
struct Chain {
    store::Memory store;
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
        vm = std::make_unique<Vm>(cfg, the_fxs(), store);
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

// ================= what a VM does before anyone tells it anything =================
//
// Skipping signature checks while replaying settled history is an optimization,
// and this is the case that keeps it one. Go's chain starts with the check off
// and its engine turns it on through SetState before the chain ever meets a
// peer; the C++ seam has no such call to rely on, so a chain that started off
// would stay off — checking nothing, for its whole life, while looking exactly
// like a chain that checks. So the default is the checking one, and the switch
// only ever relaxes it.
//
// The signature of that switch is asserted too. lux::node::VM declares
// `virtual void set_bootstrapped(bool)`, and a Vm method that differed by one
// qualifier would SHADOW it rather than override it: a host driving the seam
// would then reach the seam's do-nothing body while the concrete chain sat
// unchanged, which is the same fail-open by a quieter route.
static_assert(std::is_same_v<decltype(&Vm::set_bootstrapped), void (Vm::*)(bool)>,
              "Vm::set_bootstrapped must be exactly lux::node::VM::set_bootstrapped(bool), "
              "or a host driving the seam silently changes nothing");

void signature_checking_is_the_default() {
    std::printf("\n  -- a chain nobody has spoken to --\n");

    store::Memory store;
    auto genesis = genesis_asset(4);
    const Id asset = genesis->id();

    VmConfig cfg;
    cfg.network_id = kNetworkID;
    cfg.chain_id = chain_id();
    cfg.net_id = id(0x0A);
    cfg.fee_asset_id = asset;

    // Built and brought up, and NOT told anything about its lifecycle — the
    // host that forgets, or the seam that never grew the call.
    Vm vm(cfg, the_fxs(), store);
    auto r = vm.initialize({genesis}, kGenesisTime);
    check(r.has_value(), r ? "genesis installs" : "genesis installs: " + r.error());
    vm.set_now(kGenesisTime + 1);

    check(vm.bootstrapped(), "a VM nobody has spoken to is checking signatures");

    // A credential from the wrong key over a genesis output owned by key 0.
    // Nothing about its SHAPE is wrong: the only thing that refuses it is the
    // recovery, which is the thing the flag governs.
    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->base.ins.push_back(
        txs::TransferableInput{txs::UTXOID{asset, 0, false}, asset, tin(kStartingBalance)});
    utx->base.outs.push_back(txs::TransferableOutput{asset, tout(100, 1)});
    auto forged = std::make_shared<txs::Tx>();
    forged->unsigned_tx = utx;
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    cred->signatures = {sign_unsigned_tx(test_key(1), view(utx->bytes()))};
    forged->creds.push_back(cred);
    (void)forged->initialize();

    auto refused = vm.verify_tx(*forged);
    check(!refused, "…so it refuses a credential the owner did not sign");
    check(!refused && refused.error().find(fx::kErrWrongSig) != std::string::npos,
          "…and refuses it as a wrong signature, not as some earlier shape check");
    check(!vm.issue(forged), "…and will not take it into the mempool either");

    // And the check is real on the path a PEER reaches, not just at the mempool
    // door. The same forged credential inside a block, with a root deliberately
    // left blank: the root is the LAST thing verification looks at, so which of
    // the two refusals comes back says how far the block got.
    auto built = block::build(vm.last_accepted(), 1, vm.now(), kEmptyId, {forged});
    check(built.has_value(), built ? "a peer's block carrying it is well-formed"
                                   : "block: " + built.error());
    Bytes raw = (*built)->bytes;

    auto why = [&](const std::shared_ptr<lux::node::Block>& b) {
        auto* vb = dynamic_cast<VmBlock*>(b.get());
        return vb == nullptr ? std::string("not a block") : vb->error();
    };

    auto peer_block = vm.parse(view(raw));
    check(peer_block != nullptr && !peer_block->verify(),
          "…and a block carrying it does not verify either");
    check(peer_block != nullptr && why(peer_block).find(fx::kErrWrongSig) != std::string::npos,
          "…stopping at the signature, before it ever reaches the root");

    // The switch is the only thing that relaxes it, and it is not a one-way
    // door: a node finishes replaying and starts checking again. While
    // replaying the mempool shuts for its own reason — the node's state is
    // behind, so no verdict it reached would be about the chain that exists —
    // so the block path is where the relaxation shows.
    vm.set_bootstrapped(false);
    check(!vm.bootstrapped(), "a host replaying history says so, once");
    check(!vm.verify_tx(*forged) &&
              vm.verify_tx(*forged).error().find(kErrChainNotSynced) != std::string::npos,
          "…and a replaying node stops answering for the mempool at all");

    auto replayed = vm.parse(view(raw));
    check(replayed != nullptr && !replayed->verify(), "the same bytes are still not a valid block");
    check(replayed != nullptr &&
              why(replayed).find(kErrUnexpectedMerkleRoot) != std::string::npos,
          "…but it now fails on its root, having walked THROUGH the signature it "
          "was stopped at before");

    vm.set_bootstrapped(true);
    check(vm.bootstrapped(), "and the chain goes back to checking when replay ends");
    check(!vm.verify_tx(*forged), "…refusing the same forged credential again");
    auto again = vm.parse(view(raw));
    check(again != nullptr && !again->verify() &&
              why(again).find(fx::kErrWrongSig) != std::string::npos,
          "…and stopping at the signature once more");
}

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

// ================= the chain comes back =================

// A chain that forgets what it accepted when the process exits is not a chain:
// it would re-sign a height it already signed, and it would re-install genesis
// on top of its own history. store_test proves the store is durable; this proves
// the CHAIN is — which is a different claim, because it is the VM that decides
// whether to read the store or to overwrite it.
//
// Each block below is a separate Vm over the SAME file. The first is destroyed
// before the second opens, so nothing is carried across in memory: everything
// the second knows, it read back from disk.

void the_chain_survives_a_restart() {
    std::printf("\n  -- a restart --\n");
    const std::string path =
        "/tmp/xvm-vm-restart-" + std::to_string(::getpid()) + "-" + std::to_string(::time(nullptr));
    ::unlink(path.c_str());

    auto genesis = genesis_asset(4);
    const Id asset = genesis->id();
    VmConfig cfg;
    cfg.network_id = kNetworkID;
    cfg.chain_id = chain_id();
    cfg.net_id = id(0x0A);
    cfg.fee_asset_id = asset;

    Id accepted_id{};
    Id accepted_root{};
    Id spent_utxo{};
    Id produced_utxo{};
    Id spent_tx{};

    {  // ---- the first boot: genesis, then one accepted block ----
        auto f = store::File::open(path);
        if (!f) {
            check(false, "the store opens: " + f.error());
            return;
        }
        Vm vm(cfg, the_fxs(), **f);
        auto r = vm.initialize({genesis}, kGenesisTime);
        check(r.has_value(), r ? "the first boot installs genesis" : "first boot: " + r.error());
        vm.set_bootstrapped(true);
        vm.set_now(kGenesisTime + 1);

        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(
            txs::TransferableInput{txs::UTXOID{asset, 0, false}, asset, tin(kStartingBalance)});
        utx->base.outs.push_back(txs::TransferableOutput{asset, tout(100)});
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
        (void)tx->initialize();

        check(vm.issue(tx).has_value(), "a transaction is issued");
        auto blk = vm.build();
        if (blk == nullptr) {
            check(false, "build: " + vm.last_error());
            return;
        }
        blk->accept();
        check(vm.last_accepted_height() == 1, "the first boot accepts a block at height 1");

        accepted_id = blk->id();
        accepted_root = blk->root();
        spent_tx = tx->id();
        spent_utxo = txs::UTXOID{asset, 0, false}.input_id();
        produced_utxo = txs::UTXOID{tx->id(), 0, false}.input_id();
    }  // the Vm and the File are destroyed here — this IS the process exiting

    {  // ---- the second boot: the same file, a brand-new VM ----
        auto f = store::File::open(path);
        if (!f) {
            check(false, "the store reopens: " + f.error());
            return;
        }
        Vm vm(cfg, the_fxs(), **f);

        // Genesis is offered again, exactly as a host would offer it on every
        // boot. The store already holds a chain, so it must NOT be installed a
        // second time — that is the VM's decision, read out of the store rather
        // than passed in as a flag.
        auto r = vm.initialize({genesis_asset(4)}, kGenesisTime);
        check(r.has_value(), r ? "the second boot comes up" : "second boot: " + r.error());
        vm.set_bootstrapped(true);
        vm.set_now(kGenesisTime + 2);

        check(vm.last_accepted() == accepted_id, "it remembers the block it accepted");
        check(vm.last_accepted_height() == 1, "…and the height that block closed");

        auto blk = vm.get(accepted_id);
        check(blk != nullptr, "the accepted block itself is still there");
        check(blk != nullptr && blk->root() == accepted_root,
              "…carrying the execution root it was accepted with");

        check(!vm.chain_state().get_utxo(spent_utxo).has_value(),
              "the UTXO that block spent is still spent");
        check(vm.chain_state().get_utxo(produced_utxo).has_value(),
              "…and the one it produced is still there");
        check(vm.chain_state().get_tx(spent_tx).has_value(),
              "…and the transaction that did it is still known");
        check(vm.chain_state().utxo_count() == 4,
              "the whole occupied set came back — three genesis outputs and one produced");

        auto h1 = vm.chain_state().get_block_id_at_height(1);
        check(h1 && *h1 == accepted_id, "height 1 still resolves to that block");

        // The chain does not merely remember: it CONTINUES. A restart that could
        // read but not extend would still be a chain that has to re-sign.
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(
            txs::TransferableInput{txs::UTXOID{asset, 1, false}, asset, tin(kStartingBalance)});
        utx->base.outs.push_back(txs::TransferableOutput{asset, tout(200)});
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
        (void)tx->initialize();

        check(vm.issue(tx).has_value(), "a new transaction is admitted against the read-back state");
        auto next = vm.build();
        if (next == nullptr) {
            check(false, "the restarted chain builds: " + vm.last_error());
            return;
        }
        check(next->parent() == accepted_id, "the next block extends what was on disk");
        check(next->height() == 2, "…at the next height");
        next->accept();
        check(vm.last_accepted_height() == 2, "…and the chain moves on");
    }

    // Re-spending, after a restart, an output the chain consumed BEFORE the
    // restart. This is the double-spend the whole exercise is about: a node that
    // came back empty would accept it.
    {
        auto f = store::File::open(path);
        if (!f) {
            check(false, "the store reopens a third time: " + f.error());
            return;
        }
        Vm vm(cfg, the_fxs(), **f);
        (void)vm.initialize({genesis_asset(4)}, kGenesisTime);
        vm.set_bootstrapped(true);
        vm.set_now(kGenesisTime + 3);
        check(vm.last_accepted_height() == 2, "the third boot is at height 2");

        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(
            txs::TransferableInput{txs::UTXOID{asset, 0, false}, asset, tin(kStartingBalance)});
        utx->base.outs.push_back(txs::TransferableOutput{asset, tout(300)});
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
        (void)tx->initialize();

        auto r = vm.issue(tx);
        check(!r && r.error().find(state::kErrNotFound) != std::string::npos,
              "an output spent before the restart cannot be spent after it");
    }

    ::unlink(path.c_str());
}

// ================= the other half of being decided =================

// Ported from block/executor.TestBlockReject. Go's version is built on mocks, so
// what it can assert is that Reject succeeds and frees the block's pinned state.
// Here the chain is real, which lets the same two cases assert the thing the
// mocks stand in for: WHICH transactions come back.
//
// That is the whole reason reject is not optional. A rejected block's
// transactions were never refused — they lost a race — so a node that dropped
// them would hold a different pending set from every node that returned them,
// and the next block it proposed would differ. The last check in each case is
// therefore the one that matters: what this node builds NEXT.

// siblings builds two blocks at the same height over the same parent: consensus
// will decide between them. `first` is issued and sealed before `second` is
// issued, so neither is in the other's pool.
struct Siblings {
    std::shared_ptr<lux::node::Block> first;
    std::shared_ptr<lux::node::Block> second;
};

Siblings siblings(Chain& c, const std::vector<std::shared_ptr<txs::Tx>>& first_txs,
                  const std::vector<std::shared_ptr<txs::Tx>>& second_txs) {
    Siblings s;
    for (const auto& tx : first_txs) (void)c.vm->issue(tx);
    s.first = c.vm->build();
    for (const auto& tx : second_txs) (void)c.vm->issue(tx);
    s.second = c.vm->build();
    return s;
}

// reject_of reaches the port's reject. The node's seam declares accept and not
// reject (see vm.hpp), so this names the concrete block; the day the seam grows
// `reject`, this cast is the only line here that changes.
VmBlock* reject_of(const std::shared_ptr<lux::node::Block>& blk) {
    return dynamic_cast<VmBlock*>(blk.get());
}

void reject_mixed() {
    std::printf("\n  -- rejecting a block: some of its txs still hold --\n");
    Chain c;

    // The winner spends genesis output 0.
    auto winner = c.spend(0, 100);
    // The loser holds two transactions: one that spends the SAME output 0 — so
    // the winner's acceptance kills it — and one that spends output 1, which the
    // winner never touched and which therefore survives the loss.
    auto doomed = c.spend(0, 200);
    auto survivor = c.spend(1, 300);

    auto s = siblings(c, {winner}, {doomed, survivor});
    if (s.first == nullptr || s.second == nullptr) {
        check(false, "the two siblings build: " + c.vm->last_error());
        return;
    }
    check(s.first->height() == s.second->height(), "the two blocks are at the same height");
    check(s.first->parent() == s.second->parent(), "…over the same parent");
    check(s.first->id() != s.second->id(), "…and they are different blocks");
    check(c.vm->mempool_size() == 0, "building drained the pool");

    s.first->accept();
    check(c.vm->last_accepted() == s.first->id(), "consensus accepted the first");

    VmBlock* loser = reject_of(s.second);
    if (loser == nullptr) {
        check(false, "the losing block can be rejected");
        return;
    }
    check(c.vm->get_state(s.second->id()) != nullptr,
          "the loser still pins its state before it is rejected");

    loser->reject();

    // Go frees exactly the rejected block's state: nothing may build on it now.
    check(c.vm->get_state(s.second->id()) == nullptr, "rejecting frees the block's pinned state");

    // The verdict on each transaction is reached AGAIN, against the state that
    // actually won — not remembered from the block that lost.
    check(c.vm->pool().get(survivor->id()) != nullptr,
          "the tx that still holds is back in the mempool");
    check(c.vm->pool().get(doomed->id()) == nullptr,
          "the tx the winner invalidated is not");
    check(c.vm->mempool_size() == 1, "…and those are the only two verdicts reached");

    // NOT marked dropped. A drop reason is a cached refusal, and a transaction
    // invalidated by a reorganisation is exactly the kind that can become valid
    // again — caching a refusal for it would refuse what Go admits. Go's Reject
    // logs both failures and remembers neither.
    check(c.vm->pool().drop_reason(doomed->id()).empty(),
          "an invalidated tx is let go, not remembered as dropped");
    check(c.vm->pool().drop_reason(survivor->id()).empty(),
          "and neither is one that came back");

    // THE POINT: what this node proposes next. A chain that had dropped the
    // survivor would build nothing here, and would disagree with every node that
    // kept it about what is still pending.
    auto next = c.vm->build();
    if (next == nullptr) {
        check(false, "the returned tx is proposed in the next block: " + c.vm->last_error());
        return;
    }
    check(next->parent() == s.first->id(), "the next block extends the accepted sibling");
    check(next->height() == 2, "…at the next height");
    auto* std_next = dynamic_cast<VmBlock*>(next.get());
    check(std_next != nullptr && std_next->standard()->transactions.size() == 1 &&
              std_next->standard()->transactions[0]->id() == survivor->id(),
          "…and it carries exactly the transaction rejection returned");
}

void reject_all_valid() {
    std::printf("\n  -- rejecting a block: all of its txs still hold --\n");
    Chain c;

    // Go's second case. The two blocks touch nothing in common, so losing the
    // race costs the loser's transactions nothing at all.
    auto winner = c.spend(0, 100);
    auto lost1 = c.spend(1, 200);
    auto lost2 = c.spend(2, 300);

    auto s = siblings(c, {winner}, {lost1, lost2});
    if (s.first == nullptr || s.second == nullptr) {
        check(false, "the two siblings build: " + c.vm->last_error());
        return;
    }
    s.first->accept();

    VmBlock* loser = reject_of(s.second);
    if (loser == nullptr) {
        check(false, "the losing block can be rejected");
        return;
    }
    loser->reject();

    check(c.vm->mempool_size() == 2, "every transaction comes back");
    check(c.vm->pool().get(lost1->id()) != nullptr, "…the first of them");
    check(c.vm->pool().get(lost2->id()) != nullptr, "…and the second");
    check(c.vm->get_state(s.second->id()) == nullptr, "…and the block's state is freed");

    auto next = c.vm->build();
    auto* std_next = next ? dynamic_cast<VmBlock*>(next.get()) : nullptr;
    check(std_next != nullptr && std_next->standard()->transactions.size() == 2,
          "both are proposed again in the next block");
}

void reject_what_is_not_there() {
    std::printf("\n  -- rejecting what this node does not hold --\n");
    Chain c;
    auto tx = c.spend(0, 100);
    (void)c.vm->issue(tx);
    auto blk = c.vm->build();
    if (blk == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    VmBlock* b = reject_of(blk);
    if (b == nullptr) {
        check(false, "the block can be rejected");
        return;
    }

    // Rejecting the same block twice is not a second event. The first reject
    // freed its state and returned its transactions; the second finds the block
    // in the store or nowhere, and either way must not return them a second time
    // — a pool holding two copies of one transaction would build a block that
    // spends the same input twice.
    b->reject();
    const std::size_t after_one = c.vm->mempool_size();
    b->reject();
    check(c.vm->mempool_size() == after_one, "rejecting twice returns nothing twice");
    check(after_one == 1, "…and the one transaction did come back");
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

// ================= the other half of a decision =================

// Go: block/executor/block.go Reject. A block that will never be accepted gives
// back what it was holding — its pinned state to nobody, and its transactions to
// the pool, each one asked AGAIN against the state that actually won so that
// what still holds returns and what no longer does is remembered as dropped.
//
// This was written and unreachable: the node's seam declared accept and nothing
// else, so nothing could ask for it. It is reachable now, and the engine asks on
// every block it gives up on — which is why the property is worth pinning here
// rather than leaving to the node.
void losing_block() {
    std::printf("\n  -- rejected --\n");
    Chain c;

    const Id genesis = c.vm->last_accepted();
    auto tx = c.spend(0, 100);
    check(c.vm->issue(tx).has_value(), "the tx is issued");
    check(c.vm->mempool_size() == 1, "…and is waiting for a block");

    auto blk = c.vm->build();
    if (blk == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    check(c.vm->mempool_size() == 0, "building the block takes it out of the pool");
    check(c.vm->get_state(blk->id()) != nullptr, "…and pins the state that block would produce");

    blk->reject();
    check(c.vm->mempool_size() == 1, "rejecting the block hands the transaction back");
    check(c.vm->pool().get(tx->id()) != nullptr, "…the same transaction, by id");
    check(c.vm->pool().drop_reason(tx->id()).empty(),
          "…not remembered as dropped: it lost a race, it was not refused");
    check(c.vm->get_state(blk->id()) == nullptr,
          "…and the pinned state is released, so nothing can be built on it");

    // Nothing the block would have done was done.
    check(c.vm->last_accepted() == genesis, "the chain did not move");
    check(c.vm->chain_state().get_utxo(c.funded_utxo(0).input_id()).has_value(),
          "…and the output the block would have spent is still spendable");

    blk->reject();
    check(c.vm->mempool_size() == 1, "rejecting twice hands it back once");

    // Handed back means USABLE again, which is the whole reason to hand it back:
    // a node that swallowed it would disagree with every peer about what is
    // still pending, and then build a different block.
    auto next = c.vm->build();
    if (next == nullptr) {
        check(false, "rebuild: " + c.vm->last_error());
        return;
    }
    check(next->verify(), "the next block carries it and verifies");
    next->accept();
    check(c.vm->mempool_size() == 0, "…and accepting drains the pool");
    txs::UTXOID produced{tx->id(), 0, false};
    check(c.vm->chain_state().get_utxo(produced.input_id()).has_value(),
          "…the transaction landed after all, one height later");
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
    signature_checking_is_the_default();
    genesis_and_identity();
    issue_gate();
    happy_path();
    build_when_empty();
    refusals();
    root_is_recomputed();
    two_blocks();
    the_chain_survives_a_restart();
    reject_mixed();
    reject_all_valid();
    reject_what_is_not_there();
    preference();
    losing_block();
    accept_unverified();
    seam_answers();
    through_the_seam_only();
    reject_returns_the_transactions();
    return report("vm");
}
