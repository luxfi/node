// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm_test — build → verify → accept → REJECT over the node's seam, and what a
// restart finds.
//
// Reject is the half a chain is most likely to get wrong, so it is tested
// hardest: a rejected block's transactions must come BACK, because they were
// never refused — they lost a race — and a node that dropped them would disagree
// with every other node about what is still pending and then propose a block
// built from that disagreement.

#include "lux/dexvm/vm.hpp"

#include "check.hpp"
#include "fixtures.hpp"

#include <memory>
#include <string>
#include <unistd.h>

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

class TempPath {
public:
    TempPath() {
        char tmpl[] = "/tmp/dexvm-vm-XXXXXX";
        dir_ = ::mkdtemp(tmpl);
        path_ = dir_ + "/log";
    }
    ~TempPath() {
        ::unlink(path_.c_str());
        ::unlink((path_ + ".compact").c_str());
        ::rmdir(dir_.c_str());
    }
    const std::string& path() const { return path_; }

private:
    std::string dir_;
    std::string path_;
};

// The chain under test: a devnet C-Chain with two real ERC-20s seeded, so the
// two transactions have something true to say.
struct Fixture {
    std::shared_ptr<FakeChain> chain = std::make_shared<FakeChain>();
    Id c_chain = test_id(500);
    Bytes addr_a = addr20(0x60);
    Bytes addr_b = addr20(0x61);

    Fixture() {
        chain->seed_erc20(kMainnetID, c_chain, view(addr_a), 6);
        chain->seed_erc20(kMainnetID, c_chain, view(addr_b), 6);
    }

    Asset asset(const Bytes& addr, const std::string& sym) const {
        return Asset{kMainnetID, c_chain,  AssetKind::ERC20, addr, 6, sym, sym + " token",
                     true,       RiskTier::Tier1};
    }

    Result<std::unique_ptr<VM>> vm(store::Store& s) const {
        return VM::make(test_id(999), s, NetworkClass::Mainnet, default_dex_asset_policy(), chain,
                        nullptr);
    }
};

void the_seam_answers_its_five_questions() {
    std::printf("the seam's own questions, on an empty chain\n");
    Fixture f;
    store::Memory s;
    auto vm = f.vm(s);
    admitted(vm, "the chain opens over an empty store");
    if (!vm) return;

    check_eq((*vm)->alias(), "D", "it answers to D");
    check((*vm)->chain_id() == test_id(999), "and knows its own id");
    check((*vm)->last_accepted() == kEmptyId, "nothing accepted yet");
    check((*vm)->last_accepted_height() == 0, "at height zero");
    check((*vm)->build() == nullptr, "nothing to build is 'no', not a failure");
    check((*vm)->get(test_id(1)) == nullptr, "an unknown id is 'no'");
    const std::uint8_t junk[] = {1, 2, 3, 4};
    check((*vm)->parse(std::span<const std::uint8_t>(junk, 4)) == nullptr, "and so is a non-block");
}

void build_verify_accept() {
    std::printf("build → verify → accept, and the root that came out of executing\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }
    VM& vm = **vmr;

    vm.issue(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    vm.issue(Tx::register_asset(f.asset(f.addr_b, "BBB")));
    check(vm.pending() == 2, "two transactions pending");

    auto blk = vm.build();
    check(blk != nullptr, "a block is built");
    if (!blk) return;
    check(blk->height() == 1, "at height one");
    check(blk->parent() == kEmptyId, "on the empty parent");
    check(blk->verify(), "and this node's own execution accepts it");

    const Id root = blk->root();
    check(root != kEmptyId, "it names the state its execution produced");

    // The root is EXECUTED, not carried: the bytes hold no root, so a second
    // node deriving it from the same bytes must reach the same value.
    store::Memory s2;
    auto vm2r = f.vm(s2);
    if (vm2r) {
        auto parsed = (*vm2r)->parse(blk->bytes());
        check(parsed != nullptr, "another node parses the same bytes");
        if (parsed) {
            check(parsed->id() == blk->id(), "names the block identically");
            check(parsed->verify(), "executes it too");
            check(parsed->root() == root, "and derives the SAME root");
        }
    }

    blk->accept();
    check(vm.last_accepted() == blk->id(), "acceptance is durable and reported");
    check(vm.last_accepted_height() == 1, "at height one");
    check(vm.state().registry().len() == 2, "with both assets admitted");
    check(vm.pending() == 0, "and the pool drained");
    check(vm.state().root() == root, "the live set folds to the root the block named");
}

void a_market_may_follow_its_assets_in_one_block() {
    std::printf("a market may follow the two assets it names, in the same block\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }
    VM& vm = **vmr;

    auto id_a = derive_asset_id(kMainnetID, f.c_chain, AssetKind::ERC20, view(f.addr_a));
    auto id_b = derive_asset_id(kMainnetID, f.c_chain, AssetKind::ERC20, view(f.addr_b));
    vm.issue(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    vm.issue(Tx::register_asset(f.asset(f.addr_b, "BBB")));
    vm.issue(Tx::create_market(Market{kMainnetID, *id_a, *id_b, {}, true}));

    auto blk = vm.build();
    check(blk != nullptr, "the block builds");
    if (!blk) return;
    check(blk->verify(), "and verifies — order within a block is what makes it possible");
    blk->accept();
    check(vm.state().registry().market_len() == 1, "the market is admitted");

    // The same market a second time is a duplicate, so it cannot get into a
    // block at all: build leaves it in the pool rather than proposing a block
    // that would not verify.
    vm.issue(Tx::create_market(Market{kMainnetID, *id_a, *id_b, {}, true}));
    check(vm.build() == nullptr, "a duplicate cannot be built into a block");
    check(vm.pending() == 1, "and stays pending rather than vanishing");
}

void a_block_this_node_refuses_is_not_accepted() {
    std::printf("a block whose transactions this node cannot admit\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }
    VM& vm = **vmr;

    // A block proposed by a peer carrying a token that is not on this node's
    // chain. It parses — the bytes are well formed — and then execution refuses.
    BlockBody body;
    body.parent = kEmptyId;
    body.height = 1;
    body.txs.push_back(Tx::register_asset(f.asset(addr20(0xEE), "GHOST")));
    const Bytes bytes = body.encode();

    auto blk = vm.parse(bytes);
    check(blk != nullptr, "the bytes parse");
    if (!blk) return;
    check(!blk->verify(), "but this node refuses to vote for it");
    check(blk->root() == kEmptyId, "and names no state, because it produced none");

    // Accept is inert on a block execution refused: a node that wrote here would
    // durably record a set it never agreed to.
    blk->accept();
    check(vm.last_accepted() == kEmptyId, "accepting it does nothing");
    check(vm.state().registry().len() == 0, "and the set is untouched");
}

void a_block_out_of_position_is_refused() {
    std::printf("a block that does not sit where this node is\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }
    VM& vm = **vmr;

    BlockBody wrong_parent;
    wrong_parent.parent = test_id(600);
    wrong_parent.height = 1;
    wrong_parent.txs.push_back(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    auto blk = vm.parse(wrong_parent.encode());
    check(blk != nullptr, "it parses");
    if (blk) check(!blk->verify(), "and is refused: its parent's effects are not in the set");

    BlockBody wrong_height;
    wrong_height.parent = kEmptyId;
    wrong_height.height = 7;
    wrong_height.txs.push_back(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    auto blk2 = vm.parse(wrong_height.encode());
    check(blk2 != nullptr, "a height that does not follow parses too");
    if (blk2) check(!blk2->verify(), "and is refused as well");
}

void reject_hands_the_transactions_back() {
    std::printf("REJECT hands the transactions back — the half most easily got wrong\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }
    VM& vm = **vmr;

    vm.issue(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    vm.issue(Tx::register_asset(f.asset(f.addr_b, "BBB")));
    auto blk = vm.build();
    check(blk != nullptr && blk->verify(), "a block is built and verifies");
    if (!blk) return;

    // The block loses a race. Its transactions were never refused.
    blk->reject();
    check(vm.pending() == 2, "both transactions are back in the pool");
    check(vm.state().registry().len() == 0, "and nothing was admitted");
    check(vm.last_accepted() == kEmptyId, "nor accepted");

    // They are still good, so the next build proposes them again.
    auto again = vm.build();
    check(again != nullptr, "the next build proposes them again");
    if (again) {
        check(again->verify(), "and it verifies");
        again->accept();
        check(vm.state().registry().len() == 2, "so nothing was lost by losing the race");
    }

    // A decision is made ONCE, either way.
    again->reject();
    check(vm.state().registry().len() == 2, "reject after accept is inert");
    check(vm.pending() == 0, "and hands nothing back");
}

void a_rejected_block_does_not_duplicate_the_pool() {
    std::printf("a transaction handed back twice is still one transaction\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }
    VM& vm = **vmr;

    vm.issue(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    auto blk = vm.build();
    if (!blk) {
        check(false, "build");
        return;
    }
    blk->reject();
    blk->reject();  // a second reject is inert
    check(vm.pending() == 1, "the pool holds it once");
}

void what_a_restart_finds() {
    std::printf("what a restart finds, which is the reason the store exists\n");
    Fixture f;
    TempPath tp;
    Id accepted_id{};
    Id accepted_root{};

    {
        auto s = store::File::open(tp.path());
        if (!s) {
            check(false, "store opens");
            return;
        }
        auto vmr = f.vm(**s);
        if (!vmr) {
            check(false, "chain opens");
            return;
        }
        VM& vm = **vmr;
        vm.issue(Tx::register_asset(f.asset(f.addr_a, "AAA")));
        vm.issue(Tx::register_asset(f.asset(f.addr_b, "BBB")));
        auto blk = vm.build();
        if (!blk || !blk->verify()) {
            check(false, "a block builds and verifies");
            return;
        }
        blk->accept();
        accepted_id = blk->id();
        accepted_root = blk->root();
        check(vm.state().registry().len() == 2, "two assets admitted before the restart");
    }
    {
        auto s = store::File::open(tp.path());
        if (!s) {
            check(false, "store reopens");
            return;
        }
        auto vmr = f.vm(**s);
        admitted(vmr, "the chain reopens over the same log");
        if (!vmr) return;
        VM& vm = **vmr;
        check(vm.last_accepted() == accepted_id, "and reports the block it last accepted");
        check(vm.last_accepted_height() == 1, "at the height it accepted it");
        check(vm.state().registry().len() == 2, "with the admitted set restored");
        check(vm.state().root() == accepted_root, "folding to exactly the root it signed");

        // And it carries on from there rather than re-signing the height.
        vm.issue(Tx::register_asset(f.asset(f.addr_a, "AAA")));
        check(vm.build() == nullptr, "an asset already admitted cannot be admitted again");
    }
}

void the_block_bytes_are_canonical() {
    std::printf("a block's bytes are canonical, so two nodes name it identically\n");
    Fixture f;
    BlockBody body;
    body.parent = test_id(700);
    body.height = 42;
    body.txs.push_back(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    body.txs.push_back(Tx::create_market(Market{kMainnetID, test_id(1), test_id(2),
                                                to_bytes(view(addr20(0x01))), true}));
    const Bytes bytes = body.encode();

    auto back = decode_block(view(bytes));
    admitted(back, "a block round-trips");
    if (!back) return;
    check(back->parent == body.parent, "its parent survives");
    check(back->height == body.height, "its height survives");
    check(back->txs.size() == 2, "and both transactions");
    check(back->encode() == bytes, "re-encoding is byte-identical");
    check(back->id() == body.id(), "so the two name the same block");

    // Transaction order is preserved exactly, because it is consensus: the same
    // transactions in another order can admit a different set.
    check(back->txs[0].kind == TxKind::RegisterAsset, "the first is the asset");
    check(back->txs[1].kind == TxKind::CreateMarket, "and the second the market");
    BlockBody swapped = body;
    std::swap(swapped.txs[0], swapped.txs[1]);
    check(swapped.id() != body.id(), "and reordering them is a different block");

    // A truncated or corrupt block does not decode into something plausible.
    refused_any(decode_block(view(Bytes(bytes.begin(), bytes.begin() + 8))), "a truncated block");
    refused_any(decode_block(view(Bytes{0, 1, 2, 3})), "and garbage");
}

// A tiny deterministic generator: a mutation corpus must reproduce exactly, or
// a failure found once cannot be found again.
std::uint64_t next_random(std::uint64_t* state) {
    *state ^= *state << 13;
    *state ^= *state >> 7;
    *state ^= *state << 17;
    return *state;
}

void mutated_bytes_are_refused_not_believed() {
    std::printf("a mutated block is refused, never half-believed\n");
    Fixture f;
    store::Memory s;
    auto vmr = f.vm(s);
    if (!vmr) {
        check(false, "open");
        return;
    }

    BlockBody body;
    body.parent = test_id(800);
    body.height = 3;
    body.txs.push_back(Tx::register_asset(f.asset(f.addr_a, "AAA")));
    body.txs.push_back(Tx::create_market(Market{kMainnetID, test_id(1), test_id(2), {}, true}));
    const Bytes original = body.encode();

    // Every decode below is over bytes a peer could have sent. What must hold is
    // not that they are rejected — a mutation can be harmless — but that the
    // decoder never reads outside its buffer and never invents a block whose
    // re-encoding differs from what it was handed. Run under ASan and UBSan,
    // this is where an out-of-bounds read would surface.
    // Most single-byte edits land in a symbol or an address and produce a
    // DIFFERENT but perfectly well-formed block — that is correct, and the id
    // moves with the bytes. So the assertion is not "mutations are refused"; it
    // is that every input is either refused or decodes to something that
    // re-encodes to exactly what it was handed.
    std::uint64_t state = 0x9E3779B97F4A7C15ull;
    int decoded = 0, refused_count = 0, non_canonical = 0, canonical = 0;
    for (int i = 0; i < 4000; ++i) {
        Bytes mutated = original;
        const int edits = 1 + int(next_random(&state) % 3);
        for (int e = 0; e < edits; ++e) {
            const std::size_t at = std::size_t(next_random(&state) % mutated.size());
            mutated[at] = std::uint8_t(next_random(&state) & 0xff);
        }
        auto got = decode_block(view(mutated));
        if (!got) {
            ++refused_count;
            continue;
        }
        ++decoded;
        // A decoder that accepts must round-trip: a block whose re-encoding
        // differs would hash to an id its own contents do not produce, and two
        // nodes would name the same block differently. parse() enforces exactly
        // this, so anything non-canonical is refused there.
        if (got->encode() != mutated) {
            ++non_canonical;
            // Reported once: the log states the property, not four thousand
            // instances of it.
            if (non_canonical == 1)
                check((*vmr)->parse(mutated) == nullptr,
                      "a non-canonical encoding is refused at parse");
            else if ((*vmr)->parse(mutated) != nullptr)
                check(false, "a non-canonical encoding reached parse");
        } else {
            ++canonical;
            // A decoded block names itself by the hash of the bytes it was given.
            if (got->id() != sha256(view(mutated))) {
                check(false, "a decoded block's id is the hash of its own bytes");
                return;
            }
        }
    }
    check(decoded + refused_count == 4000,
          "all 4000 mutations were decided, none crashed (" + std::to_string(refused_count) +
              " refused, " + std::to_string(canonical) + " re-encoded exactly, " +
              std::to_string(non_canonical) + " non-canonical and so refused at parse)");
    check(refused_count > 0, "and a mutated header is refused rather than guessed at");

    // Truncations at every length, which is where a length field is trusted.
    int survived = 0;
    for (std::size_t n = 0; n < original.size(); ++n) {
        auto got = decode_block(view(Bytes(original.begin(), original.begin() + std::ptrdiff_t(n))));
        if (got) ++survived;
    }
    check(true, "every truncation is decided rather than crashing (" +
                    std::to_string(survived) + " decoded)");

    // And the same for a single transaction.
    const Bytes tx_bytes = body.txs[0].encode();
    for (int i = 0; i < 2000; ++i) {
        Bytes mutated = tx_bytes;
        mutated[std::size_t(next_random(&state) % mutated.size())] =
            std::uint8_t(next_random(&state) & 0xff);
        (void)decode_tx(view(mutated));
    }
    check(true, "a mutated transaction is decided too");
}

}  // namespace

int main() {
    the_seam_answers_its_five_questions();
    build_verify_accept();
    a_market_may_follow_its_assets_in_one_block();
    a_block_this_node_refuses_is_not_accepted();
    a_block_out_of_position_is_refused();
    reject_hands_the_transactions_back();
    a_rejected_block_does_not_duplicate_the_pool();
    what_a_restart_finds();
    the_block_bytes_are_canonical();
    mutated_bytes_are_refused_not_believed();
    return report("vm");
}
