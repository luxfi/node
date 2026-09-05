// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.hpp — the F-Chain: the coordination plane for confidential compute
// (LP-8200, LP-167).
//
// F records the PUBLIC coordinates of encrypted values — a handle, the digest
// of the off-chain ciphertext body, its owner, the capabilities granted over
// it, and the threshold decryptions asked for and answered — and it records
// nothing else. It holds no ciphertext body, no FHE secret key, and no
// decryption share; see records.hpp for the structurally-enforced invariant.
//
// F owns its own persistence rather than reusing the FHE runtime's registry,
// and the reason is consensus: that registry stamps records with the wall
// clock, which is right for the off-chain daemon it was written for and wrong
// here — two validators replaying one block would store different bytes. Every
// timestamp F writes comes from the accepting block.
//
// Mutating operations take effect only through fee-settled consensus blocks,
// priced by a per-scheme gas schedule and burned from the payer's on-chain
// balance.

#pragma once

#include "lux/fhevm/block.hpp"
#include "lux/fhevm/error.hpp"
#include "lux/fhevm/fee.hpp"
#include "lux/fhevm/gas.hpp"
#include "lux/fhevm/records.hpp"
#include "lux/fhevm/store.hpp"
#include "lux/fhevm/transaction.hpp"
#include "lux/node/vm.hpp"

#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <set>
#include <string>
#include <vector>

namespace lux::fhevm {

inline constexpr std::string_view kVersion = "1.0.0";
inline constexpr std::string_view kVMName = "fhevm";

// Database namespaces. Records are JSON; balances live under the fee ledger's
// own namespace.
inline constexpr std::string_view kCiphertextPrefix = "ct:";
inline constexpr std::string_view kPermitPrefix = "pm:";
inline constexpr std::string_view kDecryptPrefix = "dr:";
inline constexpr std::string_view kEpochPrefix = "ep:";
inline constexpr std::string_view kBlockPrefix = "block:";
inline constexpr std::string_view kHeightPrefix = "height:";
inline constexpr std::string_view kNoncePrefix = "nonce:";

// The three single-valued pointers a node reads on boot. Each is written in the
// same commit as the block that moved it.
inline constexpr std::string_view kLastAcceptedKey = "fhevm/last-accepted";
inline constexpr std::string_view kGenesisMarker = "fhevm/genesis-applied";
inline constexpr std::string_view kCurrentEpochKey = "fhevm/current-epoch";

// MaxMempool bounds the queue. Admission is open to anyone who can pay, so
// without a bound the queue is whatever an adversary chooses to make it.
inline constexpr std::size_t kMaxMempool = 4096;

// VMID identifies F-Chain: coordination of confidential compute — ciphertext
// handles, access permits, and threshold decryption (LP-8200, LP-167).
//
// A vmID is an immutable one-way door: it is baked into the chain-creation
// transaction at genesis, it is the plugin binary's filename, and the P-Chain
// stores it forever. Every declaration of it must agree, so there is exactly
// one.
Id vm_id();

// Clock is the one thing this chain reads that is not in its own state, and it
// reads it only as a bound. A test drives it directly, which is why it is a
// value the VM holds rather than a call it makes.
class Clock {
public:
    std::int64_t now() const { return faked_ ? faked_at_ : real_now(); }
    void set(std::int64_t t) {
        faked_ = true;
        faked_at_ = t;
    }
    void advance(std::int64_t by) { set(now() + by); }
    void unfake() { faked_ = false; }

    static std::int64_t real_now();

private:
    bool faked_ = false;
    std::int64_t faked_at_ = 0;
};

// Genesis is the F-Chain genesis: a funding allocation (address hex -> nLUX)
// and the epoch-0 threshold committee. There is no ciphertext in genesis —
// ciphertexts are registered through consensus transactions — so genesis
// carries nothing encrypted.
struct Genesis {
    std::int64_t version = 0;
    std::string message;
    std::int64_t timestamp = 0;
    // alloc is ordered by address, which is how Go marshals a map, so the two
    // produce the same document.
    std::map<std::string, std::uint64_t> alloc;
    std::vector<CommitteeMember> committee;
    std::int64_t threshold = 0;
    Bytes public_key;
    bool public_key_nil = true;
    bool committee_nil = true;
    bool alloc_nil = true;
};

Result<Genesis> parse_genesis(std::string_view json);
std::string marshal(const Genesis& g);

// HealthReport is what an operator reads. A committee is what makes F
// answerable: with none seated, decryptions can be requested but never
// fulfilled, which is a degraded chain and is reported as such rather than as
// healthy.
struct HealthReport {
    bool healthy = false;
    std::map<std::string, std::string> details;
};

class VM final : public lux::node::VM {
public:
    struct Config {
        std::uint32_t network_id = 0;
        Id chain_id{};
        std::string alias = "F";
    };

    // The store is a constructor argument rather than something the VM makes
    // for itself: whether this node survives a restart is the host's decision.
    VM(Store* store, Config cfg);

    // initialize wires the ledger, the fee policy, the caches and genesis. It
    // is idempotent across restarts: genesis is applied once, behind a marker.
    Result<void> initialize(std::string_view genesis_json);

    // ---- the node's seam ----
    lux::node::Id chain_id() const override { return chain_id_; }
    std::string alias() const override { return alias_; }
    std::shared_ptr<lux::node::Block> build() override;
    std::shared_ptr<lux::node::Block> parse(std::span<const std::uint8_t> b) override;
    std::shared_ptr<lux::node::Block> get(const lux::node::Id& id) const override;
    void prefer(const lux::node::Id&) override {}
    lux::node::Id last_accepted() const override { return last_accepted_; }
    std::uint64_t last_accepted_height() const override { return height_; }

    // ---- the same three, with their reasons ----
    Result<std::shared_ptr<Block>> build_block();
    Result<std::shared_ptr<Block>> parse_block(ByteView b);
    Result<std::shared_ptr<Block>> get_block(const Id& id) const;

    // ---- admission ----
    //
    // submit_tx admits a transaction to this node's mempool. Admission is a
    // FILTER, not a consensus rule: it refuses what this node can already tell
    // will not work, so the queue stays useful. The rules consensus actually
    // enforces live in the batch, and the fee is SETTLED later, in acceptance —
    // never here.
    Result<Id> submit_tx(const Transaction& tx);

    // ---- PUBLIC state, as the transaction rules read it ----
    const CiphertextRecord* ciphertext(const Id& handle) const;
    const PermitRecord* permit(const Id& id) const;
    const DecryptRecord* decrypt(const Id& id) const;
    const EpochRecord* epoch(std::uint64_t n) const;
    // current_epoch returns the sitting epoch. A chain whose genesis declared no
    // committee has an epoch 0 with no members, which authorizes nobody — so
    // fulfilment and epoch advance are refused until a committee exists, rather
    // than accepted from anyone.
    EpochRecord current_epoch() const;
    std::uint64_t current_epoch_number() const { return epoch_; }
    std::vector<const CiphertextRecord*> ciphertexts() const;

    Result<void> put_ciphertext(const CiphertextRecord& r);
    Result<void> put_permit(const PermitRecord& r);
    Result<void> put_decrypt(const DecryptRecord& r);
    Result<void> put_epoch(const EpochRecord& r);
    Result<void> set_current_epoch(std::uint64_t n);

    // nonce_of returns the payer's last-used nonce. An account that has never
    // transacted has nonce 0; a read that FAILED is an error, not a zero,
    // because a zero here says "this payer has spent nothing" and lets every
    // transaction it ever signed through again.
    Result<std::uint64_t> nonce_of(const Account& payer) const;
    Result<void> set_nonce(const Account& payer, std::uint64_t n);

    Result<std::uint64_t> balance(const Account& acct) const;
    Result<std::uint64_t> burned() const;
    fee::Ledger& ledger() { return ledger_; }
    const fee::Ledger& ledger() const { return ledger_; }
    const fee::FlatPolicy& fee_policy() const { return fee_policy_; }

    Store* store() const { return store_; }
    Clock& clock() { return clock_; }
    const Clock& clock() const { return clock_; }
    std::uint64_t height() const { return height_; }
    std::uint32_t network_id() const { return network_id_; }

    Result<Id> block_id_at_height(std::uint64_t h) const;
    Result<HealthReport> health() const;
    void shutdown() { shutting_down_ = true; }
    bool shutting_down() const { return shutting_down_; }

    // dump lists every row this chain has written, in key order, so two nodes
    // that replayed the same blocks can be compared exactly — and so a failure
    // names the row that differs.
    std::string dump() const;

    // records_root digests every PUBLIC record this chain holds, in key order.
    // It is NOT a consensus commitment — F commits no state root, see
    // Block::root — it is what the replay test compares when it drives one
    // chain onto two independently-built nodes and requires them to agree.
    Id records_root() const;

    // ---- the mempool, and the blocks in flight ----
    const std::vector<Transaction>& mempool() const { return mempool_; }
    // pool is the queue itself, for a caller that has already decided what goes
    // in it — gossip intake, and a test that must place a transaction the
    // admission FILTER would refuse but consensus still has to cope with.
    std::vector<Transaction>& pool() { return mempool_; }
    // skipped counts records that were on disk and did not decode. One
    // unreadable row is skipped, not guessed at; a whole unreadable index stops
    // the boot instead.
    std::size_t skipped() const { return skipped_; }
    const std::set<Id>& claims() const { return claims_; }
    const std::map<Id, std::shared_ptr<Block>>& pending_blocks() const { return pending_; }
    // release drops accepted transactions from the mempool and rebuilds the
    // indexes from what remains, so a claim or a queued nonce cannot outlive the
    // transaction that put it there.
    void release(const std::vector<Transaction>& txs);
    // track_verified makes a block findable by id while it is in flight, so a
    // child can resolve it as a parent. It also PRUNES, because nothing else
    // will: the engine may drop a block it never accepts and never rejects.
    void track_verified(const std::shared_ptr<Block>& b);
    // on_tip reports whether a block whose parent is `parent` extends the
    // chain: the parent is the accepted tip, or a block that verified above it
    // and is still in flight. Height alone is not that check — a block whose
    // parent is an OLD accepted block satisfies height == parent+1 perfectly
    // well, and accepting it rewinds the chain.
    Result<void> on_tip(const Id& parent) const;

    // note_revert records that a transaction failed authorization and had no
    // effect. The fee was still burned and the nonce still consumed.
    void note_revert(const Transaction& tx, const Error& why);
    struct Revert {
        Id tx_id;
        Err reason;
    };
    const std::vector<Revert>& reverts() const { return reverts_; }

    // load_state rebuilds the PUBLIC caches and the last-accepted pointer from
    // the store. Acceptance's failure path calls it after an abort.
    Result<void> load_state();

    // The block drives these two directly; they are the whole of what
    // acceptance changes outside the records.
    void set_accepted(const std::shared_ptr<Block>& b);
    void drop_pending(const Id& id);

private:
    Result<void> seed_genesis(const Genesis& g, const Id& genesis_block_id);
    Result<std::optional<Bytes>> read(ByteView key, std::size_t width) const;

    Store* store_;
    std::uint32_t network_id_ = 0;
    Id chain_id_{};
    std::string alias_;
    Clock clock_;

    fee::Ledger ledger_;
    fee::FlatPolicy fee_policy_;

    std::map<Id, CiphertextRecord> ciphertexts_;
    std::map<Id, PermitRecord> permits_;
    std::map<Id, DecryptRecord> decrypts_;
    std::map<std::uint64_t, EpochRecord> epochs_;
    std::uint64_t epoch_ = 0;

    std::vector<Transaction> mempool_;
    // claims holds the effect of every queued transaction so admission can
    // refuse a second claim on it; queued holds the highest nonce queued per
    // payer so admission demands the same consecutive nonces consensus does.
    // Both are derived from the mempool and rebuilt from it whenever it
    // shrinks, so neither can drift away from the queue it describes.
    std::set<Id> claims_;
    std::map<Account, std::uint64_t> queued_;

    std::map<Id, std::shared_ptr<Block>> pending_;
    Id last_accepted_{};
    std::shared_ptr<Block> last_block_;
    std::uint64_t height_ = 0;

    bool shutting_down_ = false;
    std::size_t skipped_ = 0;
    std::vector<Revert> reverts_;
};

// batch is the running state a block's transactions must satisfy AS A
// SEQUENCE: nonces strictly in order per payer, fees affordable against a
// running per-payer debit, and no two transactions claiming one effect.
//
// One rule, two policies. Verify runs it over a received block and refuses the
// whole block on the first transaction that does not fit; the builder runs it
// over the mempool and simply leaves out what does not fit. A proposer
// therefore cannot build a block its own verify would reject, and one unfit
// transaction can no longer take the rest of the mempool down with it.
//
// What is deliberately NOT here is authorization: it reads state that earlier
// transactions in the same block may change, so a block-time verdict can differ
// from the application-time one.
class Batch {
public:
    explicit Batch(const VM& vm) : vm_(&vm) {}

    // admit tests one transaction against the running state and, if it fits,
    // records its consumption of that state.
    Result<void> admit(const Transaction& tx);

private:
    const VM* vm_;
    std::map<Account, std::uint64_t> nonce_;  // next nonce owed by each payer
    std::map<Account, std::uint64_t> spent_;  // running debit per payer
    std::set<Id> claimed_;                    // effects already taken in this block
};

}  // namespace lux::fhevm
