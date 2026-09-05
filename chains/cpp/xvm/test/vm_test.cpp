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
#include <ctime>
#include <functional>
#include <map>
#include <optional>
#include <string>
#include <type_traits>
#include <unistd.h>

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

// ---- the cross-chain surface, stated ----
//
// A peer chain the X-Chain may import from and export to, and the shared memory
// between them. Both are test doubles for the seams in executor.hpp: the chain
// asks which network a peer is on, and reads the UTXO bytes a peer put there.

Id peer_chain() { return id(0x77); }

struct Nets final : executor::NetLookup {
    std::map<Id, Id> of;
    wire::Result<Id> network_of(const Id& chain) const override {
        auto it = of.find(chain);
        if (it == of.end()) return std::unexpected("unknown chain");
        return it->second;
    }
};

struct Memory final : executor::SharedMemory {
    std::map<Bytes, Bytes> rows;
    wire::Result<std::vector<Bytes>> get(const Id&,
                                         const std::vector<Bytes>& keys) const override {
        std::vector<Bytes> out;
        for (const auto& k : keys) {
            auto it = rows.find(k);
            if (it == rows.end()) return std::unexpected("not found in shared memory");
            out.push_back(it->second);
        }
        return out;
    }
    void put(const txs::UTXO& utxo) {
        auto b = utxo.wire_bytes();
        if (!b) return;
        Id key = utxo.utxo_id.input_id();
        rows[Bytes(key.begin(), key.end())] = *b;
    }
};

// Chain is a booted VM: genesis installed, bootstrapped, clock set.
//
// The peer chain and the shared memory between them are DECLARED BEFORE the VM,
// so they outlive it: the backend holds raw pointers to both and a VM torn down
// after its own seams would read freed memory on the way out.
struct Chain {
    store::Memory store;
    Nets nets;
    Memory memory;
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

        // The peer sits on the same network, which is what makes it importable
        // from and exportable to at all.
        nets.of[peer_chain()] = cfg.net_id;
        vm->backend().net_lookup = &nets;
        vm->backend().shared_memory = &memory;
    }

    // seal issues a transaction and drives the block that carries it all the way
    // to acceptance — Go's issueAndAccept. It answers whether the whole journey
    // worked, so a caller that only cares that a transaction lands says so once.
    bool seal(const std::shared_ptr<txs::Tx>& tx, const std::string& what) {
        auto issued = vm->issue(tx);
        if (!issued) {
            check(false, what + ": " + issued.error());
            return false;
        }
        auto blk = vm->build();
        if (blk == nullptr) {
            check(false, what + " (build): " + vm->last_error());
            return false;
        }
        if (!blk->verify()) {
            auto* vb = dynamic_cast<VmBlock*>(blk.get());
            check(false, what + " (verify): " + (vb == nullptr ? "" : vb->error()));
            return false;
        }
        blk->accept();
        const bool ok = vm->last_accepted() == blk->id();
        check(ok, what);
        return ok;
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

// ================= the empty root is not a root =================
//
// Ported from vm_merkleroot_test.TestMerkleRootEmptyRootRejected. The historical
// pre-activation shape of a block carried ids.Empty where the execution root now
// sits, and there is no surviving path that accepts it: the executor recomputes
// the real root and refuses the mismatch like any other.
//
// root_is_recomputed() flips ONE BYTE of an honest root. This is the other shape —
// the root a block that never computed one would carry — and it is the shape a
// node running old code would actually emit, so it is worth its own case.
void the_empty_root_is_refused() {
    std::printf("\n  -- a block that declares no root at all --\n");
    Chain c;

    auto tx = c.spend(0, 100);
    (void)c.vm->issue(tx);
    auto built = c.vm->build();
    if (built == nullptr) {
        check(false, "build: " + c.vm->last_error());
        return;
    }
    check(built->root() != kEmptyId, "the built block's root is not empty");

    // A sibling identical to the built block in every field but the root, which is
    // left empty.
    auto* vb = dynamic_cast<VmBlock*>(built.get());
    if (vb == nullptr) {
        check(false, "the built block is a VmBlock");
        return;
    }
    auto empty = block::build(vb->standard()->parent_id, vb->standard()->height,
                              vb->standard()->time, kEmptyId, vb->standard()->transactions);
    if (!empty) {
        check(false, "the empty-root sibling builds: " + empty.error());
        return;
    }
    check((*empty)->root == kEmptyId, "…and it declares the empty root");
    check((*empty)->block_id != built->id(),
          "…which makes it a different block, because the root is part of the id");

    auto parsed = c.vm->parse(view((*empty)->bytes));
    check(parsed != nullptr, "the empty-root block parses");
    if (parsed == nullptr) return;
    check(!parsed->verify(), "…and does not verify");
    auto* pb = dynamic_cast<VmBlock*>(parsed.get());
    check(pb != nullptr && pb->error().find(kErrUnexpectedMerkleRoot) != std::string::npos,
          "…because the real execution root is not empty");
}

// ================= imported inputs may be spent once =================
//
// Ported from block/executor.TestVerifyUniqueInputs and the "tx imported inputs
// overlap" case of TestBlockVerify. Both are about the same thing and neither is
// about ordinary double-spending: an imported UTXO is not a row in this chain's
// table, so nothing local goes missing when two transactions both claim it. The
// only thing that stops the second is this check.

// imported_utxo puts a UTXO of the chain's asset into shared memory and returns
// the (UTXOID, amount) an ImportTx names it by.
txs::UTXOID put_in_shared_memory(Chain& c, std::uint8_t seed, std::uint64_t amount) {
    txs::UTXOID uid{id(seed), 0, false};
    c.memory.put(txs::UTXO{uid, c.asset, tout(amount, 0)});
    return uid;
}

// import_of builds a signed ImportTx that consumes `uid` from the peer chain and
// pays `pays` back out on this chain.
std::shared_ptr<txs::Tx> import_of(const Chain& c, const txs::UTXOID& uid, std::uint64_t amount,
                                   std::uint64_t pays) {
    auto utx = std::make_shared<txs::ImportTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->base.outs.push_back(txs::TransferableOutput{c.asset, tout(pays)});
    utx->source_chain = peer_chain();
    utx->imported_ins = {txs::TransferableInput{uid, c.asset, tin(amount)}};

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
    tx->creds.push_back(cred);
    (void)tx->initialize();
    return tx;
}

void an_imported_input_is_spent_once() {
    std::printf("\n  -- an imported input, claimed twice --\n");

    // Go's first case: no inputs at all is not a conflict with anything.
    {
        Chain c;
        auto tx = c.spend(0, 100);
        check(c.vm->issue(tx).has_value(),
              "a transaction that imports nothing has nothing to conflict with");
        auto blk = c.vm->build();
        check(blk != nullptr && blk->verify(), "…and its block verifies");
    }

    // Two transactions in ONE block, both importing the same UTXO. The second is
    // refused as a conflict inside the block, before the root is ever compared.
    {
        Chain c;
        auto uid = put_in_shared_memory(c, 0x55, kStartingBalance);
        auto a = import_of(c, uid, kStartingBalance, 100);
        auto b = import_of(c, uid, kStartingBalance, 200);
        check(a->id() != b->id(), "the two imports are different transactions");

        auto blk = forge(c, c.vm->last_accepted(), 1, c.vm->now(), kEmptyId, {a, b});
        auto v = c.vm->parse(view(blk->bytes));
        check(v != nullptr && !v->verify(),
              "a block importing one UTXO twice is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(kErrConflictingBlockTxs) != std::string::npos,
              "…as conflicting transactions inside the block");
    }

    // The same claim, split across a block and its child. blk1 is verified but NOT
    // accepted, so its imported input is still pinned; a child that claims it too
    // is refused against the ancestry.
    {
        Chain c;
        auto uid = put_in_shared_memory(c, 0x56, kStartingBalance);
        auto first = import_of(c, uid, kStartingBalance, 100);
        check(c.vm->issue(first).has_value(), "the first import is admitted");
        auto blk1 = c.vm->build();
        if (blk1 == nullptr) {
            check(false, "the first block builds: " + c.vm->last_error());
            return;
        }
        check(c.vm->get_state(blk1->id()) != nullptr,
              "…and the block pins its state without being accepted");

        auto second = import_of(c, uid, kStartingBalance, 200);
        auto child = forge(c, blk1->id(), 2, c.vm->now(), kEmptyId, {second});
        auto v = c.vm->parse(view(child->bytes));
        check(v != nullptr && !v->verify(),
              "a child claiming its parent's imported input is refused");
        auto* vb = dynamic_cast<VmBlock*>(v.get());
        check(vb != nullptr && vb->error().find(kErrConflictingParentTxs) != std::string::npos,
              "…as conflicting with an undecided ancestor");

        // A child importing something ELSE is fine, so the refusal above is about
        // the shared input rather than about having an undecided parent at all.
        auto other_uid = put_in_shared_memory(c, 0x57, kStartingBalance);
        auto other = import_of(c, other_uid, kStartingBalance, 300);
        check(c.vm->issue(other).has_value(), "a second, unrelated import is admitted");
        c.vm->prefer(blk1->id());
        auto blk2 = c.vm->build();
        check(blk2 != nullptr, blk2 != nullptr
                                   ? "…and a child carrying it builds on the undecided parent"
                                   : "the unrelated child builds: " + c.vm->last_error());
        check(blk2 != nullptr && blk2->parent() == blk1->id(), "…extending that parent");
    }
}

// ================= a UTXO that arrived, and one that left =================
//
// Ported from vm_test.TestIssueImportTx / TestIssueExportTx /
// TestClearForceAcceptedExportTx.
//
// WHAT IS NOT ASSERTED, and why. Each Go test ends by reading the PEER's shared
// memory: the source chain's UTXO must be gone after an import is accepted, and
// the destination chain must hold one indexed element after an export is. This
// port cannot be asked either question, because it never writes shared memory:
// executor::SharedMemory declares `get` and nothing else, and Vm::accept_block
// applies the block's diff and drops the atomic requests it collected during
// verification. Writing a test that "passed" over that gap would be a test of
// nothing, so the halves that exist are asserted and the missing half is named.
void a_utxo_arrives_from_another_chain() {
    std::printf("\n  -- importing --\n");
    Chain c;

    const std::uint64_t amount = 1010;
    auto uid = put_in_shared_memory(c, 0x58, amount);
    auto tx = import_of(c, uid, amount, amount);

    check(c.vm->chain_state().get_utxo(uid.input_id()).has_value() == false,
          "the imported UTXO is not a row in this chain's table");
    if (!c.seal(tx, "an import is issued, built and accepted")) return;

    // What the import produced IS a row here, denominated in the asset that
    // arrived.
    txs::UTXOID produced{tx->id(), 0, false};
    auto u = c.vm->chain_state().get_utxo(produced.input_id());
    check(u.has_value(), "…and what it paid out is a UTXO on this chain");
    check(u && u->asset_id == c.asset, "…of the asset that arrived");
    const auto* out = u ? dynamic_cast<const fx::FxTransferOut*>(u->out.get()) : nullptr;
    check(out != nullptr && out->amount() == amount, "…carrying the whole amount");

    // The imported UTXO is still not a local row: importing does not copy the
    // source chain's table, it consumes from shared memory.
    check(!c.vm->chain_state().get_utxo(uid.input_id()).has_value(),
          "the source chain's UTXO never becomes a row here");

    // An import naming a UTXO shared memory does not hold is refused rather than
    // treated as an empty input.
    auto missing = import_of(c, txs::UTXOID{id(0x59), 0, false}, amount, amount);
    auto r = c.vm->issue(missing);
    check(!r, "an import of a UTXO nobody exported is refused");
    check(!r && r.error().find("shared memory") != std::string::npos,
          "…because shared memory does not hold it");
}

void a_utxo_leaves_for_another_chain() {
    std::printf("\n  -- exporting --\n");
    Chain c;

    auto utx = std::make_shared<txs::ExportTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->base.ins.push_back(
        txs::TransferableInput{c.funded_utxo(0), c.asset, tin(kStartingBalance)});
    utx->base.outs.push_back(txs::TransferableOutput{c.asset, tout(kStartingBalance - 400)});
    utx->destination_chain = peer_chain();
    utx->exported_outs = {txs::TransferableOutput{c.asset, tout(400)}};

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
    tx->creds.push_back(cred);
    (void)tx->initialize();

    const std::size_t before = c.vm->chain_state().utxo_count();
    if (!c.seal(tx, "an export is issued, built and accepted")) return;

    // The change stayed; the exported output did not. This is the whole local
    // effect of an export, and the count is the check that nothing else moved.
    txs::UTXOID change{tx->id(), 0, false};
    txs::UTXOID exported{tx->id(), 1, false};
    check(c.vm->chain_state().get_utxo(change.input_id()).has_value(),
          "the change output stays on this chain");
    check(!c.vm->chain_state().get_utxo(exported.input_id()).has_value(),
          "the exported output does not — it left");
    check(!c.vm->chain_state().get_utxo(c.funded_utxo(0).input_id()).has_value(),
          "…and the output it was funded from is spent");
    check(c.vm->chain_state().utxo_count() == before,
          "one UTXO in, one out: nothing else moved");

    // An export to a chain on ANOTHER network is refused: value must not leave
    // for a chain this one is not on.
    {
        Chain d;
        d.nets.of[id(0x78)] = id(0xFF);  // a peer on a different network
        auto bad = std::make_shared<txs::ExportTx>();
        bad->base.network_id = kNetworkID;
        bad->base.blockchain_id = chain_id();
        bad->base.ins.push_back(
            txs::TransferableInput{d.funded_utxo(0), d.asset, tin(kStartingBalance)});
        bad->base.outs.push_back(
            txs::TransferableOutput{d.asset, tout(kStartingBalance - 400)});
        bad->destination_chain = id(0x78);
        bad->exported_outs = {txs::TransferableOutput{d.asset, tout(400)}};
        auto btx = std::make_shared<txs::Tx>();
        btx->unsigned_tx = bad;
        auto bc = std::make_shared<fx::secp256k1fx::Credential>();
        bc->signatures = {sign_unsigned_tx(test_key(0), view(bad->bytes()))};
        btx->creds.push_back(bc);
        (void)btx->initialize();

        auto r = d.vm->issue(btx);
        check(!r && r.error().find(executor::kErrMismatchedNetIDs) != std::string::npos,
              "an export to a chain on another network is refused");
    }
}

// ================= the other two fx families, end to end =================
//
// Ported from vm_test.TestIssueNFT and TestIssueProperty. Both are the same
// journey: define an asset whose initial state is a MINT AUTHORITY of a non-value
// fx, run an operation that spends that authority, and then run a second
// operation over what the first produced. fx_test proves each gate in isolation;
// this proves the chain carries an OperationTx from the mempool to the UTXO set.

// create_asset issues, builds and accepts a CreateAssetTx carrying `states`, and
// returns it. With no inputs and no fee there is nothing to authorize, which is
// why it needs no credential.
std::shared_ptr<txs::Tx> create_asset(Chain& c, const std::string& name,
                                      const std::string& symbol,
                                      std::vector<txs::InitialState> states) {
    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->name = name;
    utx->symbol = symbol;
    utx->denomination = 0;
    std::sort(states.begin(), states.end(),
              [](const txs::InitialState& a, const txs::InitialState& b) {
                  return a.compare(b) < 0;
              });
    utx->states = std::move(states);

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

// operation_tx builds a signed OperationTx over one operation, with the fx
// credential the operation's own family requires.
std::shared_ptr<txs::Tx> operation_tx(const Id& asset_id, const txs::UTXOID& consumes,
                                      std::shared_ptr<fx::FxOperation> op,
                                      wire::TypeKind family, int signer) {
    auto utx = std::make_shared<txs::OperationTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    txs::Operation o;
    o.asset_id = asset_id;
    o.utxo_ids = {consumes};
    o.op = std::move(op);
    utx->ops = {o};

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    const auto sig = sign_unsigned_tx(test_key(signer), view(utx->bytes()));
    if (family == wire::TypeKind::NFT) {
        auto cred = std::make_shared<fx::nftfx::Credential>();
        cred->signatures = {sig};
        tx->creds.push_back(cred);
    } else {
        auto cred = std::make_shared<fx::propertyfx::Credential>();
        cred->signatures = {sig};
        tx->creds.push_back(cred);
    }
    (void)tx->initialize();
    return tx;
}

fx::OutputOwners owned_by(int key) { return fx::OutputOwners{0, 1, {test_address(key)}}; }

void the_nft_family_end_to_end() {
    std::printf("\n  -- an NFT: define, mint, move --\n");
    Chain c;

    // The asset declares TWO families: secp256k1 at index 0 holding a unit of
    // value, and nftfx at index 1 holding the mint authority. That is Go's
    // TestVerifyFxUsage shape, and it is the one that matters — an asset with a
    // single fx cannot tell "the fx this asset declared" from "the only fx there
    // is", so a check that looked at the wrong one would still pass.
    txs::InitialState value;
    value.fx_index = 0;
    value.outs = {tout(1, 0)};
    value.sort();

    auto mint_authority = std::make_shared<fx::nftfx::MintOutput>();
    mint_authority->group_id = 1;
    mint_authority->out_owners = owned_by(0);
    txs::InitialState authority;
    authority.fx_index = 1;
    authority.outs = {mint_authority};
    authority.sort();

    auto create = create_asset(c, "Team Rocket", "TR", {value, authority});
    if (!c.seal(create, "the asset is defined")) return;
    const Id nft_asset = create->id();

    // Index 0 is the secp value output, index 1 the nft mint authority: base
    // outputs first (there are none), then each state's outputs in state order.
    auto value_utxo = c.vm->chain_state().get_utxo(txs::UTXOID{nft_asset, 0, false}.input_id());
    auto authority_utxo =
        c.vm->chain_state().get_utxo(txs::UTXOID{nft_asset, 1, false}.input_id());
    check(value_utxo.has_value(), "its unit of value is a UTXO");
    check(authority_utxo.has_value(), "…and its mint authority is another");
    check(authority_utxo &&
              dynamic_cast<fx::nftfx::MintOutput*>(authority_utxo->out.get()) != nullptr,
          "…which is an nftfx mint output");

    // Mint. The operation spends the authority and pays out an NFT.
    auto mint = std::make_shared<fx::nftfx::MintOperation>();
    mint->mint_input.sig_indices = {0};
    mint->group_id = 1;
    mint->payload = Bytes{'h', 'e', 'l', 'l', 'o'};
    mint->outputs = {std::make_shared<fx::OutputOwners>(owned_by(0))};

    auto mint_tx = operation_tx(nft_asset, txs::UTXOID{nft_asset, 1, false}, mint,
                                wire::TypeKind::NFT, 0);
    if (!c.seal(mint_tx, "the NFT is minted")) return;

    check(!c.vm->chain_state().get_utxo(txs::UTXOID{nft_asset, 1, false}.input_id()).has_value(),
          "…and the authority it spent is gone");
    txs::UTXOID nft{mint_tx->id(), 0, false};
    auto minted = c.vm->chain_state().get_utxo(nft.input_id());
    check(minted.has_value(), "…leaving the NFT itself in the set");
    auto* nft_out = minted ? dynamic_cast<fx::nftfx::TransferOutput*>(minted->out.get()) : nullptr;
    check(nft_out != nullptr, "…as an nftfx transfer output");
    check(nft_out != nullptr && nft_out->group_id == 1, "…in the group it was minted for");
    check(nft_out != nullptr && nft_out->payload == Bytes({'h', 'e', 'l', 'l', 'o'}),
          "…carrying the payload it was minted with");
    check(nft_out != nullptr && nft_out->out_owners.addrs == std::vector<ShortId>{test_address(0)},
          "…owned by whoever minted it");

    // Move it to someone else.
    auto move = std::make_shared<fx::nftfx::TransferOperation>();
    move->input.sig_indices = {0};
    move->output.group_id = 1;
    move->output.payload = Bytes{'h', 'e', 'l', 'l', 'o'};
    move->output.out_owners = owned_by(2);

    auto move_tx = operation_tx(nft_asset, nft, move, wire::TypeKind::NFT, 0);
    if (!c.seal(move_tx, "the NFT is moved")) return;

    check(!c.vm->chain_state().get_utxo(nft.input_id()).has_value(),
          "…the old owner's UTXO is gone");
    auto moved = c.vm->chain_state().get_utxo(txs::UTXOID{move_tx->id(), 0, false}.input_id());
    auto* moved_out = moved ? dynamic_cast<fx::nftfx::TransferOutput*>(moved->out.get()) : nullptr;
    check(moved_out != nullptr &&
              moved_out->out_owners.addrs == std::vector<ShortId>{test_address(2)},
          "…and the NFT is the new owner's");

    // TestVerifyFxUsage's own point: the SECP value of that two-family asset is
    // still spendable as ordinary value. An fx-usage check that read the asset's
    // last declared family, or its first, rather than asking whether the family in
    // hand is among them, would refuse this.
    auto spend = std::make_shared<txs::BaseTx>();
    spend->base.network_id = kNetworkID;
    spend->base.blockchain_id = chain_id();
    spend->base.ins.push_back(txs::TransferableInput{txs::UTXOID{nft_asset, 0, false},
                                                     nft_asset, tin(1)});
    spend->base.outs.push_back(txs::TransferableOutput{nft_asset, tout(1, 2)});
    auto spend_tx = std::make_shared<txs::Tx>();
    spend_tx->unsigned_tx = spend;
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    cred->signatures = {sign_unsigned_tx(test_key(0), view(spend->bytes()))};
    spend_tx->creds.push_back(cred);
    (void)spend_tx->initialize();
    check(c.seal(spend_tx, "the asset's ordinary value is still spendable"),
          "…so declaring a second fx did not cost the asset its first");
}

void the_property_family_end_to_end() {
    std::printf("\n  -- a property: define, mint, burn --\n");
    Chain c;

    // propertyfx is the third family this chain registers (the_fxs()), so the
    // asset declares index 2. Go's test registers only two and says 1; the index
    // is the fx's POSITION in the host's list, not a constant, which is exactly
    // why it is declared per asset rather than assumed.
    auto authority = std::make_shared<fx::propertyfx::MintOutput>();
    authority->out_owners = owned_by(0);
    txs::InitialState state;
    state.fx_index = 2;
    state.outs = {authority};
    state.sort();

    auto create = create_asset(c, "Team Rocket", "TR", {state});
    if (!c.seal(create, "the asset is defined")) return;
    const Id prop_asset = create->id();

    // Mint. The operation re-creates the authority and pays out an owned output.
    //
    // Go's mint leaves OwnedOutput zero-valued; this port refuses a threshold of
    // zero outright (an output nobody can authorize is not an output), so the
    // owned output is given a real owner. That is the only difference, and it
    // makes the burn below a real spend rather than a spend of nothing.
    auto mint = std::make_shared<fx::propertyfx::MintOperation>();
    mint->mint_input.sig_indices = {0};
    mint->mint_output.out_owners = owned_by(0);
    mint->owned_output.out_owners = owned_by(0);

    auto mint_tx = operation_tx(prop_asset, txs::UTXOID{prop_asset, 0, false}, mint,
                                wire::TypeKind::Property, 0);
    if (!c.seal(mint_tx, "the property is minted")) return;

    txs::UTXOID re_minted{mint_tx->id(), 0, false};
    txs::UTXOID owned{mint_tx->id(), 1, false};
    auto a = c.vm->chain_state().get_utxo(re_minted.input_id());
    auto o = c.vm->chain_state().get_utxo(owned.input_id());
    check(a && dynamic_cast<fx::propertyfx::MintOutput*>(a->out.get()) != nullptr,
          "…the mint authority is re-created at index 0");
    check(o && dynamic_cast<fx::propertyfx::OwnedOutput*>(o->out.get()) != nullptr,
          "…and the owned output is at index 1");
    check(!c.vm->chain_state().get_utxo(txs::UTXOID{prop_asset, 0, false}.input_id()).has_value(),
          "…while the authority it spent is gone");

    // Burn. A burn produces nothing at all, which is the point: the owned output
    // simply stops existing.
    auto burn = std::make_shared<fx::propertyfx::BurnOperation>();
    burn->input.sig_indices = {0};

    const std::size_t before = c.vm->chain_state().utxo_count();
    auto burn_tx = operation_tx(prop_asset, owned, burn, wire::TypeKind::Property, 0);
    if (!c.seal(burn_tx, "the property is burned")) return;

    check(!c.vm->chain_state().get_utxo(owned.input_id()).has_value(),
          "…and what was burned is gone");
    check(c.vm->chain_state().utxo_count() == before - 1,
          "…having produced nothing to replace it");
    check(c.vm->chain_state().get_utxo(re_minted.input_id()).has_value(),
          "…while the mint authority is untouched");
}

// ================= two assets in one transaction =================
//
// Ported from vm_test.TestIssueTxWithFeeAsset and TestIssueTxWithAnotherAsset. The
// X-Chain is a multi-asset settlement layer, so a transaction may move the fee
// asset and some other asset at once, and the flow check has to balance EACH asset
// separately. A checker that summed them would let a shortfall in one be paid for
// by a surplus in the other.
void two_assets_in_one_transaction() {
    std::printf("\n  -- two assets, one transaction --\n");
    Chain c;

    // A second asset, whose whole supply is a genesis-style initial state.
    txs::InitialState state;
    state.fx_index = 0;
    state.outs = {tout(kStartingBalance, 0)};
    state.sort();
    auto create = create_asset(c, "Second", "SEC", {state});
    if (!c.seal(create, "a second asset is defined")) return;
    const Id other = create->id();

    // One transaction spending a UTXO of EACH asset and paying each back out.
    // Inputs must be sorted and unique, which the builder below leans on the
    // tx-layer sort for rather than assuming an order.
    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->base.ins.push_back(
        txs::TransferableInput{c.funded_utxo(0), c.asset, tin(kStartingBalance)});
    utx->base.ins.push_back(
        txs::TransferableInput{txs::UTXOID{other, 0, false}, other, tin(kStartingBalance)});
    std::sort(utx->base.ins.begin(), utx->base.ins.end(),
              [](const txs::TransferableInput& a, const txs::TransferableInput& b) {
                  return a.utxo_id.compare(b.utxo_id) < 0;
              });
    utx->base.outs.push_back(txs::TransferableOutput{c.asset, tout(kStartingBalance, 1)});
    utx->base.outs.push_back(txs::TransferableOutput{other, tout(kStartingBalance, 1)});
    txs::sort_transferable_outputs(utx->base.outs);

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    for (std::size_t i = 0; i < utx->base.ins.size(); ++i) {
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
    }
    (void)tx->initialize();

    if (!c.seal(tx, "a transaction moving both assets is accepted")) return;

    // Both payments landed, each denominated in the asset it came from.
    bool saw_fee_asset = false, saw_other = false;
    for (std::uint32_t i = 0; i < 2; ++i) {
        auto u = c.vm->chain_state().get_utxo(txs::UTXOID{tx->id(), i, false}.input_id());
        if (!u) continue;
        if (u->asset_id == c.asset) saw_fee_asset = true;
        if (u->asset_id == other) saw_other = true;
    }
    check(saw_fee_asset, "…paying out the fee asset");
    check(saw_other, "…and the other asset, in the same transaction");

    // And the balance is PER ASSET: a transaction paying out more of the second
    // asset than it consumed is refused even though the first asset is over-funded
    // by more than the shortfall.
    auto bad = std::make_shared<txs::BaseTx>();
    bad->base.network_id = kNetworkID;
    bad->base.blockchain_id = chain_id();
    bad->base.ins.push_back(
        txs::TransferableInput{c.funded_utxo(1), c.asset, tin(kStartingBalance)});
    bad->base.outs.push_back(txs::TransferableOutput{other, tout(1, 1)});
    auto bad_tx = std::make_shared<txs::Tx>();
    bad_tx->unsigned_tx = bad;
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    cred->signatures = {sign_unsigned_tx(test_key(0), view(bad->bytes()))};
    bad_tx->creds.push_back(cred);
    (void)bad_tx->initialize();

    auto r = c.vm->issue(bad_tx);
    check(!r && r.error().find(txs::kErrInsufficientFunds) != std::string::npos,
          "a surplus of one asset does not fund a shortfall in another");
}

// ================= what the builder leaves out =================
//
// Ported from block/builder.TestBuilderBuildBlock. Go drives its builder through
// mocks; here the transactions are real, so each case has to be made real too —
// which is stronger, because a mock can be made to fail in a way the code never
// fails.
void the_builder_leaves_out_what_will_not_hold() {
    std::printf("\n  -- what the builder drops --\n");

    // A transaction that passes the mempool gate and then stops holding, because
    // the chain moved underneath it. It is admitted against the last accepted
    // state, and by the time the builder tries it that state has advanced.
    {
        Chain c;
        auto first = c.spend(0, 100);
        auto second = c.spend(0, 200);  // same input as `first`

        check(c.vm->issue(first).has_value(), "the first transaction is admitted");
        auto blk = c.vm->build();
        if (blk == nullptr) {
            check(false, "build: " + c.vm->last_error());
            return;
        }
        blk->accept();

        // `second` spends what `first` already spent. The gate refuses it now, so
        // it is put into the POOL directly — the builder must be the thing that
        // drops it, which is what this case is about.
        check(c.vm->pool().add(second).has_value(),
              "a stale transaction is placed in the pool directly");
        check(c.vm->mempool_size() == 1, "…and the pool holds it");

        auto next = c.vm->build();
        check(next == nullptr, "the builder builds nothing from it");
        check(c.vm->mempool_size() == 0, "…and it is out of the pool");
        check(c.vm->pool().drop_reason(second->id()).find(state::kErrNotFound) !=
                  std::string::npos,
              "…dropped for the reason the chain gave");
    }

    // Two transactions spending ONE output. The pool refuses the second at
    // admission, so both have to be placed directly for the BUILDER's own conflict
    // check to be what is exercised. tx1 was pooled first, so tx1 is the one that
    // survives — insertion order is the tie-break, and it is what makes two nodes
    // holding the same pool build the same block.
    {
        Chain c;
        auto tx1 = c.spend(0, 100);
        auto tx2 = c.spend(0, 200);
        check(c.vm->pool().add(tx1).has_value(), "the first transaction is pooled");
        // The pool's own conflict check refuses tx2, which is the check being
        // stepped around here — this case is about the builder's.
        check(!c.vm->pool().add(tx2), "…and the pool itself already refuses the second");

        auto blk = c.vm->build();
        if (blk == nullptr) {
            check(false, "build: " + c.vm->last_error());
            return;
        }
        auto* vb = dynamic_cast<VmBlock*>(blk.get());
        check(vb != nullptr && vb->standard()->transactions.size() == 1,
              "the block carries exactly one transaction");
        check(vb != nullptr && vb->standard()->transactions[0]->id() == tx1->id(),
              "…and it is the one pooled first");
        check(blk->height() == 1, "…at the parent's height plus one");
        check(blk->parent() == c.vm->last_accepted(), "…over the preferred block");
    }

    // A block is not built out of nothing when every candidate is dropped: an
    // empty block would cost a consensus round and decide nothing.
    {
        Chain c;
        auto stale = c.spend(0, 100);
        auto other = c.spend(0, 200);
        check(c.vm->issue(stale).has_value(), "a transaction is admitted");
        auto blk = c.vm->build();
        if (blk == nullptr) {
            check(false, "build: " + c.vm->last_error());
            return;
        }
        blk->accept();
        check(c.vm->pool().add(other).has_value(), "a doomed transaction is pooled");
        check(c.vm->build() == nullptr, "the builder returns nothing rather than an empty block");
        check(c.vm->mempool_size() == 0, "…and the pool is left empty");
    }
}

// The block's timestamp is max(parent, now) — Go's "preferred timestamp after
// now" and "preferred timestamp before now" cases. Neither is a formality: a
// block that ran the clock backwards would be refused by every node including its
// own builder, and one that ran ahead would be refused as beyond the sync bound.
void the_block_timestamp_is_the_later_of_two() {
    std::printf("\n  -- the block's timestamp --\n");

    // The clock reads BEFORE the parent's timestamp: the parent's is taken.
    {
        Chain c;
        c.vm->set_now(kGenesisTime - 2);
        auto tx = c.spend(0, 100);
        check(c.vm->issue(tx).has_value(), "a transaction is admitted");
        auto blk = c.vm->build();
        if (blk == nullptr) {
            check(false, "build: " + c.vm->last_error());
            return;
        }
        auto* vb = dynamic_cast<VmBlock*>(blk.get());
        check(vb != nullptr && vb->standard()->time == kGenesisTime,
              "a clock behind the parent gives the block the PARENT's timestamp");
    }

    // The clock reads after: the clock is taken.
    {
        Chain c;
        c.vm->set_now(kGenesisTime + 5);
        auto tx = c.spend(0, 100);
        check(c.vm->issue(tx).has_value(), "a transaction is admitted");
        auto blk = c.vm->build();
        if (blk == nullptr) {
            check(false, "build: " + c.vm->last_error());
            return;
        }
        auto* vb = dynamic_cast<VmBlock*>(blk.get());
        check(vb != nullptr && vb->standard()->time == kGenesisTime + 5,
              "a clock ahead of the parent gives the block the CLOCK's timestamp");
        check(vb != nullptr && vb->standard()->time >= kGenesisTime,
              "…and either way it never precedes the parent");
    }
}

// ================= an fx the host did not supply =================
//
// Ported from vm_test.TestInvalidFx, in the only form this port can be asked.
//
// Go's VM.Initialize refuses a nil fx outright (errIncompatibleFx). This port's Vm
// takes its fx list in a CONSTRUCTOR, which has no error channel, and skips a null
// entry instead of refusing it — so "does construction fail?" cannot be asked
// here, and a test that asserted the skip would only bless it.
//
// What CAN be asked is the thing Go's error protects: a chain missing an fx must
// not act as though it had one. That is the fail-closed claim, and it is the one
// worth pinning, because it is what a missing fx would actually cost.
void a_family_the_host_did_not_supply() {
    std::printf("\n  -- an fx the host left out --\n");

    store::Memory store;
    auto genesis = genesis_asset(4);
    VmConfig cfg;
    cfg.network_id = kNetworkID;
    cfg.chain_id = chain_id();
    cfg.net_id = id(0x0A);
    cfg.fee_asset_id = genesis->id();

    // secp256k1 at index 0, and a NULL entry where nftfx would be.
    std::vector<executor::ParsedFx> fxs = {
        executor::ParsedFx{id(1), std::make_shared<fx::Secp256k1Fx>()},
        executor::ParsedFx{id(2), nullptr},
    };
    Vm vm(cfg, std::move(fxs), store);
    auto r = vm.initialize({genesis}, kGenesisTime);
    check(r.has_value(), r ? "the chain comes up" : "boot: " + r.error());
    if (!r) return;
    vm.set_bootstrapped(true);
    vm.set_now(kGenesisTime + 1);

    // Ordinary value still works: the family that IS there is unaffected.
    {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(txs::TransferableInput{txs::UTXOID{genesis->id(), 0, false},
                                                       genesis->id(), tin(kStartingBalance)});
        utx->base.outs.push_back(txs::TransferableOutput{genesis->id(), tout(100)});
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto cred = std::make_shared<fx::secp256k1fx::Credential>();
        cred->signatures = {sign_unsigned_tx(test_key(0), view(utx->bytes()))};
        tx->creds.push_back(cred);
        (void)tx->initialize();
        check(vm.issue(tx).has_value(), "the family the host DID supply still works");
    }

    // An asset declaring the missing family is refused rather than defined: index
    // 1 is not an fx this chain has.
    {
        auto mint_authority = std::make_shared<fx::nftfx::MintOutput>();
        mint_authority->group_id = 1;
        mint_authority->out_owners = owned_by(0);
        txs::InitialState s;
        s.fx_index = 1;
        s.outs = {mint_authority};
        s.sort();

        auto utx = std::make_shared<txs::CreateAssetTx>();
        utx->base.network_id = kNetworkID;
        utx->base.blockchain_id = chain_id();
        utx->name = "Team Rocket";
        utx->symbol = "TR";
        utx->states = {s};
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        (void)tx->initialize();

        auto issued = vm.issue(tx);
        check(!issued, "an asset declaring the missing family is refused");
        check(!issued && issued.error().find(txs::kErrUnknownFx) != std::string::npos,
              "…as an unknown feature extension, not quietly defined");
    }
}

// ================= acceptance that cannot be made durable =================
//
// Ported from the failure half of block/executor.TestBlockAccept. Go injects three
// failures into Accept — the commit batch cannot be taken, shared memory cannot be
// applied, the metrics cannot be recorded — and requires that each is RETURNED.
//
// Two of the three have no counterpart here: this port keeps no metrics, and it
// never writes shared memory (see the importing/exporting cases above). The third
// does: the store under the state can refuse to commit, and Go's "can't get commit
// batch" is exactly that.
//
// The port's seam declares `void accept()`, so a refusal cannot be returned; what
// it can do is not be SILENT, and last_error() is where it says so. That is what
// is asserted. It is also less than Go gets: the in-memory chain has already moved
// by the time the commit is attempted, so a caller that ignores last_error() would
// carry on from a height that is not on disk. That gap is named here rather than
// asserted away.
struct RefusingStore final : store::Store {
    store::Memory under;
    bool refuse = false;
    int commits = 0;

    std::optional<Bytes> get(ByteView key) const override { return under.get(key); }
    void put(ByteView key, ByteView value) override { under.put(key, value); }
    void erase(ByteView key) override { under.erase(key); }
    void each(ByteView prefix,
              const std::function<bool(ByteView, ByteView)>& f) const override {
        under.each(prefix, f);
    }
    store::Result<void> commit() override {
        ++commits;
        if (refuse) return std::unexpected("the disk is full");
        return under.commit();
    }
};

void an_acceptance_that_cannot_be_made_durable() {
    std::printf("\n  -- acceptance the store refuses --\n");

    RefusingStore store;
    auto genesis = genesis_asset(4);
    VmConfig cfg;
    cfg.network_id = kNetworkID;
    cfg.chain_id = chain_id();
    cfg.net_id = id(0x0A);
    cfg.fee_asset_id = genesis->id();

    Vm vm(cfg, the_fxs(), store);
    auto r = vm.initialize({genesis}, kGenesisTime);
    check(r.has_value(), r ? "the chain comes up" : "boot: " + r.error());
    if (!r) return;
    vm.set_bootstrapped(true);
    vm.set_now(kGenesisTime + 1);
    check(store.commits == 1, "installing genesis committed once — genesis is durable too");

    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->base.ins.push_back(txs::TransferableInput{txs::UTXOID{genesis->id(), 0, false},
                                                   genesis->id(), tin(kStartingBalance)});
    utx->base.outs.push_back(txs::TransferableOutput{genesis->id(), tout(100)});
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

    store.refuse = true;
    blk->accept();
    check(!vm.last_error().empty(), "an acceptance the store refuses is not silent");
    check(vm.last_error().find("the disk is full") != std::string::npos,
          "…and it reports the store's own reason");

    // Go's other two injections — shared memory and metrics — have no counterpart:
    // this port applies neither, so there is nothing there to fail. Named, not
    // faked.

    // Go's other two injections — shared memory and metrics — have no counterpart:
    // this port applies neither, so there is nothing there to fail. Named, not
    // faked.
    //
    // The block-not-found path is Go's first Accept case, asserted in
    // accept_unverified() above. It is asked here too, against this same VM, so the
    // two failures are known to be distinguishable rather than one error string
    // standing in for every way acceptance can go wrong.
    auto stranger = block::build(vm.last_accepted(), vm.last_accepted_height() + 1, vm.now(),
                                 kEmptyId, {tx});
    if (stranger) {
        auto parsed = vm.parse(view((*stranger)->bytes));
        if (parsed != nullptr) {
            parsed->accept();
            check(vm.last_error() == std::string(kErrBlockNotFound),
                  "…while a block nobody verified fails for its own, different reason");
        }
    }
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
    accept_unverified();
    seam_answers();
    through_the_seam_only();
    the_empty_root_is_refused();
    an_imported_input_is_spent_once();
    a_utxo_arrives_from_another_chain();
    a_utxo_leaves_for_another_chain();
    the_nft_family_end_to_end();
    the_property_family_end_to_end();
    two_assets_in_one_transaction();
    the_builder_leaves_out_what_will_not_hold();
    the_block_timestamp_is_the_later_of_two();
    a_family_the_host_did_not_supply();
    an_acceptance_that_cannot_be_made_durable();
    return report("vm");
}
